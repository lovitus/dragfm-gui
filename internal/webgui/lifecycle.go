package webgui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/routespec"
)

func (a *App) isUnlocked() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.store != nil && !a.locking
}

func (a *App) activeContext(parent context.Context) (context.Context, context.CancelFunc, uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil || a.locking || a.sessionCtx == nil {
		return nil, nil, 0, errors.New("保险库尚未解锁")
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(a.sessionCtx, cancel)
	return ctx, func() { stop(); cancel() }, a.generation, nil
}

// Admission and the vault generation check are atomic with respect to Lock.
func (a *App) submitFor(generation uint64, job jobs.Job, bound ...*paneState) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil || a.locking || generation != a.generation {
		return "", errors.New("保险库已锁定或会话已改变；未提交操作")
	}
	for _, expected := range bound {
		current := a.panes[expected.id]
		if current == nil || current.stale || current.endpoint != expected.endpoint || expected.generation != a.generation {
			return "", errStaleJobBinding
		}
	}
	if job.Run == nil {
		return "", errors.New("job has no runner")
	}
	settings := a.snapshotSettingsLocked()
	run := job.Run
	job.Description = settings.redact(job.Description)
	job.Run = func(ctx context.Context, emit func(jobs.Update)) (err error) {
		defer func() {
			if value := recover(); value != nil {
				err = fmt.Errorf("job panicked: %v", value)
			}
			if err != nil {
				err = sanitizedJobError{err: err, message: settings.redact(err.Error())}
			}
		}()
		ctx = context.WithValue(ctx, jobSettingsKey{}, settings)
		err = run(ctx, func(update jobs.Update) {
			update.Description = settings.redact(update.Description)
			update.Message = settings.redact(update.Message)
			update.Output = settings.redact(update.Output)
			update.Error = settings.redact(update.Error)
			update.Stage = settings.redact(update.Stage)
			update.Method = settings.redact(update.Method)
			emit(update)
		})
		return err
	}
	return a.queue.Submit(job)
}

// JobSnapshot repairs lost frontend events. Subscribe before reading it and
// compare per-job revisions when merging it with live updates.
func (a *App) JobSnapshot() ([]JobUpdateModel, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil || a.locking {
		return nil, errors.New("保险库尚未解锁")
	}
	result := make([]JobUpdateModel, 0)
	for _, update := range a.queue.Snapshot() {
		result = append(result, a.jobModelLocked(update))
	}
	return result, nil
}

// redactKnownLocked additionally removes bare configured secrets (for example
// a command echoing a password without a password= prefix). Never persist an
// unredacted command, routing error, or job summary.
func (a *App) redactKnownLocked(value string) string {
	return knownSecretRedactor(a.document, a.runtimePasswords, string(a.password))(value)
}

func knownSecretRedactor(document config.Document, passwords map[string]map[int]string, master string, extra ...string) func(string) string {
	ordered := knownSecrets(document, passwords, master, extra...)
	pairs := make([]string, 0, 2*len(ordered))
	for _, secret := range ordered {
		pairs = append(pairs, secret, "***")
	}
	if len(pairs) == 0 {
		return redact
	}
	replacer := strings.NewReplacer(pairs...)
	return func(value string) string { return redact(replacer.Replace(value)) }
}

func knownSecrets(document config.Document, passwords map[string]map[int]string, master string, extra ...string) []string {
	secrets := make(map[string]bool)
	add := func(secret string) {
		if secret != "" {
			secrets[secret] = true
		}
	}
	add(master)
	for _, secret := range extra {
		add(secret)
	}
	for _, key := range document.Keys {
		add(key.PEM)
		add(key.Passphrase)
	}
	for _, host := range document.Hosts {
		add(host.Password)
		add(host.RootPassword)
		add(host.SudoPassword)
		for _, password := range host.HopPasswords {
			add(password)
		}
		for _, password := range host.UnverifiedPasswords {
			add(password)
		}
		if host.RouteSpec != "" {
			hops, err := routespec.ParseSSH(host.RouteSpec, func(name string) (*config.PrivateKey, bool) {
				for index := range document.Keys {
					key := &document.Keys[index]
					if key.Name == name || key.ID == name {
						return key, true
					}
				}
				return nil, false
			})
			if err == nil {
				for _, hop := range hops {
					add(hop.Credentials.Password)
				}
			}
		}
	}
	for _, hopPasswords := range passwords {
		for _, password := range hopPasswords {
			add(password)
		}
	}
	for _, proxy := range document.SOCKS {
		add(proxy.Password)
		if proxy.Spec != "" {
			if parsed, err := routespec.ParseSOCKS(proxy.Spec); err == nil {
				add(parsed.Password)
			}
		}
	}
	ordered := make([]string, 0, len(secrets))
	for secret := range secrets {
		ordered = append(ordered, secret)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	return ordered
}

const maxCommandOutput = 64 * 1024

// Stdout and stderr can write concurrently. Retain a bounded tail instead of
// growing an unbounded bytes.Buffer on the controller.
type commandOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *commandOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) >= maxCommandOutput {
		b.data = append(b.data[:0], p[len(p)-maxCommandOutput:]...)
		b.truncated = true
	} else {
		if extra := len(b.data) + len(p) - maxCommandOutput; extra > 0 {
			copy(b.data, b.data[extra:])
			b.data = b.data[:len(b.data)-extra]
			b.truncated = true
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *commandOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	prefix := ""
	if b.truncated {
		prefix = "[输出已截断；仅保留最后 64 KiB]\n"
	}
	// Tail truncation can split a UTF-8 character; do not return invalid JSON.
	return prefix + strings.ToValidUTF8(string(b.data), "�")
}
