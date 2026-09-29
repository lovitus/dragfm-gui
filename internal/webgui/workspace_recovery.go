package webgui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

const workspaceExpiry = 24 * time.Hour

func (a *App) newTaskWorkspace(ctx context.Context, origin *endpoint.Remote, files endpoint.Endpoint, parent string, elevated bool) (*remoteagent.Workspace, func(bool) error, error) {
	journal, err := a.taskWorkspaceJournal(ctx, origin, files, elevated)
	if err != nil {
		return nil, nil, err
	}
	var registrars []func(config.WorkspaceRecord) error
	if journal != nil {
		registrars = append(registrars, func(record config.WorkspaceRecord) error { return journal(record, false) })
	}
	work, err := remoteagent.NewWorkspace(ctx, files, parent, registrars...)
	if err != nil {
		return nil, nil, err
	}
	return work, func(remove bool) error {
		if err := work.Close(remove); err != nil {
			return err
		}
		if remove && journal != nil {
			return journal(work.Record(), true)
		}
		return nil
	}, nil
}

// Both helper installs and system workspaces bind to the admitted host and
// the actual authenticated file connection, including a separate root login.
func (a *App) taskWorkspaceJournal(ctx context.Context, origin *endpoint.Remote, files endpoint.Endpoint, elevated bool) (remoteagent.WorkspaceJournal, error) {
	settings, queued := ctx.Value(jobSettingsKey{}).(*jobSettings)
	if !queued {
		// The standalone method entry is used by the existing low-level CLI
		// integration contract. An unlocked GUI must always journal its task.
		a.mu.RLock()
		unlocked := a.store != nil
		a.mu.RUnlock()
		if unlocked {
			return nil, transfer.PreserveSource(errors.New("GUI workspace creation requires a queued task"))
		}
		return nil, nil
	}
	host, ok := settings.document.HostByName(origin.Name())
	if !ok {
		return nil, transfer.PreserveSource(errors.New("workspace endpoint is absent from the admitted task"))
	}
	identity, err := origin.Identity(ctx)
	if err != nil {
		return nil, err
	}
	fileIdentity, err := files.Identity(ctx)
	if err != nil {
		return nil, err
	}
	if elevated {
		// This is the explicitly approved privileged view of THIS endpoint,
		// not permission to optimize a transfer between different accounts.
		// Keep the authenticated key, machine ID and filesystem root checks.
		fileIdentity.Principal = identity.Principal
	}
	if !endpoint.SameMachine(identity, fileIdentity) {
		return nil, transfer.PreserveSource(errors.New("workspace file channel identity cannot be tied to its admitted SSH endpoint"))
	}
	return func(record config.WorkspaceRecord, remove bool) error {
		record.HostID, record.Fingerprint, record.MachineID, record.Elevated = host.ID, identity.Fingerprint, identity.MachineID, elevated
		if err := a.saveWorkspaceRecord(settings.generation, record, remove); err != nil {
			return transfer.PreserveSource(fmt.Errorf("无法持久保存恢复记录，停止本任务，不改用未登记的传输路径: %w", err))
		}
		return nil
	}, nil
}

// Merge into the CURRENT document under the same save-before-publication lock
// as configuration edits. Old queued work may journal its original identity;
// it must never overwrite newer passwords/routes or a later vault generation.
func (a *App) saveWorkspaceRecord(generation uint64, record config.WorkspaceRecord, remove bool) error {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store == nil || a.locking || generation != a.generation {
		return errors.New("vault changed; workspace recovery journal was not updated")
	}
	next := a.document.Clone()
	found := false
	for i, item := range next.Workspaces {
		if item.HostID == record.HostID && item.Fingerprint == record.Fingerprint && item.Path == record.Path && item.MarkerSHA256 == record.MarkerSHA256 {
			found = true
			if remove {
				next.Workspaces = append(next.Workspaces[:i], next.Workspaces[i+1:]...)
			} else {
				next.Workspaces[i] = record
			}
			break
		}
	}
	if !found {
		if remove {
			return nil
		}
		next.Workspaces = append(next.Workspaces, record)
	}
	if err := a.store.Save(a.password, next); err != nil {
		return fmt.Errorf("save workspace recovery journal: %w", err)
	}
	a.document = next
	return nil
}

// Called only after an actual browsing connection has been published, NOT
// from a user-requested read-only connection test. Cleanup is visible in the
// normal queue/history and is serialized with transfers and commands.
func (a *App) enqueueWorkspaceRecovery(pane *paneState) error {
	remote, ok := pane.endpoint.(*endpoint.Remote)
	if !ok {
		return nil
	}
	a.recoveryMu.Lock()
	defer a.recoveryMu.Unlock()
	a.mu.RLock()
	if a.store == nil || a.locking || pane.generation != a.generation {
		a.mu.RUnlock()
		return context.Canceled
	}
	host, ok := a.document.HostByName(pane.name)
	if !ok {
		a.mu.RUnlock()
		return nil
	}
	var records []config.WorkspaceRecord
	for _, record := range a.document.Workspaces {
		if record.HostID == host.ID && !record.CreatedAt.IsZero() && time.Since(record.CreatedAt) >= workspaceExpiry {
			records = append(records, record)
		}
	}
	queue := a.queue
	a.mu.RUnlock()
	if len(records) == 0 {
		return nil
	}
	prefix := fmt.Sprintf("workspace-recovery-%d-%s-", pane.generation, host.ID)
	for _, job := range queue.Snapshot() {
		if strings.HasPrefix(job.ID, prefix) && (job.State == jobs.Pending || job.State == jobs.Running) {
			return nil
		}
	}
	_, err := a.submitFor(pane.generation, jobs.Job{ID: a.nextID(prefix), Description: "清理过期临时资源 · " + pane.name, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		return a.recoverWorkspaces(ctx, remote, records, emit)
	}})
	return err
}

func (a *App) recoverWorkspaces(ctx context.Context, browser *endpoint.Remote, records []config.WorkspaceRecord, emit func(jobs.Update)) (result error) {
	settings := a.settingsFor(ctx)
	a.mu.RLock()
	current := a.settingsCurrentLocked(ctx)
	a.mu.RUnlock()
	if !current {
		return errors.New("配置已改变；未清理遗留目录，请重新连接")
	}
	emit(jobs.Update{Stage: "cleanup", Indeterminate: true, Message: "核对已登记工作区；不恢复旧传输或 Pending"})
	// Timeouts/closing the cleanup connection cannot disrupt browsing or PTY.
	connect, cancel := context.WithTimeout(ctx, 20*time.Second)
	owned, err := browser.Fork(connect)
	cancel()
	if err != nil {
		return err
	}
	defer owned.Close()
	identity, err := owned.Identity(ctx)
	if err != nil {
		return err
	}
	var high endpoint.Endpoint
	var closeHigh func() error
	defer func() {
		if closeHigh != nil {
			result = errors.Join(result, closeHigh())
		}
	}()
	var rootDecision bool
	var rootDenied bool
	var output commandOutput
	log := func(message string) {
		_, _ = io.WriteString(&output, message+"\n")
		emit(jobs.Update{Stage: "cleanup", Indeterminate: true, Message: message, Output: output.String()})
	}
	removed, retained := 0, 0
	var failures []error
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		a.mu.RLock()
		current = a.settingsCurrentLocked(ctx)
		a.mu.RUnlock()
		if !current {
			return errors.Join(append(failures, errors.New("清理期间配置已改变；剩余目录已保留"))...)
		}
		// Historical recovery records bind the authenticated host, then
		// RecoverWorkspace verifies the exact marker and directory/parent
		// device+inode before deletion. They are not cp/mv path equivalence
		// evidence and do not contain a login principal or root-view token.
		if identity.Kind != endpoint.SSHKind || identity.Fingerprint == "" || identity.MachineID == "" || identity.Fingerprint != record.Fingerprint || identity.MachineID != record.MachineID {
			retained++
			failures = append(failures, fmt.Errorf("主机身份与登记不符，保留 %q", record.Path))
			continue
		}
		files := endpoint.Endpoint(owned)
		if record.Elevated {
			if !rootDecision {
				rootDecision = true
				var paths []string
				for _, item := range records {
					if item.Elevated && item.Fingerprint == record.Fingerprint && item.MachineID == record.MachineID {
						paths = append(paths, item.Path)
						for _, partial := range item.Partials {
							paths = append(paths, partial.Path)
						}
					}
				}
				answer, accepted := a.ask(ctx, ChallengeModel{Kind: "password", Title: "清理遗留工作区需要提权 · " + browser.Name(), Message: "仅检查并删除以下已登记、过期且无进程占用的临时目录：\n" + strings.Join(paths, "\n") + "\n不重新执行传输、不删除源文件。使用已配置的高权 SSH 身份或 sudo；留空使用已保存密码或 sudo -n。跳过会保留目录和恢复记录。", Secret: true, AllowSave: true, AllowSkip: true})
				if !accepted {
					if !answer.Skipped {
						return errors.Join(context.Canceled, errors.New("清理已取消；未处理目录和登记保留"))
					}
					rootDenied = true
				} else {
					password := answer.Value
					if password == "" {
						password = a.savedSudoPassword(ctx, browser.Name())
					}
					if answer.Save && answer.Value != "" {
						if err := offerSudoPassword(ctx, browser.Name(), answer.Value); err != nil {
							return err
						}
					}
					system, closeSystem, err := a.openSystemFiles(ctx, owned, password)
					if err != nil {
						return fmt.Errorf("获准的工作区清理通道: %w", err)
					}
					high = &systemTransferEndpoint{Remote: system.Files, commands: system}
					closeHigh = closeSystem
					highIdentity, err := high.Identity(ctx)
					if err != nil {
						return err
					}
					highIdentity.Principal = identity.Principal // approved account transition only
					if !endpoint.SameMachine(identity, highIdentity) {
						return errors.New("高权清理通道连接了不同主机；未删除任何高权目录")
					}
				}
			}
			if rootDenied {
				retained++
				log("已跳过提权清理，保留 " + record.Path)
				continue
			}
			files = high
		}
		// A permission dialog may outlive a configuration edit or vault lock.
		// Do not act on a pre-dialog authorization against a different config.
		a.mu.RLock()
		current = a.settingsCurrentLocked(ctx)
		a.mu.RUnlock()
		if !current {
			return errors.Join(append(failures, errors.New("清理授权期间配置已改变；剩余目录已保留"))...)
		}
		cleanup, cancel := context.WithTimeout(ctx, 20*time.Second)
		done, err := remoteagent.RecoverWorkspace(cleanup, files, record, workspaceExpiry)
		cancel()
		if err != nil {
			retained++
			failures = append(failures, fmt.Errorf("保留 %q: %w", record.Path, err))
			log("保留 " + record.Path + " · " + err.Error())
			continue
		}
		if !done {
			retained++
			log("仍被占用或尚未过期，保留 " + record.Path)
			continue
		}
		if err := a.saveWorkspaceRecord(settings.generation, record, true); err != nil {
			return errors.Join(append(failures, err)...)
		}
		removed++
		log("已回收或已不存在 " + record.Path)
	}
	log(fmt.Sprintf("检查完成：回收/已不存在 %d 项，保留 %d 项；未恢复旧任务", removed, retained))
	return errors.Join(failures...)
}
