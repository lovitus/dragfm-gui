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
		_ = path.parent.RemoveAll(path.name)
		_ = path.parent.Close()
	}
}
