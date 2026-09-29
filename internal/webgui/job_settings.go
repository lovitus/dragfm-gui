package webgui

import (
	"context"
	"errors"
	"sync"

	"github.com/lovitus/dragfm-gui/internal/config"
)

// Each admitted job owns an immutable copy of its routes, credentials and
// eligible pools. Concrete endpoints alone are insufficient: remote helpers
// also open connections, and must not resolve a reused display name against a
// newer vault document while this job waits in Pending.
type jobSettings struct {
	document    config.Document
	passwords   map[string]map[int]string
	sessions    map[string]bool
	generation  uint64
	version     uint64
	secretMu    sync.RWMutex
	master      string
	extraSecret []string
	pendingSudo map[string]string // Host ID -> offered password; not authenticated yet.
	redactValue func(string) string
}

type jobSettingsKey struct{}

func (a *App) snapshotSettingsLocked() *jobSettings {
	settings := &jobSettings{
		document: a.document.Clone(), passwords: make(map[string]map[int]string),
		sessions: make(map[string]bool), generation: a.generation, version: a.configVersion,
	}
	for id, values := range a.runtimePasswords {
		settings.passwords[id] = make(map[int]string, len(values))
		for hop, password := range values {
			settings.passwords[id][hop] = password
		}
	}
	for id, eligible := range a.sessionSSH {
		settings.sessions[id] = eligible
	}
	settings.master = string(a.password)
	settings.redactValue = knownSecretRedactor(settings.document, settings.passwords, settings.master)
	return settings
}

func (s *jobSettings) redact(value string) string {
	s.secretMu.RLock()
	redact := s.redactValue
	s.secretMu.RUnlock()
	return redact(value)
}

// Secrets entered after queue admission still belong to the task even if
// authentication fails or the user chooses not to save them. Rebuild one
// longest-first matcher; layering matchers can leak overlapping suffixes.
func (s *jobSettings) addSecret(value string) {
	if value == "" {
		return
	}
	s.secretMu.Lock()
	defer s.secretMu.Unlock()
	s.extraSecret = append(s.extraSecret, value)
	s.redactValue = knownSecretRedactor(s.document, s.passwords, s.master, s.extraSecret...)
}

func offerSudoPassword(ctx context.Context, name, password string) error {
	settings, ok := ctx.Value(jobSettingsKey{}).(*jobSettings)
	if !ok {
		return errors.New("保存 sudo 密码需要绑定传输任务")
	}
	host, ok := settings.document.HostByName(name)
	if !ok {
		return errors.New("sudo 密码对应的主机配置不存在")
	}
	settings.secretMu.Lock()
	defer settings.secretMu.Unlock()
	if settings.pendingSudo == nil {
		settings.pendingSudo = make(map[string]string)
	}
	settings.pendingSudo[host.ID] = password
	return nil
}

func (a *App) persistAuthenticatedSudo(ctx context.Context, name, password string) error {
	settings, ok := ctx.Value(jobSettingsKey{}).(*jobSettings)
	if !ok || password == "" {
		return nil
	}
	host, ok := settings.document.HostByName(name)
	if !ok {
		return nil
	}
	settings.secretMu.RLock()
	pending := settings.pendingSudo[host.ID]
	settings.secretMu.RUnlock()
	if pending != password {
		return nil
	}
	if err := a.saveSudoPassword(ctx, name, password); err != nil {
		return err
	}
	settings.secretMu.Lock()
	delete(settings.pendingSudo, host.ID)
	settings.secretMu.Unlock()
	return nil
}

func (a *App) settingsFor(ctx context.Context) *jobSettings {
	if settings, ok := ctx.Value(jobSettingsKey{}).(*jobSettings); ok {
		return settings
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.snapshotSettingsLocked()
}

// Call under a.mu. An old job may finish on its old endpoints, but must not
// repopulate edited/deleted hosts' password or successful-route caches.
func (a *App) settingsCurrentLocked(ctx context.Context) bool {
	settings, ok := ctx.Value(jobSettingsKey{}).(*jobSettings)
	return !a.locking && (!ok || (settings.generation == a.generation && settings.version == a.configVersion))
}

type sanitizedJobError struct {
	err     error
	message string
}

func (e sanitizedJobError) Error() string { return e.message }
func (e sanitizedJobError) Unwrap() error { return e.err }

var errStaleJobBinding = errors.New("提交前端点或连接配置已改变；未排队，请刷新后重试")
