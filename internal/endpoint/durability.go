package endpoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/lovitus/dragfm-gui/internal/boundedbuf"
	"github.com/lovitus/dragfm-gui/internal/filecommit"
)

func (l *Local) SyncPaths(ctx context.Context, paths []string) error {
	return filecommit.SyncPaths(ctx, paths)
}

func (s *SudoLocal) SyncPaths(ctx context.Context, paths []string) error {
	for _, target := range paths {
		err := s.local.SyncPaths(ctx, []string{target})
		if errors.Is(err, fs.ErrPermission) {
			// Use only the already-approved, narrowly scoped child. Never run
			// a general privileged shell for a filesystem acknowledgement.
			err = s.childResult(ctx, "sync", target, nil)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Remote) SyncPaths(ctx context.Context, paths []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.fileSync != nil {
		return r.fileSync(ctx, paths)
	}
	if r.sftp != nil {
		if _, supported := r.sftp.HasExtension("fsync@openssh.com"); supported {
			stop := r.watchIO(ctx)
			defer stop()
			for _, target := range paths {
				if err := ctx.Err(); err != nil {
					return err
				}
				info, err := r.sftp.Lstat(target)
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() && !info.IsDir() {
					return fmt.Errorf("refusing to synchronize a symlink or special file: %q", target)
				}
				file, err := r.sftp.Open(target)
				if err != nil {
					return fmt.Errorf("open for final sync %q: %w", target, err)
				}
				syncErr := file.Sync()
				closeErr := file.Close()
				if err := errors.Join(syncErr, closeErr); err != nil {
					// An explicit durability failure must not become success by
					// trying a weaker method, or by rereading the same bytes.
					return fmt.Errorf("final SFTP sync %q: %w", target, err)
				}
			}
			return nil
		}
	}
	if r.fileTransport != nil {
		return fmt.Errorf("privileged file view has no final sync capability: %w", errors.ErrUnsupported)
	}
	return SyncPathsWithExec(ctx, r.Exec, paths)
}

// SyncPathsWithExec is the Linux system-command equivalent used by ordinary
// SSH and the already-approved SystemFiles executor. GNU sync with operands
// (without -d/-f) calls fsync for each file/directory and reports failure.
// Capability checking rejects old tools that silently ignore file operands.
// Never substitute argument-less sync or syncfs, whose writeback error
// reporting is incomplete on older Linux kernels.
func SyncPathsWithExec(ctx context.Context, exec func(context.Context, string, ExecOptions) error, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	run := func(command string) error {
		var diagnostics boundedbuf.Buffer
		err := exec(ctx, command, ExecOptions{Stdout: io.Discard, Stderr: &diagnostics})
		if err != nil {
			return fmt.Errorf("final filesystem sync: %w: %s", err, diagnostics.String())
		}
		return nil
	}
	if err := run("LC_ALL=C command sync --file-system --help"); err != nil {
		return fmt.Errorf("no verified per-file sync command: %w", err)
	}
	command := "command sync --"
	checks := ""
	for _, target := range paths {
		if !path.IsAbs(target) || strings.ContainsRune(target, 0) {
			return fmt.Errorf("sync requires an absolute path: %q", target)
		}
		quoted := " " + shellQuote(target)
		guard := "[ ! -L " + shellQuote(target) + " ] && { [ -f " + shellQuote(target) + " ] || [ -d " + shellQuote(target) + " ]; } && "
		if len(checks)+len(command)+len(quoted)+len(guard) > 16*1024 && command != "command sync --" {
			if err := run(checks + command); err != nil {
				return err
			}
			command = "command sync --"
			checks = ""
		}
		command += quoted
		checks += guard
	}
	return run(checks + command)
}

type durabilityError struct{ error }

func (e durabilityError) Unwrap() error   { return e.error }
func (e durabilityError) Retryable() bool { return false }
