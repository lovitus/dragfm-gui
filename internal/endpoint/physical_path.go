package endpoint

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// PhysicalPath resolves parent-directory aliases, not the final entry. File
// transfers preserve a final symlink itself rather than following its target.
func (l *Local) PhysicalPath(ctx context.Context, value string) (string, error) {
	directory := filepath.Dir(value)
	tail := []string{filepath.Base(value)}
	for {
		if err := ctx.Err(); err != nil { return "", err }
		resolved, err := filepath.EvalSymlinks(directory)
		if err == nil {
			for i := len(tail)-1; i >= 0; i-- { resolved = filepath.Join(resolved, tail[i]) }
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || directory == filepath.Dir(directory) { return "", err }
		tail = append(tail, filepath.Base(directory)); directory = filepath.Dir(directory)
	}
}

func (s *SudoLocal) PhysicalPath(ctx context.Context, value string) (string, error) { return s.local.PhysicalPath(ctx, value) }

func (r *Remote) PhysicalPath(ctx context.Context, value string) (string, error) {
	directory := path.Dir(value)
	tail := []string{path.Base(value)}
	for {
		if err := ctx.Err(); err != nil { return "", err }
		var resolved string
		var err error
		if r.sftp != nil {
			stop := r.watchIO(ctx)
			resolved, err = r.sftp.RealPath(directory)
			stop()
		} else {
			var output strings.Builder
			err = r.Exec(ctx, "cd -P -- "+shellQuote(directory)+" && pwd -P", ExecOptions{Stdout: &output})
			resolved = strings.TrimSuffix(output.String(), "\n")
		}
		if err == nil && resolved != "" {
			for i := len(tail)-1; i >= 0; i-- { resolved = path.Join(resolved, tail[i]) }
			return resolved, nil
		}
		// Resolve the nearest existing ancestor for a destination not created yet.
		if _, statErr := r.Stat(ctx, directory); !errors.Is(statErr, fs.ErrNotExist) || directory == path.Dir(directory) { return "", err }
		tail = append(tail, path.Base(directory)); directory = path.Dir(directory)
	}
}
