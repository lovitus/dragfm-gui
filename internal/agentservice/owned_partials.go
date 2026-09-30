package agentservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lovitus/dragfm-gui/internal/config"
)

var partialName = regexp.MustCompile(`(?:^|\.)dragfm-partial-[0-9a-f]{16,64}$`)

type ownedPartial struct {
	parent *os.Root
	name   string
	pinned os.FileInfo
}

func (s *Service) trackPartial(path string) error {
	// Compatibility: ordinary agent file operations do not become cleanup targets.
	// Only controller-generated staging names are owned by the helper lifecycle.
	if !filepath.IsAbs(path) || !partialName.MatchString(filepath.Base(path)) {
		return nil
	}
	path = filepath.Clean(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.partials == nil {
		s.partials = make(map[string]*ownedPartial)
	}
	if s.partials[path] != nil {
		return nil
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	if _, err := parent.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		_ = parent.Close()
		if err == nil {
			return os.ErrExist
		}
		return err
	}
	s.partials[path] = &ownedPartial{parent: parent, name: name}
	return nil
}
func (s *Service) forgetPartial(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path = filepath.Clean(path)
	for name, owned := range s.partials {
		if name == path || strings.HasPrefix(name, path+string(filepath.Separator)) {
			delete(s.partials, name)
			_ = owned.parent.Close()
		}
	}
}

// Report real metadata through the authenticated control protocol. No task
// path/credential journal is written on the remote host. Pin the first observed
// inode; a replacement must never be silently adopted by a later heartbeat.
func (s *Service) partialRecords() ([]config.PartialRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]config.PartialRecord, 0, len(s.partials))
	for path, owned := range s.partials {
		parent, err := owned.parent.Stat(".")
		if err != nil {
			return nil, err
		}
		device, inode := filesystemVersion(parent)
		if inode == 0 {
			return nil, errors.New("partial parent has no filesystem identity")
		}
		record := config.PartialRecord{Path: path, ParentID: fmt.Sprintf("%d:%d", device, inode)}
		info, err := owned.parent.Lstat(owned.name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			if owned.pinned != nil && !os.SameFile(owned.pinned, info) {
				return nil, fmt.Errorf("owned partial %q was replaced; retained", path)
			}
			owned.pinned = info
		}
		if owned.pinned != nil {
			device, inode := filesystemVersion(owned.pinned)
			if inode == 0 {
				return nil, errors.New("partial has no filesystem identity")
			}
			record.FileID = fmt.Sprintf("%d:%d", device, inode)
		}
		records = append(records, record)
	}
	// A private staged tree's pinned root already covers its descendant
	// partials. Keep their in-memory handles, but do not rewrite the vault for
	// each contained file; only independent merge targets need separate pins.
	sort.Slice(records, func(i, j int) bool { return len(records[i].Path) < len(records[j].Path) })
	roots := make([]config.PartialRecord, 0, len(records))
	for _, record := range records {
		covered := false
		for _, root := range roots {
			if strings.HasPrefix(record.Path, root.Path+string(filepath.Separator)) {
				covered = true
				break
			}
		}
		if !covered {
			roots = append(roots, record)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
	return roots, nil
}

func (p *ownedPartial) remove() error {
	if p.pinned != nil {
		current, err := p.parent.Lstat(p.name)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !os.SameFile(p.pinned, current) {
			return errors.New("partial inode changed; refusing cleanup")
		}
	}
	if err := removeOwnedPartial(p.parent, p.name); err != nil {
		return err
	}
	p.pinned = nil // An explicit discard permits a new attempt at this name.
	return nil
}

// Retry only touches a partial registered before its first write, through the
// original directory handle. An arbitrary request cannot become a delete API.
func (s *Service) discardPartial(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	owned := s.partials[filepath.Clean(path)]
	if owned == nil {
		return errors.New("partial is not owned by this helper")
	}
	return owned.remove()
}
func (s *Service) cleanupPartials() error {
	s.mu.Lock()
	owned := s.partials
	s.partials = nil
	s.mu.Unlock()
	var failures []error
	for _, path := range owned {
		// Root confines cleanup even when a different process renames the parent.
		if err := path.remove(); err != nil {
			failures = append(failures, fmt.Errorf("cleanup owned partial %q: %w", path.name, err))
		}
		failures = append(failures, path.parent.Close())
	}
	return errors.Join(failures...)
}

// Restore only owner access on directories inside the owned staging tree. The
// archive may have restored 0500/0000 modes before a subsequent step failed.
// OpenRoot confines traversal; symlinks are removed as links, never followed.
func removeOwnedPartial(parent *os.Root, name string) error {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return parent.Remove(name)
	}
	if err := parent.Chmod(name, 0700); err != nil {
		return err
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	directory, err := child.Open(".")
	if err != nil {
		child.Close()
		return err
	}
	entries, err := directory.ReadDir(-1)
	directory.Close()
	if err == nil {
		for _, entry := range entries {
			if err = removeOwnedPartial(child, entry.Name()); err != nil {
				break
			}
		}
	}
	child.Close()
	if err != nil {
		return err
	}
	return parent.Remove(name)
}
