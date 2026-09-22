package webgui

import (
	"context"
	"errors"
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
func (a *App) submitFor(generation uint64, job jobs.Job) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil || a.locking || generation != a.generation {
		return "", errors.New("保险库已锁定或会话已改变；未提交操作")
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
	secrets := make(map[string]bool)
	add := func(secret string) {
		if secret != "" {
			secrets[secret] = true
		}
	}
	add(string(a.password))
	for _, key := range a.document.Keys {
		add(key.PEM)
		add(key.Passphrase)
	}
	for _, host := range a.document.Hosts {
		add(host.Password)
		add(host.RootPassword)
		add(host.SudoPassword)
		for _, password := range host.HopPasswords {
			add(password)
		}
		if host.RouteSpec != "" {
			hops, err := routespec.ParseSSH(host.RouteSpec, func(name string) (*config.PrivateKey, bool) {
				for index := range a.document.Keys {
					key := &a.document.Keys[index]
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
	for _, passwords := range a.runtimePasswords {
		for _, password := range passwords {
			add(password)
		}
	}
	for _, proxy := range a.document.SOCKS {
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
	pairs := make([]string, 0, 2*len(ordered))
	for _, secret := range ordered {
		pairs = append(pairs, secret, "***")
	}
	value = redact(value)
	if len(pairs) > 0 {
		value = strings.NewReplacer(pairs...).Replace(value)
	}
	return value
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
	return prefix + string(b.data)
}
