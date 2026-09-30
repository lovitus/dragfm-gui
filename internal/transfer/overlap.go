package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

type fileBinding struct{ device, inode uint64 }

func bindingAt(ctx context.Context, ep endpoint.Endpoint, path string) (fileBinding, error) {
	provider, ok := ep.(interface {
		FileVersion(context.Context, string) (uint64, uint64, error)
	})
	if !ok {
		return fileBinding{}, fmt.Errorf("cannot verify filesystem binding: %w", errors.ErrUnsupported)
	}
	device, inode, err := provider.FileVersion(ctx, path)
	if err != nil {
		return fileBinding{}, err
	}
	if inode == 0 {
		return fileBinding{}, errors.New("filesystem binding is unknown")
	}
	return fileBinding{device, inode}, nil
}

type overlapProbe struct {
	writer             endpoint.StagedWriter
	path, parent, name string
	token              []byte
	file, directory    fileBinding
	before, written    endpoint.Entry
	ancestor           bool // This carrier is an actual copied directory, not just a parent.
}

// checkDirectoryOverlap is separate from SameMachine. A failed fast-path
// identity comparison does not imply disjoint storage. The confirmed task
// uses unpublished, exclusively created atomic temporaries to detect shared
// visibility through the existing file views, including different users,
// chroots and differing absolute path spellings. No extra connection is made.
// This is a check of the current directory bindings, not a lock against later
// administrative mount changes or a guarantee about incoherent network caches.
func checkDirectoryOverlap(ctx context.Context, op Operation, source Manifest, guard *destinationGuard) (retErr error) {
	if len(source.Items) == 0 || !source.Items[0].Mode.IsDir() {
		return nil
	}
	op.TargetPath = guard.physicalTarget
	// Map each copied directory to its nearest existing destination directory.
	// New trees need only one carrier; directory merges may have several.
	carriers := make(map[string]bool)
	var order []string
	for _, item := range source.Items {
		if !item.Mode.IsDir() {
			continue
		}
		candidate := targetPath(op, item.Relative)
		exact := true
		for {
			if previous, exists := carriers[candidate]; exists {
				carriers[candidate] = previous || exact
				break
			}
			info, err := op.Destination.Stat(ctx, candidate)
			if err == nil && info.IsDir() {
				carriers[candidate] = exact
				order = append(order, candidate)
				break
			}
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return accessError(err, false, candidate)
			}
			if candidate == op.Destination.Dir(candidate) {
				return errors.New("no accessible destination directory for overlap check")
			}
			candidate, exact = op.Destination.Dir(candidate), false
		}
	}
	var probes []*overlapProbe
	byName := make(map[string]*overlapProbe)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for i := len(probes) - 1; i >= 0; i-- {
			if err := probes[i].remove(cleanup, op.Destination); err != nil {
				retErr = PreserveSource(errors.Join(retErr, fmt.Errorf("重叠检查临时文件清理未确认，源已保留: %w", err)))
			}
		}
	}()
	for _, parent := range order {
		probe, err := makeOverlapProbe(ctx, op.Destination, parent)
		if probe != nil {
			probes = append(probes, probe)
			probe.ancestor = carriers[parent]
		}
		if err != nil {
			return accessError(err, false, parent)
		}
		if _, collision := byName[probe.name]; collision {
			return PreserveSource(errors.New("overlap probe basename collision"))
		}
		byName[probe.name] = probe
		guard.targetDirectories[parent] = probe.directory
	}
	visited := make(map[string]bool)
	inspect := func(directory string, ancestor bool) error {
		if visited[directory] {
			return nil
		}
		visited[directory] = true
		if ancestor {
			// Search permission suffices for checking a known nonce name.
			// Listing an unrelated 0711 ancestor would demand extra access.
			for _, probe := range probes {
				if !probe.ancestor {
					continue
				}
				path := op.Source.Join(directory, probe.name)
				if _, err := op.Source.Stat(ctx, path); errors.Is(err, fs.ErrNotExist) {
					continue
				} else if err != nil {
					return PreserveSource(fmt.Errorf("无法排除源/目标祖先重叠，源已保留: %w", err))
				}
				if err := readProbe(ctx, op.Source, path, probe.token); err != nil {
					return PreserveSource(fmt.Errorf("两端出现相同探针但无法确认独立性，源已保留: %w", err))
				}
				return PreserveSource(errors.New("源和目标目录实际重叠；已阻止复制/移动，源已保留"))
			}
			return nil
		}
		entries, err := op.Source.List(ctx, directory)
		if err != nil {
			return PreserveSource(fmt.Errorf("无法排除源/目标重叠，源已保留: %w", err))
		}
		for _, entry := range entries {
			probe := byName[entry.Name]
			if probe == nil || ancestor && !probe.ancestor {
				continue
			}
			if err := readProbe(ctx, op.Source, entry.Path, probe.token); err != nil {
				return PreserveSource(fmt.Errorf("两端出现相同探针但无法确认独立性，源已保留: %w", err))
			}
			return PreserveSource(errors.New("源和目标目录实际重叠；已阻止复制/移动，源已保留"))
		}
		return nil
	}
	for _, item := range source.Items {
		if item.Mode.IsDir() {
			if err := inspect(item.SourcePath, false); err != nil {
				return err
			}
		}
	}
	// An existing destination may also be an ancestor of source. A carrier
	// that is merely the parent of an absent destination does not prove this:
	// legitimate sibling transfers naturally share that parent.
	for _, existing := range carriers {
		if !existing {
			continue
		}
		for parent := op.Source.Dir(op.SourcePath); ; parent = op.Source.Dir(parent) {
			if err := inspect(parent, true); err != nil {
				return err
			}
			if parent == op.Source.Dir(parent) {
				break
			}
		}
		break
	}
	return nil
}

func makeOverlapProbe(ctx context.Context, ep endpoint.Endpoint, parent string) (*overlapProbe, error) {
	before, err := ep.Stat(ctx, parent)
	if err != nil || !before.IsDir() {
		return nil, errors.Join(err, errors.New("overlap carrier is not a directory"))
	}
	directory, err := bindingAt(ctx, ep, parent)
	if err != nil {
		return nil, err
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	// The nominal target is never committed. The writer itself chooses and
	// exclusively creates the real random partial; a collision is not replaced.
	writer, err := ep.CreateAtomic(ctx, ep.Join(parent, ".dragfm-overlap-unpublished"), 0600)
	if err != nil {
		return nil, err
	}
	staged, ok := writer.(endpoint.StagedWriter)
	if !ok {
		unavailable := errors.New("file view cannot expose an acknowledged unpublished stage")
		if err := writer.Abort(); err != nil {
			return nil, PreserveSource(errors.Join(unavailable, err))
		}
		return nil, unavailable
	}
	probe := &overlapProbe{writer: staged, parent: parent, token: token, directory: directory, before: before}
	written, writeErr := staged.Write(token)
	if writeErr == nil && written != len(token) {
		writeErr = io.ErrShortWrite
	}
	probe.path, err = staged.PrepareStaged()
	if writeErr != nil || err != nil {
		return probe, PreserveSource(errors.Join(writeErr, err))
	}
	if ep.Dir(probe.path) != parent || probe.path == parent {
		return probe, PreserveSource(errors.New("unpublished stage escaped its carrier"))
	}
	probe.name = strings.TrimPrefix(probe.path, parent)
	probe.name = strings.TrimLeft(probe.name, `/\`)
	probe.file, err = bindingAt(ctx, ep, probe.path)
	if err == nil {
		err = readProbe(ctx, ep, probe.path, token)
	}
	if err == nil {
		probe.written, err = ep.Stat(ctx, parent)
	}
	if err != nil {
		return probe, PreserveSource(err)
	}
	return probe, nil
}

func readProbe(ctx context.Context, ep endpoint.Endpoint, path string, token []byte) error {
	info, err := ep.Stat(ctx, path)
	if err != nil {
		return err
	}
	if !info.Mode.IsRegular() || info.Size != int64(len(token)) {
		return errors.New("overlap probe type or size changed")
	}
	reader, err := ep.Open(ctx, path)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, int64(len(token)+1)))
	if err := errors.Join(readErr, reader.Close()); err != nil {
		return err
	}
	if !bytes.Equal(data, token) {
		return errors.New("overlap probe contents changed")
	}
	return nil
}

func (p *overlapProbe) remove(ctx context.Context, ep endpoint.Endpoint) error {
	// A failed prepare/identity query is not permission to blindly unlink a
	// pathname. The writer is already closed/joined; retain its owned journal.
	if p.path == "" || p.file.inode == 0 {
		return fmt.Errorf("unconfirmed probe retained at %q", p.path)
	}
	parent, err := bindingAt(ctx, ep, p.parent)
	if err != nil || parent != p.directory {
		return errors.Join(err, errors.New("probe parent binding changed; retained temporary"))
	}
	file, err := bindingAt(ctx, ep, p.path)
	if err != nil || file != p.file {
		return errors.Join(err, errors.New("probe ownership changed; retained temporary"))
	}
	if err := readProbe(ctx, ep, p.path, p.token); err != nil {
		return err
	}
	current, err := ep.Stat(ctx, p.parent)
	if err != nil {
		return err
	}
	changed := !current.Modified.Equal(p.written.Modified)
	if err := p.writer.Abort(); err != nil {
		return err
	}
	parent, err = bindingAt(ctx, ep, p.parent)
	if err != nil || parent != p.directory {
		return errors.Join(err, errors.New("probe carrier changed concurrently; metadata not restored"))
	}
	// A pre-existing carrier above an absent target is not itself being
	// copied. Creating/removing our entry is legitimate in a shared 1777
	// directory, but setting its old timestamp requires ownership we may not
	// have. Do not introduce that permission or hide unrelated entry changes.
	if !p.ancestor {
		return nil
	}
	if changed {
		return errors.New("copied directory changed concurrently; metadata not restored")
	}
	// Namespace events/ctime cannot be undone. Restore only the task's known
	// directory mtime when no concurrent namespace change was observed. The
	// final move durability barrier comes after this acknowledged cleanup.
	return ep.Chtimes(ctx, p.parent, p.before.Modified, p.before.Modified)
}
