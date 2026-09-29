package webgui

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/configtext"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/routespec"
)

// Credentials can be saved by a successful connection while the editor is
// open. Include the editable document, not just the policy-change counter, so
// a stale editor cannot silently restore an old password. This opaque token
// contains no plaintext configuration and never leaves the unlocked UI.
func (a *App) configRevisionLocked() string {
	return fmt.Sprintf("%d:%d:%x", a.generation, a.configVersion, sha256.Sum256([]byte(configtext.Markdown(a.document))))
}

// Purely reads saved state. Opening/refreshing this view must not dial any
// endpoint, proxy, DNS name or public test service.
func (a *App) GetConnectionOverview() (ConnectionOverview, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil || a.locking {
		return ConnectionOverview{}, errors.New("保险库尚未解锁")
	}
	result := ConnectionOverview{Revision: a.configRevisionLocked(), Hosts: []ConnectionRow{}, SOCKS: []ConnectionRow{}, Relays: []RelayCacheModel{}}
	names := map[string]string{"local": "本机"}
	for _, host := range a.document.Hosts {
		names[host.ID] = host.Name
	}
	for _, host := range a.document.Hosts {
		row := ConnectionRow{ID: host.ID, Name: host.Name, Disabled: host.Disabled, RelayAllowed: !host.NoRelay, RelayReady: !host.Disabled && !host.NoRelay && a.sessionSSH[host.ID]}
		var latest time.Time
		for _, relay := range a.document.Relays {
			if relay.RelayHostID == host.ID && !host.Disabled && !host.NoRelay {
				row.RelayReady = true
				if relay.LastSuccess.After(latest) {
					latest = relay.LastSuccess
				}
			}
		}
		row.LastSuccess = timestamp(latest)
		result.Hosts = append(result.Hosts, row)
	}
	for _, proxy := range a.document.SOCKS {
		result.SOCKS = append(result.SOCKS, ConnectionRow{ID: proxy.ID, Name: proxy.Name, Disabled: proxy.Disabled, LastRTT: proxy.LastRTT, LastSuccess: timestamp(proxy.LastSuccess)})
	}
	for _, relay := range a.document.Relays {
		host := a.document.HostByID(relay.RelayHostID)
		if host == nil || host.Disabled || host.NoRelay || names[relay.EndpointAID] == "" || names[relay.EndpointBID] == "" {
			continue
		}
		result.Relays = append(result.Relays, RelayCacheModel{EndpointAID: relay.EndpointAID, EndpointBID: relay.EndpointBID, EndpointA: names[relay.EndpointAID], EndpointB: names[relay.EndpointBID], Relay: host.Name, LastSuccess: timestamp(relay.LastSuccess)})
	}
	return result, nil
}

func (a *App) SetConnectionPolicy(kind, id string, disabled, relayAllowed bool, revision string) (BootstrapModel, error) {
	a.mu.RLock()
	if a.store == nil || a.locking || revision == "" || revision != a.configRevisionLocked() {
		a.mu.RUnlock()
		return BootstrapModel{}, errors.New("配置版本已改变，请重新载入后操作")
	}
	next := a.document.Clone()
	a.mu.RUnlock()
	switch kind {
	case "ssh":
		host := next.HostByID(id)
		if host == nil {
			return BootstrapModel{}, errors.New("SSH 配置不存在")
		}
		host.Disabled, host.NoRelay = disabled, !relayAllowed
	case "socks":
		proxy := next.SOCKSByID(id)
		if proxy == nil {
			return BootstrapModel{}, errors.New("SOCKS 配置不存在")
		}
		proxy.Disabled = disabled
	default:
		return BootstrapModel{}, errors.New("未知连接类型")
	}
	// Reuse the same validation, vault-first transaction and selective cache
	// invalidation as text editing. The revision is checked again under saveMu.
	return a.SaveConfigTextsAtRevision(configtext.Markdown(next), revision)
}

func (a *App) ClearRelayCache(endpointA, endpointB, revision string) (ConnectionOverview, error) {
	a.saveMu.Lock()
	a.mu.Lock()
	if a.store == nil || a.locking || revision == "" || revision != a.configRevisionLocked() {
		a.mu.Unlock()
		a.saveMu.Unlock()
		return ConnectionOverview{}, errors.New("配置版本已改变，请重新载入后操作")
	}
	left, right := orderedPair(endpointA, endpointB)
	next := a.document.Clone()
	next.Relays = nil
	found := false
	for _, relay := range a.document.Relays {
		if relay.EndpointAID != left || relay.EndpointBID != right {
			next.Relays = append(next.Relays, relay)
		} else {
			found = true
		}
	}
	if !found {
		a.mu.Unlock()
		a.saveMu.Unlock()
		return ConnectionOverview{}, errors.New("这条缓存已不存在，请刷新记录")
	}
	if err := a.store.Save(a.password, next); err != nil {
		a.mu.Unlock()
		a.saveMu.Unlock()
		return ConnectionOverview{}, err
	}
	a.document = next
	a.configVersion++ // An already-running old job may not repopulate this cache.
	a.mu.Unlock()
	a.saveMu.Unlock()
	return a.GetConnectionOverview()
}

func (a *App) QueueConnectionTest(request ConnectionTestRequest) (string, error) {
	a.mu.RLock()
	if a.store == nil || a.locking || request.Revision == "" || request.Revision != a.configRevisionLocked() {
		a.mu.RUnlock()
		return "", errors.New("测试前配置已改变，请重新载入")
	}
	document, generation := a.document.Clone(), a.generation
	a.mu.RUnlock()
	var proxy *connector.SOCKS5
	targetID := request.ID
	label := "SSH 登录测试"
	if request.Kind == "socks" {
		saved := document.SOCKSByID(request.ID)
		if saved == nil || saved.Disabled {
			return "", errors.New("请选择已启用的 SOCKS")
		}
		parsed, err := routespec.ParseSOCKS(saved.Spec)
		if err != nil {
			return "", err
		}
		proxy, targetID, label = &parsed, request.TargetID, "SOCKS 认证路由测试 · "+saved.Name
	} else if request.Kind != "ssh" {
		return "", errors.New("未知连接测试类型")
	}
	host := document.HostByID(targetID)
	if host == nil || host.Disabled {
		return "", errors.New("请选择已启用的 SSH 测试目标")
	}
	name := host.Name
	return a.submitFor(generation, jobs.Job{Description: label + " → " + name, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		a.mu.RLock()
		current := request.Revision == a.configRevisionLocked() && a.settingsCurrentLocked(ctx)
		a.mu.RUnlock()
		if !current {
			return errors.New("等待测试期间配置已改变；没有连接任何主机，请重新测试")
		}
		emit(jobs.Update{Stage: "connection-test", Indeterminate: true, Message: "仅从控制机连接所选路由；不遍历池、不执行传输、不更新远端传输延迟排序"})
		started := time.Now()
		remote, _, err := a.connectRemoteVia(ctx, name, "", true, proxy)
		if err != nil {
			return fmt.Errorf("所选路由登录测试: %w", err)
		}
		defer remote.Close()
		emit(jobs.Update{Stage: "done", ProgressKnown: true, Progress: 1, Message: fmt.Sprintf("控制机路由登录成功 · 总用时 %s（含认证/人工确认，不是 TCP RTT）；不代表两远端互通或可用跳板", time.Since(started).Round(time.Millisecond))})
		return nil
	}})
}
