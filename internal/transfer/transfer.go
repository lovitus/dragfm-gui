package transfer

import (
	"context"
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
}

type ManifestItem struct {
	Relative   string
	Mode       fs.FileMode
	Size       int64
	ModifiedNS int64
	LinkTarget string
	SHA256     string
	SourcePath string
}

var ErrSourceChanged = errors.New("源文件在传输期间发生变化")

func Run(ctx context.Context, operation Operation) (Result, error) {
	if operation.Source == nil || operation.Destination == nil {
		return Result{}, errors.New("transfer endpoints are required")
	}
	if operation.SourcePath == "" || operation.TargetPath == "" {
		return Result{}, errors.New("source and target paths are required")
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
	before, err := Snapshot(ctx, operation.Source, operation.SourcePath, strong)
	if err != nil {
		return Result{}, fmt.Errorf("snapshot source: %w", err)
	}
	copyOperation, stagedRoot, err := prepareRootCopy(ctx, operation, before.Items[0])
	if err != nil {
		return Result{}, err
	}
	if stagedRoot != "" {
		defer func() {
			if stagedRoot != "" {
				_ = operation.Destination.Remove(context.Background(), stagedRoot, before.Items[0].Mode.IsDir())
			}
		}()
	}
	progress := Progress{Stage: "copy", BytesTotal: before.Bytes, FilesTotal: regularFileCount(before), Method: "controller-stream"}
	for _, item := range orderedForCopy(before.Items) {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		target := targetPath(copyOperation, item.Relative)
		progress.Path = item.Relative
		emit(operation, progress)
		if err := copyItem(ctx, copyOperation, item, target, &progress); err != nil {
			return Result{}, fmt.Errorf("copy %q: %w", item.Relative, err)
		}
	}
	applyDirectoryMetadata(ctx, copyOperation, before.Items)
	if stagedRoot != "" {
		if err := commitStagedRoot(ctx, operation, stagedRoot, before.Items[0]); err != nil {
			return Result{}, fmt.Errorf("commit staged root: %w", err)
		}
		stagedRoot = ""
	}

	result := Result{Copied: true, Bytes: before.Bytes, Files: regularFileCount(before)}
	if !strong {
		result.Verification = "atomic-write"
		return result, nil
	}
	emit(operation, Progress{Stage: "verify", BytesTotal: before.Bytes, FilesTotal: result.Files})
	afterSource, err := Snapshot(ctx, operation.Source, operation.SourcePath, true)
	if err != nil {
		return Result{Copied: true, SourceKept: true, Bytes: result.Bytes, Files: result.Files}, fmt.Errorf("re-snapshot source: %w", err)
	}
	if err := CompareManifests(before, afterSource, false); err != nil {
		return Result{Copied: true, SourceKept: true, Bytes: result.Bytes, Files: result.Files}, errors.Join(ErrSourceChanged, err)
	}
	afterTarget, err := Snapshot(ctx, operation.Destination, operation.TargetPath, true)
	if err != nil {
		return Result{Copied: true, SourceKept: true, Bytes: result.Bytes, Files: result.Files}, fmt.Errorf("snapshot target: %w", err)
	}
	if err := CompareManifests(before, afterTarget, true); err != nil {
		return Result{Copied: true, SourceKept: true, Bytes: result.Bytes, Files: result.Files}, fmt.Errorf("目标校验失败，源文件已保留: %w", err)
	}
	if err := operation.Source.Remove(ctx, operation.SourcePath, before.Items[0].Mode.IsDir()); err != nil {
		return Result{Copied: true, SourceKept: true, Bytes: result.Bytes, Files: result.Files, Verification: "sha256"}, fmt.Errorf("已复制并校验，但删除源失败: %w", err)
	}
	result.Moved = true
	result.Verification = "sha256"
	emit(operation, Progress{Stage: "done", BytesDone: before.Bytes, BytesTotal: before.Bytes, FilesDone: result.Files, FilesTotal: result.Files})
	return result, nil
}

func applyDirectoryMetadata(ctx context.Context, operation Operation, items []ManifestItem) {
	ordered := orderedForCopy(items)
	for index := len(ordered) - 1; index >= 0; index-- {
		item := ordered[index]
		if !item.Mode.IsDir() {
			continue
		}
		target := targetPath(operation, item.Relative)
		_ = operation.Destination.Chmod(ctx, target, item.Mode)
		_ = operation.Destination.Chtimes(ctx, target, item.ModifiedTime(), item.ModifiedTime())
	}
}

func tryNativeCopy(ctx context.Context, operation Operation) (Result, bool, error) {
	sourceIdentity, sourceErr := operation.Source.Identity(ctx)
	targetIdentity, targetErr := operation.Destination.Identity(ctx)
	if sourceErr != nil || targetErr != nil || sourceIdentity.MachineID == "" || sourceIdentity.MachineID != targetIdentity.MachineID {
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
	merge := false
	if targetInfo, statErr := operation.Destination.Stat(ctx, operation.TargetPath); statErr == nil {
		if !operation.Overwrite {
			return Result{}, true, fs.ErrExist
		}
		merge = sourceInfo.Mode.IsDir() && targetInfo.Mode.IsDir()
		if !merge {
			if err := operation.Destination.Remove(ctx, operation.TargetPath, targetInfo.Mode.IsDir()); err != nil {
				return Result{}, true, err
			}
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return Result{}, true, statErr
	}
	emit(operation, Progress{Stage: "native-cp", Path: operation.SourcePath, Method: "cp"})
	if err := copier.CopyNative(ctx, operation.SourcePath, operation.TargetPath, sourceInfo.Mode.IsDir(), merge); err != nil {
		// A same-machine identity can still represent different users. If cp is
		// denied, retain the controller-stream fallback which writes using the
		// destination endpoint's credentials.
		return Result{}, false, nil
	}
	return Result{Copied: true, Files: 1, Bytes: sourceInfo.Size, Verification: "same-machine-cp"}, true, nil
}

func tryNativeMove(ctx context.Context, operation Operation) (Result, bool, error) {
	sourceIdentity, sourceErr := operation.Source.Identity(ctx)
	targetIdentity, targetErr := operation.Destination.Identity(ctx)
	if sourceErr != nil || targetErr != nil || sourceIdentity.MachineID == "" || sourceIdentity.MachineID != targetIdentity.MachineID {
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
		manifest.RootDevice, manifest.RootInode, _ = provider.FileVersion(ctx, root)
	}
	if err := snapshotItem(ctx, source, root, "", entry, hashFiles, &manifest); err != nil {
		return Manifest{}, err
	}
	sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].Relative < manifest.Items[j].Relative })
	return manifest, nil
}

func snapshotItem(ctx context.Context, source endpoint.Endpoint, path, relative string, entry endpoint.Entry, hashFiles bool, manifest *Manifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	item := ManifestItem{Relative: relative, Mode: entry.Mode, Size: entry.Size, ModifiedNS: entry.Modified.UnixNano(), LinkTarget: entry.LinkTarget, SourcePath: path}
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

func copyItem(ctx context.Context, operation Operation, item ManifestItem, target string, progress *Progress) error {
	if item.Mode.IsDir() {
		if err := operation.Destination.MkdirAll(ctx, target, item.Mode); err != nil {
			return err
		}
		return nil
	}
	if item.Mode&fs.ModeSymlink != 0 {
		if operation.Overwrite {
			_ = operation.Destination.Remove(ctx, target, false)
		} else if _, err := operation.Destination.Stat(ctx, target); err == nil {
			return fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return operation.Destination.Symlink(ctx, item.LinkTarget, target)
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
		return err
	}
	defer reader.Close()
	writer, err := operation.Destination.CreateAtomic(ctx, target, item.Mode)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = writer.Abort()
		}
	}()
	buffer := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
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
			return readErr
		}
	}
	if err := writer.Commit(); err != nil {
		return err
	}
	committed = true
	_ = operation.Destination.Chmod(ctx, target, item.Mode)
	_ = operation.Destination.Chtimes(ctx, target, item.ModifiedTime(), item.ModifiedTime())
	progress.FilesDone++
	return nil
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
	if !ignoreModified && expected.RootInode != 0 && actual.RootInode != 0 && (expected.RootDevice != actual.RootDevice || expected.RootInode != actual.RootInode) {
		return errors.New("source root identity changed")
	}
	actualByRelative := make(map[string]ManifestItem, len(actual.Items))
	for _, item := range actual.Items {
		actualByRelative[item.Relative] = item
	}
	for _, wanted := range expected.Items {
		got, ok := actualByRelative[wanted.Relative]
		if !ok {
			return fmt.Errorf("missing %q", wanted.Relative)
		}
		if wanted.Mode.Type() != got.Mode.Type() || (wanted.Mode.IsRegular() && wanted.Size != got.Size) || wanted.LinkTarget != got.LinkTarget || wanted.SHA256 != got.SHA256 {
			return fmt.Errorf("content mismatch %q", wanted.Relative)
		}
		if !ignoreModified && wanted.ModifiedNS != got.ModifiedNS {
			return fmt.Errorf("mtime changed %q", wanted.Relative)
		}
	}
	return nil
}

func prepareRootCopy(ctx context.Context, operation Operation, root ManifestItem) (Operation, string, error) {
	existing, err := operation.Destination.Stat(ctx, operation.TargetPath)
	if errors.Is(err, fs.ErrNotExist) {
		staged := operation.TargetPath + ".dragfm-partial-" + fmt.Sprintf("%d", time.Now().UnixNano())
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
	staged := operation.TargetPath + ".dragfm-partial-" + fmt.Sprintf("%d", time.Now().UnixNano())
	copy := operation
	copy.TargetPath, copy.Overwrite = staged, false
	return copy, staged, nil
}

func commitStagedRoot(ctx context.Context, operation Operation, staged string, root ManifestItem) error {
	if err := operation.Destination.Rename(ctx, staged, operation.TargetPath, operation.Overwrite); err == nil {
		return nil
	} else if !operation.Overwrite {
		return err
	}
	existing, statErr := operation.Destination.Stat(ctx, operation.TargetPath)
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	if statErr == nil {
		if removeErr := operation.Destination.Remove(ctx, operation.TargetPath, existing.Mode.IsDir()); removeErr != nil {
			return removeErr
		}
	}
	return operation.Destination.Rename(ctx, staged, operation.TargetPath, false)
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
