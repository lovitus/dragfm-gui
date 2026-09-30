package transfer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

type Operation struct {
	Source, Destination    endpoint.Endpoint
	SourcePath, TargetPath string
	Move, Overwrite        bool
	Progress               func(Progress)
	Baseline               *Manifest
	PreserveOwner          bool
}

type Progress struct {
	Stage      string
	Path       string
	BytesDone  int64
	BytesTotal int64
	FilesDone  int
	FilesTotal int
	Method     string
}

type Result struct {
	Copied       bool
	Moved        bool
	SourceKept   bool
	Bytes        int64
	Files        int
	Verification string
}

type Manifest struct {
	Items      []ManifestItem
	Bytes      int64
	RootDevice uint64
	RootInode  uint64
	// Controller-only evidence for this exact operation. Never serialized to
	// another machine or compared across unrelated filesystem identity domains.
	destination *destinationGuard
}

type ManifestItem struct {
	Relative   string
	Mode       fs.FileMode
	Size       int64
	ModifiedNS int64
	LinkTarget string
	SHA256     string
	SourcePath string
	UID, GID   uint32
	OwnerKnown bool
	// Strong snapshots retain per-entry identities in the controller only.
	// Destination identities are never compared with a different machine's.
	binding fileBinding
}

var ErrSourceChanged = PreserveSource(errors.New("源文件在传输期间发生变化"))

func Run(ctx context.Context, operation Operation) (out Result, retErr error) {
	if operation.Source == nil || operation.Destination == nil {
		return Result{}, errors.New("transfer endpoints are required")
	}
	if operation.SourcePath == "" || operation.TargetPath == "" {
		return Result{}, errors.New("source and target paths are required")
	}
	if err := validateOperationPaths(ctx, operation); err != nil {
		return Result{}, err
	}
	var before Manifest
	var err error
	if operation.Baseline != nil {
		before, err = SnapshotForOperation(ctx, operation)
		if err != nil {
			return Result{}, err
		}
	}
	if operation.Move {
		if result, done, err := tryNativeMove(ctx, operation); done {
			return result, err
		}
	} else if result, done, err := tryNativeCopy(ctx, operation); done {
		return result, err
	}
	strong := operation.Move
	emit(operation, Progress{Stage: "snapshot", Path: operation.SourcePath})
	if operation.Baseline == nil {
		before, err = SnapshotForOperation(ctx, operation)
	}
	if err != nil {
		return Result{}, fmt.Errorf("snapshot source: %w", err)
	}
	copyOperation, stagedRoot, err := prepareRootCopy(ctx, operation, before.Items[0])
	if err != nil {
		return Result{}, accessError(err, false, operation.TargetPath)
	}
	if stagedRoot != "" {
		defer func() {
			if stagedRoot != "" {
				retErr = cleanupStaged(operation.Destination, stagedRoot, before.Items[0].Mode.IsDir(), retErr)
			}
		}()
	}
	progress := Progress{Stage: "copy", BytesTotal: before.Bytes, FilesTotal: regularFileCount(before), Method: "controller-stream"}
	for _, item := range orderedForCopy(before.Items) {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		target := targetPath(copyOperation, item.Relative)
		if err := verifyCopyParents(ctx, copyOperation, target); err != nil {
			return Result{}, accessError(err, false, target)
		}
		progress.Path = item.Relative
		emit(operation, progress)
		if err := copyItem(ctx, copyOperation, item, target, &progress); err != nil {
			return Result{}, fmt.Errorf("copy %q: %w", item.Relative, accessError(err, false, target))
		}
	}
	if err := applyDirectoryMetadata(ctx, copyOperation, before.Items); err != nil {
		return Result{}, accessError(err, false, operation.TargetPath)
	}
	if err := RestoreOwnership(ctx, copyOperation, before); err != nil {
		return Result{}, accessError(err, false, operation.TargetPath)
	}
	if stagedRoot != "" {
		if err := commitStagedRoot(ctx, operation, stagedRoot, before.Items[0]); err != nil {
			return Result{}, fmt.Errorf("commit staged root: %w", accessError(err, false, operation.TargetPath))
		}
		stagedRoot = ""
	}

	result := Result{Copied: true, Bytes: before.Bytes, Files: regularFileCount(before)}
	if !strong {
		result.Verification = "atomic-write"
		return result, nil
	}
	emit(operation, Progress{Stage: "verify", BytesTotal: before.Bytes, FilesTotal: result.Files})
	if err := FinishMove(ctx, operation, before); err != nil {
		result.SourceKept = true
		return result, err
	}
	result.Moved = true
	result.Verification = "sha256"
	emit(operation, Progress{Stage: "done", BytesDone: before.Bytes, BytesTotal: before.Bytes, FilesDone: result.Files, FilesTotal: result.Files})
	return result, nil
}

func applyDirectoryMetadata(ctx context.Context, operation Operation, items []ManifestItem) error {
	ordered := orderedForCopy(items)
	for index := len(ordered) - 1; index >= 0; index-- {
		item := ordered[index]
		if !item.Mode.IsDir() {
			continue
		}
		target := targetPath(operation, item.Relative)
		if err := operation.Destination.Chmod(ctx, target, item.Mode); err != nil {
			return err
		}
		if err := operation.Destination.Chtimes(ctx, target, item.ModifiedTime(), item.ModifiedTime()); err != nil {
			return err
		}
	}
	return nil
}

func tryNativeCopy(ctx context.Context, operation Operation) (result Result, handled bool, retErr error) {
	sourceIdentity, sourceErr := operation.Source.Identity(ctx)
	targetIdentity, targetErr := operation.Destination.Identity(ctx)
	if sourceErr != nil || targetErr != nil || !endpoint.SameMachine(sourceIdentity, targetIdentity) {
		return Result{}, false, nil
	}
	copier, ok := operation.Source.(endpoint.NativeCopier)
	if !ok {
		return Result{}, false, nil
	}
	sourceInfo, err := operation.Source.Stat(ctx, operation.SourcePath)
	if err != nil {
		return Result{}, true, err
	}
	if targetInfo, statErr := operation.Destination.Stat(ctx, operation.TargetPath); statErr == nil {
		if !operation.Overwrite {
			return Result{}, true, fs.ErrExist
		}
		// Directory merges need per-file atomic writers, not cp truncating the
		// existing destination in place. Keep every old file until its copy is ready.
		if sourceInfo.IsDir() && targetInfo.IsDir() {
			return Result{}, false, nil
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return Result{}, true, statErr
	}
	staged, err := stagingPath(operation.TargetPath)
	if err != nil {
		return Result{}, true, err
	}
	defer func() {
		if staged == "" {
			return
		}
		retErr = cleanupStaged(operation.Source, staged, sourceInfo.IsDir(), retErr)
		if !Retryable(retErr) {
			handled = true // A live/uncleaned first writer forbids a second attempt.
		}
	}()
	emit(operation, Progress{Stage: "native-cp", Path: operation.SourcePath, Method: "cp"})
	if err := copier.CopyNative(ctx, operation.SourcePath, staged, sourceInfo.IsDir(), false); err != nil {
		if ctx.Err() != nil || !Retryable(err) {
			return Result{}, true, errors.Join(ctx.Err(), err)
		}
		return Result{}, false, err // Different users may require destination-side writes.
	}
	if operation.PreserveOwner {
		manifest, err := SnapshotForOperation(ctx, operation)
		if err != nil {
			return Result{}, true, err
		}
		stagedOp := operation
		stagedOp.TargetPath = staged
		if err := RestoreOwnership(ctx, stagedOp, manifest); err != nil {
			return Result{}, true, err
		}
	}
	if err := operation.Destination.Rename(ctx, staged, operation.TargetPath, operation.Overwrite); err != nil {
		return Result{}, true, err
	}
	staged = ""
	return Result{Copied: true, Files: 1, Bytes: sourceInfo.Size, Verification: "same-machine-cp"}, true, nil
}

func tryNativeMove(ctx context.Context, operation Operation) (Result, bool, error) {
	sourceIdentity, sourceErr := operation.Source.Identity(ctx)
	targetIdentity, targetErr := operation.Destination.Identity(ctx)
	if sourceErr != nil || targetErr != nil || !endpoint.SameMachine(sourceIdentity, targetIdentity) {
		return Result{}, false, nil
	}
	sourceInfo, err := operation.Source.Stat(ctx, operation.SourcePath)
	if err != nil {
		return Result{}, true, err
	}
	targetInfo, targetErr := operation.Destination.Stat(ctx, operation.TargetPath)
	if targetErr == nil {
		if !operation.Overwrite {
			return Result{}, true, fs.ErrExist
		}
		if sourceInfo.Mode.IsDir() && targetInfo.Mode.IsDir() {
			return Result{}, false, nil
		}
	} else if !errors.Is(targetErr, fs.ErrNotExist) {
		return Result{}, true, targetErr
	}
	emit(operation, Progress{Stage: "native-mv", Path: operation.SourcePath, Method: "mv"})
	if err := operation.Source.Rename(ctx, operation.SourcePath, operation.TargetPath, operation.Overwrite); err != nil {
		if !Retryable(err) {
			return Result{}, true, err
		}
		return Result{}, false, nil
	}
	return Result{Copied: true, Moved: true, Files: 1, Bytes: sourceInfo.Size, Verification: "same-machine-rename"}, true, nil
}

func Snapshot(ctx context.Context, source endpoint.Endpoint, root string, hashFiles bool) (Manifest, error) {
	entry, err := source.Stat(ctx, root)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{}
	if provider, ok := source.(interface {
		FileVersion(context.Context, string) (uint64, uint64, error)
	}); ok {
		manifest.RootDevice, manifest.RootInode, err = provider.FileVersion(ctx, root)
		if err != nil || manifest.RootInode == 0 {
			if hashFiles {
				if err != nil {
					return Manifest{}, fmt.Errorf("read file identity %q: %w", root, err)
				}
				return Manifest{}, fmt.Errorf("file identity is unavailable for %q", root)
			}
			// Non-destructive previews/copies can use SFTP-only accounts.
			// This is unknown identity, never a match for a known baseline.
			manifest.RootDevice, manifest.RootInode = 0, 0
		}
	}
	if err := snapshotItem(ctx, source, root, "", entry, hashFiles, &manifest); err != nil {
		return Manifest{}, err
	}
	sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].Relative < manifest.Items[j].Relative })
	return manifest, nil
}

func snapshotItem(ctx context.Context, source endpoint.Endpoint, path, relative string, entry endpoint.Entry, hashFiles bool, manifest *Manifest) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	item := ManifestItem{Relative: relative, Mode: entry.Mode, Size: entry.Size, ModifiedNS: entry.Modified.UnixNano(), LinkTarget: entry.LinkTarget, SourcePath: path, UID: entry.UID, GID: entry.GID, OwnerKnown: entry.OwnerKnown}
	if hashFiles && manifest.RootInode != 0 {
		item.binding = fileBinding{manifest.RootDevice, manifest.RootInode}
		if relative != "" {
			var err error
			item.binding, err = bindingAt(ctx, source, path)
			if err != nil {
				return fmt.Errorf("read file identity %q: %w", path, err)
			}
		}
		// Bind the hash/list operation to the object we started reading. A
		// pathname replaced during that read must not become verified evidence.
		defer func() {
			if retErr != nil {
				return
			}
			current, err := bindingAt(ctx, source, path)
			if err != nil {
				retErr = fmt.Errorf("confirm file identity %q: %w", path, err)
			} else if current != item.binding {
				retErr = fmt.Errorf("file replaced during snapshot: %q", path)
			}
		}()
	}
	if entry.Mode.IsRegular() {
		manifest.Bytes += entry.Size
		if hashFiles {
			hash, err := hashFile(ctx, source, path)
			if err != nil {
				return err
			}
			item.SHA256 = hash
		}
	}
	manifest.Items = append(manifest.Items, item)
	if !entry.Mode.IsDir() {
		return nil
	}
	children, err := source.List(ctx, path)
	if err != nil {
		return err
	}
	for _, child := range children {
		childRelative := child.Name
		if relative != "" {
			childRelative = relative + "/" + child.Name
		}
		if err := snapshotItem(ctx, source, child.Path, childRelative, child, hashFiles, manifest); err != nil {
			return err
		}
	}
	return nil
}

func copyItem(ctx context.Context, operation Operation, item ManifestItem, target string, progress *Progress) (retErr error) {
	if item.Mode.IsDir() {
		if err := ensureCopyDirectory(ctx, operation.Destination, target); err != nil {
			return err
		}
		return nil
	}
	if item.Mode&fs.ModeSymlink != 0 {
		// Never unlink the previous destination before the new link exists.
		if err := operation.Destination.MkdirAll(ctx, operation.Destination.Dir(target), 0700); err != nil {
			return err
		}
		staged, err := stagingPath(target)
		if err != nil {
			return err
		}
		if err := operation.Destination.Symlink(ctx, item.LinkTarget, staged); err != nil {
			return err
		}
		if err := operation.Destination.Rename(ctx, staged, target, operation.Overwrite); err != nil {
			return cleanupStaged(operation.Destination, staged, false, err)
		}
		return nil
	}
	if !item.Mode.IsRegular() {
		return fmt.Errorf("unsupported file type %s", item.Mode.Type())
	}
	if err := operation.Destination.MkdirAll(ctx, operation.Destination.Dir(target), 0700); err != nil {
		return err
	}
	if !operation.Overwrite {
		if _, err := operation.Destination.Stat(ctx, target); err == nil {
			return fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	reader, err := operation.Source.Open(ctx, item.SourcePath)
	if err != nil {
		return accessError(err, true, item.SourcePath)
	}
	defer reader.Close()
	writer, err := operation.Destination.CreateAtomic(ctx, target, item.Mode)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if err := writer.Abort(); err != nil {
				retErr = PreserveSource(errors.Join(retErr, fmt.Errorf("abort destination %q: %w", target, err)))
			}
		}
	}()
	buffer := make([]byte, 256*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			copied += int64(count)
			if copied > item.Size {
				return ErrSourceChanged
			}
			written, writeErr := writer.Write(buffer[:count])
			progress.BytesDone += int64(written)
			emit(operation, *progress)
			if writeErr != nil {
				return writeErr
			}
			if written != count {
				return io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return accessError(readErr, true, item.SourcePath)
		}
	}
	if copied != item.Size {
		return ErrSourceChanged
	}
	if err := writer.Commit(); err != nil {
		return err
	}
	committed = true
	if err := operation.Destination.Chmod(ctx, target, item.Mode); err != nil {
		return err
	}
	if err := operation.Destination.Chtimes(ctx, target, item.ModifiedTime(), item.ModifiedTime()); err != nil {
		return err
	}
	progress.FilesDone++
	return nil
}

// Writer-exit evidence is not interchangeable with a closed connection. Do
// not erase a staged tree around a possibly live child, and do not hide a
// cleanup failure behind the copy error or start another route over it.
func cleanupStaged(destination endpoint.Endpoint, path string, directory bool, failure error) error {
	if errors.Is(failure, endpoint.ErrCommandExitUnconfirmed) {
		return PreserveSource(fmt.Errorf("暂存路径仍可能被写入，已保留 %q: %w", path, failure))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := destination.Remove(ctx, path, directory); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return PreserveSource(errors.Join(failure, fmt.Errorf("清理暂存路径失败 %q: %w", path, err)))
	}
	return failure
}

func (item ManifestItem) ModifiedTime() (value time.Time) {
	return time.Unix(0, item.ModifiedNS)
}

func hashFile(ctx context.Context, source endpoint.Endpoint, path string) (string, error) {
	reader, err := source.Open(ctx, path)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	hash := sha256.New()
	buffer := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func CompareManifests(expected, actual Manifest, ignoreModified bool) error {
	if !ignoreModified && expected.RootInode != 0 {
		if actual.RootInode == 0 {
			return errors.New("source root identity can no longer be confirmed")
		}
		if expected.RootDevice != actual.RootDevice || expected.RootInode != actual.RootInode {
			return errors.New("source root identity changed")
		}
	}
	if len(expected.Items) == 0 || len(actual.Items) == 0 {
		return errors.New("empty verification manifest")
	}
	if !ignoreModified && len(expected.Items) != len(actual.Items) {
		return errors.New("source entry set changed")
	}
	actualByRelative := make(map[string]ManifestItem, len(actual.Items))
	for _, item := range actual.Items {
		if _, exists := actualByRelative[item.Relative]; exists {
			return fmt.Errorf("duplicate manifest entry %q", item.Relative)
		}
		actualByRelative[item.Relative] = item
	}
	seen := make(map[string]bool, len(expected.Items))
	for _, wanted := range expected.Items {
		if seen[wanted.Relative] {
			return fmt.Errorf("duplicate source entry %q", wanted.Relative)
		}
		seen[wanted.Relative] = true
		got, ok := actualByRelative[wanted.Relative]
		if !ok {
			return fmt.Errorf("missing %q", wanted.Relative)
		}
		if wanted.Mode.Type() != got.Mode.Type() || (wanted.Mode.IsRegular() && wanted.Size != got.Size) || wanted.LinkTarget != got.LinkTarget || wanted.SHA256 != got.SHA256 {
			return fmt.Errorf("content mismatch %q", wanted.Relative)
		}
		if !ignoreModified && (wanted.ModifiedNS != got.ModifiedNS || wanted.Mode.Perm() != got.Mode.Perm()) {
			return fmt.Errorf("source metadata changed %q", wanted.Relative)
		}
		if !ignoreModified && wanted.binding.inode != 0 && wanted.binding != got.binding {
			return fmt.Errorf("source entry identity changed %q", wanted.Relative)
		}
		if !ignoreModified && wanted.OwnerKnown && (!got.OwnerKnown || wanted.UID != got.UID || wanted.GID != got.GID) {
			return fmt.Errorf("source ownership changed %q", wanted.Relative)
		}
	}
	return nil
}

func prepareRootCopy(ctx context.Context, operation Operation, root ManifestItem) (Operation, string, error) {
	existing, err := operation.Destination.Stat(ctx, operation.TargetPath)
	if errors.Is(err, fs.ErrNotExist) {
		staged, stageErr := stagingPath(operation.TargetPath)
		if stageErr != nil {
			return Operation{}, "", stageErr
		}
		copy := operation
		copy.TargetPath, copy.Overwrite = staged, false
		return copy, staged, nil
	}
	if err != nil {
		return Operation{}, "", err
	}
	if !operation.Overwrite {
		return Operation{}, "", fs.ErrExist
	}
	if root.Mode.IsDir() && existing.Mode.IsDir() {
		return operation, "", nil
	}
	staged, stageErr := stagingPath(operation.TargetPath)
	if stageErr != nil {
		return Operation{}, "", stageErr
	}
	copy := operation
	copy.TargetPath, copy.Overwrite = staged, false
	return copy, staged, nil
}

func commitStagedRoot(ctx context.Context, operation Operation, staged string, root ManifestItem) error {
	// A failed rename (permissions, cancellation, incompatible file types) is
	// not permission to delete the old destination and retry destructively.
	return operation.Destination.Rename(ctx, staged, operation.TargetPath, operation.Overwrite)
}

func stagingPath(target string) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return target + ".dragfm-partial-" + hex.EncodeToString(nonce[:]), nil
}

func validateOperationPaths(ctx context.Context, operation Operation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sourcePath, err := operation.Source.Abs(ctx, operation.SourcePath)
	if err != nil {
		return err
	}
	targetPath, err := operation.Destination.Abs(ctx, operation.TargetPath)
	if err != nil {
		return err
	}
	if sourcePath != operation.SourcePath || targetPath != operation.TargetPath {
		return errors.New("transfer paths must be absolute and normalized")
	}
	if operation.Source.Dir(sourcePath) == sourcePath || operation.Destination.Dir(targetPath) == targetPath {
		return errors.New("refusing to transfer a filesystem root")
	}
	sourceID, sourceErr := operation.Source.Identity(ctx)
	targetID, targetErr := operation.Destination.Identity(ctx)
	if sourceErr != nil || targetErr != nil || !endpoint.SameMachine(sourceID, targetID) {
		return nil
	}
	sourcePath, targetPath, err = physicalOperationPaths(ctx, operation, sourcePath, targetPath)
	if err != nil {
		return err
	}
	if sourcePath == targetPath {
		return errors.New("source and destination are the same path")
	}
	// Endpoint-aware ancestry checks work with both POSIX and Windows paths.
	for parent := operation.Destination.Dir(targetPath); ; parent = operation.Destination.Dir(parent) {
		if parent == sourcePath {
			return errors.New("destination is inside the source tree")
		}
		if parent == operation.Destination.Dir(parent) {
			break
		}
	}
	return nil
}

func targetPath(operation Operation, relative string) string {
	if relative == "" {
		return operation.TargetPath
	}
	parts := append([]string{operation.TargetPath}, strings.Split(relative, "/")...)
	return operation.Destination.Join(parts...)
}

func orderedForCopy(items []ManifestItem) []ManifestItem {
	result := append([]ManifestItem(nil), items...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Mode.IsDir() != result[j].Mode.IsDir() {
			return result[i].Mode.IsDir()
		}
		return strings.Count(result[i].Relative, "/") < strings.Count(result[j].Relative, "/")
	})
	return result
}

func regularFileCount(manifest Manifest) int {
	count := 0
	for _, item := range manifest.Items {
		if item.Mode.IsRegular() {
			count++
		}
	}
	return count
}

func emit(operation Operation, progress Progress) {
	if operation.Progress != nil {
		operation.Progress(progress)
	}
}
