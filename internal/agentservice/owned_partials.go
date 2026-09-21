package agentservice

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var partialName = regexp.MustCompile(`(?:^|\.)dragfm-partial-[0-9a-f]{16,64}$`)

type ownedPartial struct {
	parent *os.Root
	name   string
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
	owned := s.partials[filepath.Clean(path)]
	delete(s.partials, filepath.Clean(path))
	s.mu.Unlock()
	if owned != nil {
		_ = owned.parent.Close()
	}
}
func (s *Service) cleanupPartials() {
	s.mu.Lock()
	owned := s.partials
	s.partials = nil
	s.mu.Unlock()
	for _, path := range owned {
		// Root confines cleanup even when a different process renames the parent.
		_ = removeOwnedPartial(path.parent, path.name)
		_ = path.parent.Close()
	}
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
