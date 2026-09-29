package webgui

import (
	"context"
	"errors"
	"fmt"

	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// Only transfer commands use the privileged executor. The embedded Remote
// supplies the SAME account's SFTP view, including physical paths and owners;
// neither this adapter nor its task-owned transport replaces a browser endpoint.
type systemTransferEndpoint struct {
	*endpoint.Remote
	commands *remoteagent.SystemFiles
}

func (s *systemTransferEndpoint) Exec(ctx context.Context, command string, options endpoint.ExecOptions) error {
	return s.commands.Exec(ctx, command, options)
}

func (a *App) openNcatEndpoint(ctx context.Context, remote *endpoint.Remote, side strategy.Direction, elevated bool) (endpoint.Endpoint, func() error, error) {
	if !elevated {
		owned, err := remote.Fork(ctx)
		if err != nil {
			return nil, nil, err
		}
		return owned, func() error { _ = owned.Close(); return nil }, nil
	}
	access := accessFor(ctx)
	if access == nil {
		return nil, nil, errors.New("系统高权传输缺少任务权限上下文")
	}
	if err := strategy.Authorize(ctx, strategy.Attempt{Direction: side, Elevated: true}, sideRisk(side)); err != nil {
		return nil, nil, err
	}
	files, cleanup, err := a.openSystemFiles(ctx, remote, access.passwords[side])
	if err != nil {
		return nil, nil, err
	}
	return &systemTransferEndpoint{Remote: files.Files, commands: files}, cleanup, nil
}

// Called only after the source/target's existing privilege approval. Reuse a
// configured root identity if present, otherwise use this login's scoped sudo.
// The system server is architecture-independent; no installed agent is needed.
func (a *App) openSystemFiles(ctx context.Context, remote *endpoint.Remote, password string) (*remoteagent.SystemFiles, func() error, error) {
	var rootErr error
	host, configured := a.settingsFor(ctx).document.HostByName(remote.Name())
	if configured && (host.RootUser != "" || len(host.RootKeyIDs) > 0) {
		route, err := a.routeForHostContext(ctx, host)
		if err == nil {
			route, err = a.rootRouteForHost(ctx, route, host)
		}
		if err == nil {
			last := route.Hops[len(route.Hops)-1]
			owned, dialErr := endpoint.DialSSH(ctx, remote.Name(), last.HostKey.PinnedSHA256, route)
			if dialErr == nil {
				journal, journalErr := a.taskWorkspaceJournal(ctx, remote, owned, true)
				if journalErr != nil {
					_ = owned.Close()
					return nil, nil, journalErr
				}
				files, fileErr := remoteagent.OpenSystemFiles(ctx, owned, false, "", journal)
				if fileErr == nil {
					activity.Report(ctx, "system-files/root-ssh", 0)
					return files, func() error {
						defer owned.Close()
						return files.Close()
					}, nil
				}
				_ = owned.Close()
				rootErr = fmt.Errorf("root SSH system files: %w", fileErr)
				if !transfer.Retryable(rootErr) {
					return nil, nil, rootErr
				}
			} else {
				rootErr = fmt.Errorf("root SSH: %w", dialErr)
			}
		} else {
			rootErr = err
		}
	}
	owned, err := remote.Fork(ctx)
	if err != nil {
		return nil, nil, errors.Join(rootErr, err)
	}
	journal, err := a.taskWorkspaceJournal(ctx, remote, owned, true)
	if err != nil {
		_ = owned.Close()
		return nil, nil, errors.Join(rootErr, err)
	}
	files, err := remoteagent.OpenSystemFiles(ctx, owned, true, password, journal)
	if err != nil {
		_ = owned.Close()
		return nil, nil, errors.Join(rootErr, err)
	}
	if files.SudoAuthenticated() {
		if err := a.persistAuthenticatedSudo(ctx, remote.Name(), password); err != nil {
			cleanupErr := files.Close()
			_ = owned.Close()
			return nil, nil, transfer.PreserveSource(errors.Join(fmt.Errorf("system sudo verified but credential save failed: %w", err), cleanupErr))
		}
	}
	activity.Report(ctx, "system-files/sudo", 0)
	return files, func() error {
		// Files.Close confirms the OS server exited and reports partial
		// cleanup failures. Transport teardown may legitimately return EOF
		// when closing its already-closed ordinary SFTP channel afterwards.
		defer owned.Close()
		return files.Close()
	}, nil
}
