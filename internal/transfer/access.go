package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

// AccessError identifies the endpoint that actually denied an operation. A
// controller must not guess from an error string and elevate the wrong host.
type AccessError struct {
	Source bool
	Path   string
	Err    error
}

func (e *AccessError) Error() string { return fmt.Sprintf("access %q: %v", e.Path, e.Err) }
func (e *AccessError) Unwrap() error { return e.Err }
func accessError(err error, source bool, path string) error {
	var existing *AccessError
	if err == nil || !errors.Is(err, fs.ErrPermission) || errors.As(err, &existing) {
		return err
	}
	return &AccessError{Source: source, Path: path, Err: err}
}

// SnapshotForOperation keeps one source baseline across route/permission
// fallback. A new method must not adopt a source changed by a previous attempt.
func SnapshotForOperation(ctx context.Context, op Operation) (Manifest, error) {
	current, err := Snapshot(ctx, op.Source, op.SourcePath, op.Move)
	if err != nil {
		return Manifest{}, accessError(err, true, op.SourcePath)
	}
	if op.Baseline != nil {
		if err := CompareManifests(*op.Baseline, current, false); err != nil {
			return Manifest{}, errors.Join(ErrSourceChanged, err)
		}
	}
	// A non-destructive regular-file copy still works without inode support.
	// Destructive moves and directory alias checks require stable bindings.
	if op.Move || len(current.Items) > 0 && current.Items[0].Mode.IsDir() {
		guard, err := prepareDestinationGuard(ctx, op, current)
		if err != nil {
			return Manifest{}, err
		}
		if err := checkDirectoryOverlap(ctx, op, current, guard); err != nil {
			return Manifest{}, err
		}
		if err := guard.verify(ctx, op); err != nil {
			return Manifest{}, err
		}
		current.destination = guard
		if op.Baseline != nil {
			// Only controller verification evidence changes. The original
			// source content/identity snapshot is never replaced on fallback.
			op.Baseline.destination = guard
		}
	}
	if op.Baseline != nil {
		return *op.Baseline, nil
	}
	return current, nil
}

// FinishMove may be retried after an explicitly approved source-delete
// permission change, but only against the SAME before manifest. It never
// copies again or replaces the baseline. Every failure retains the source.
func FinishMove(ctx context.Context, op Operation, before Manifest) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = PreserveSource(fmt.Errorf("已复制但未移动，源文件已保留: %w", retErr))
		}
	}()
	afterSource, err := Snapshot(ctx, op.Source, op.SourcePath, true)
	if err != nil {
		return accessError(err, true, op.SourcePath)
	}
	if err := CompareManifests(before, afterSource, false); err != nil {
		return errors.Join(ErrSourceChanged, err)
	}
	if before.destination != nil {
		if err := before.destination.verify(ctx, op); err != nil {
			return err
		}
	} else if len(before.Items) > 0 && before.Items[0].Mode.IsDir() {
		return errors.New("缺少传输前目录重叠检查，源已保留")
	}
	afterTarget, err := Snapshot(ctx, op.Destination, op.TargetPath, true)
	if err != nil {
		return accessError(err, false, op.TargetPath)
	}
	if err := CompareManifests(before, afterTarget, true); err != nil {
		return fmt.Errorf("目标校验失败，源文件已保留: %w", err)
	}
	if op.PreserveOwner {
		byPath := make(map[string]ManifestItem, len(afterTarget.Items))
		for _, item := range afterTarget.Items {
			byPath[item.Relative] = item
		}
		for _, item := range before.Items {
			got := byPath[item.Relative]
			if item.OwnerKnown && (!got.OwnerKnown || item.UID != got.UID || item.GID != got.GID) {
				return fmt.Errorf("目标所有权校验失败，源已保留: %q", item.Relative)
			}
		}
	}
	if err := syncMoveDestination(ctx, op, before); err != nil {
		return fmt.Errorf("目标最终落盘未确认: %w", accessError(err, false, op.TargetPath))
	}
	if err := VerifySourceUnchanged(ctx, op.Source, op.SourcePath, before); err != nil {
		return err
	}
	if before.destination != nil {
		if err := before.destination.verify(ctx, op); err != nil {
			return err
		}
	}
	// Pin every copied destination entry to the object whose contents were
	// verified, including directories that did not exist before copying and
	// children of merged directories. A stable root alone cannot protect a
	// subtree renamed during the long final source hash. This remains a final
	// read-only check, not a lock against later external filesystem changes.
	verified := make(map[string]ManifestItem, len(afterTarget.Items))
	for _, item := range afterTarget.Items {
		verified[item.Relative] = item
	}
	for _, item := range before.Items {
		path := targetPath(op, item.Relative)
		current, err := bindingAt(ctx, op.Destination, path)
		if err != nil {
			return fmt.Errorf("目标最终文件身份未确认，源已保留: %w", accessError(err, false, path))
		}
		wanted := verified[item.Relative].binding
		if wanted.inode == 0 || wanted != current {
			return fmt.Errorf("目标路径在校验后指向了不同文件，源已保留: %q", path)
		}
	}
	if err := op.Source.Remove(ctx, op.SourcePath, before.Items[0].Mode.IsDir()); err != nil {
		return accessError(fmt.Errorf("已复制并校验，但删除源失败: %w", err), true, op.SourcePath)
	}
	return nil
}

func syncMoveDestination(ctx context.Context, op Operation, before Manifest) error {
	syncer, ok := op.Destination.(interface {
		SyncPaths(context.Context, []string) error
	})
	if !ok {
		return fmt.Errorf("destination has no final sync capability: %w", errors.ErrUnsupported)
	}
	physical, err := physicalTarget(ctx, op)
	if err != nil {
		return err
	}
	if before.destination != nil && physical != before.destination.physicalTarget {
		return errors.New("目标父路径在校验后改变，源已保留")
	}
	op.TargetPath = physical
	seen := make(map[string]bool)
	var paths []string
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	// Files first, directories bottom-up, publication parent last. Sync only
	// copied entries, not unrelated merged files; links persist via parents.
	items := orderedForCopy(before.Items)
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		if item.Mode.IsRegular() || item.Mode.IsDir() {
			add(targetPath(op, item.Relative))
		}
	}
	if before.destination != nil {
		for _, parent := range before.destination.publication {
			add(parent)
		}
	} else {
		// Standalone FinishMove callers may supply only a source Snapshot.
		// Without pre-copy publication evidence we cannot safely infer which
		// ancestors were newly created, so retain the conservative barrier.
		for parent := op.Destination.Dir(op.TargetPath); ; parent = op.Destination.Dir(parent) {
			add(parent)
			if parent == op.Destination.Dir(parent) {
				break
			}
		}
	}
	return syncer.SyncPaths(ctx, paths)
}

// Ownership is opt-in only for an approved elevated destination. Lchown
// semantics are required so a copied symlink cannot change its referent owner.
func RestoreOwnership(ctx context.Context, op Operation, before Manifest) error {
	if !op.PreserveOwner {
		return nil
	}
	setter, capable := op.Destination.(interface {
		SetOwner(context.Context, string, uint32, uint32) error
	})
	items := orderedForCopy(before.Items)
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if !item.OwnerKnown {
			continue
		}
		if !capable {
			return errors.New("目标端不支持保留所有权")
		}
		if err := setter.SetOwner(ctx, targetPath(op, item.Relative), item.UID, item.GID); err != nil {
			return err
		}
	}
	return nil
}
