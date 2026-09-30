package webgui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/routespec"
)

type routeCandidate struct {
	route          connector.Route
	relayID        string
	cached         bool
	owners         []hopOwner
	temporaryProxy bool // One-off SOCKS test must not qualify a different saved route as a relay.
}

// A composed route's hop number is not a credential identity. Prefix hops
// belong to the relay configuration, never to the target's password slots.
type hopOwner struct {
	host config.Host
	hop  int
}

func routeOwners(host config.Host, count int) []hopOwner {
	owners := make([]hopOwner, count)
	for index := range owners {
		owners[index] = hopOwner{host: host, hop: index}
	}
	return owners
}

func (a *App) ensureEndpoint(ctx context.Context, paneID PaneID, name, peerName string) (*paneState, error) {
	ctx, cancel, generation, err := a.activeContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if !validPane(paneID) {
		return nil, fmt.Errorf("未知文件栏 %q", paneID)
	}
	if name == "" {
		name = "本机"
	}
	a.mu.RLock()
	configVersion := a.configVersion
	current := a.panes[paneID]
	if current != nil && !current.stale && current.endpoint != nil && current.name == name && endpointOpen(current.endpoint) {
		copy := *current
		copy.generation = generation
		a.mu.RUnlock()
		return &copy, nil
	}
	if peerName == "" {
		other := LeftPane
		if paneID == LeftPane {
			other = RightPane
		}
		if peer := a.panes[other]; peer != nil {
			peerName = peer.name
		}
	}
	a.mu.RUnlock()

	var next endpoint.Endpoint
	directory := ""
	if name == "本机" {
		next = endpoint.NewLocal()
	} else {
		remote, home, err := a.connectRemote(ctx, name, peerName)
		if err != nil {
			return nil, err
		}
		next, directory = remote, home
	}
	if directory == "" {
		var err error
		directory, err = next.Home(ctx)
		if err != nil {
			_ = next.Close()
			return nil, err
		}
	}
	state := &paneState{name: name, path: directory, endpoint: next, generation: generation}
	a.mu.Lock()
	if a.store == nil || a.locking || generation != a.generation || configVersion != a.configVersion {
		a.mu.Unlock()
		_ = next.Close()
		return nil, context.Canceled
	}
	previous := a.panes[paneID]
	if host, ok := a.document.HostByName(name); ok {
		state.shell = host.Shell
	}
	a.sequence++
	state.connection = a.sequence
	a.panes[paneID] = state
	if previous != nil && previous.endpoint != nil {
		a.retired = append(a.retired, previous.endpoint)
	}
	snapshot := *state
	a.mu.Unlock()
	if err := a.enqueueWorkspaceRecovery(&snapshot); err != nil {
		a.mu.Lock()
		snapshot.warning = "遗留工作区未能加入清理队列，目录仍保留：" + a.redactKnownLocked(err.Error())
		if a.panes[paneID] == state {
			state.warning = snapshot.warning
		}
		a.mu.Unlock()
	}
	return &snapshot, nil
}

func (a *App) connectRemote(ctx context.Context, name, peerName string) (*endpoint.Remote, string, error) {
	return a.connectRemoteVia(ctx, name, peerName, false, nil)
}

// Explicit tests use exactly the selected saved route (and optional selected
// SOCKS). Failure must not silently dial unrelated relay candidates.
func (a *App) connectRemoteVia(ctx context.Context, name, peerName string, exact bool, proxy *connector.SOCKS5) (*endpoint.Remote, string, error) {
	a.mu.RLock()
	document := a.document.Clone()
	generation, configVersion := a.generation, a.configVersion
	if exact {
		if settings, ok := ctx.Value(jobSettingsKey{}).(*jobSettings); ok {
			document, generation, configVersion = settings.document, settings.generation, settings.version
		}
	}
	host, ok := document.HostByName(name)
	a.mu.RUnlock()
	if !ok || host.Disabled {
		return nil, "", fmt.Errorf("未找到可用主机 %q", name)
	}
	var candidates []routeCandidate
	var err error
	if exact {
		var route connector.Route
		route, err = a.routeForHostContext(ctx, host)
		if proxy != nil {
			route.SOCKS = proxy
		}
		candidates = []routeCandidate{{route: route, owners: routeOwners(host, len(route.Hops)), temporaryProxy: proxy != nil}}
	} else {
		candidates, err = a.routeCandidates(host, peerName)
	}
	if err != nil {
		return nil, "", err
	}
	var failures []error
	for _, candidate := range candidates {
		route := candidate.route
		answers := make(map[int]challengeAnswer)
		for retry := 0; retry < max(3, len(route.Hops)*2+1); retry++ {
			a.mu.RLock()
			stale := a.locking || a.generation != generation || a.configVersion != configVersion
			a.mu.RUnlock()
			if stale {
				return nil, "", errors.New("连接期间配置或会话已改变，请重新连接")
			}
			if err := ctx.Err(); err != nil {
				return nil, "", err
			}
			remote, dialErr := endpoint.DialSSH(ctx, host.Name, host.HostFingerprint, route)
			if dialErr == nil {
				directory := host.LastDirectory
				if directory == "" {
					directory, dialErr = remote.Home(ctx)
				}
				if dialErr != nil {
					_ = remote.Close()
					return nil, "", dialErr
				}
				if err := a.acceptConnectionCredentials(host.ID, candidate, answers, generation, configVersion); err != nil {
					_ = remote.Close()
					return nil, "", err
				}
				if candidate.relayID != "" {
					a.rememberRelay(host.ID, peerName, candidate.relayID)
				}
				return remote, directory, nil
			}
			failures = append(failures, dialErr)
			if !isAuthenticationError(dialErr) || retry+1 == max(3, len(route.Hops)*2+1) {
				break
			}
			hopIndex := authenticationHop(dialErr)
			if hopIndex < 0 || hopIndex >= len(route.Hops) || hopIndex >= len(candidate.owners) {
				break
			}
			owner := candidate.owners[hopIndex]
			answer, accepted := a.ask(ctx, ChallengeModel{Kind: "password", Title: fmt.Sprintf("SSH 认证 · %s · 第 %d 跳", owner.host.Name, owner.hop+1), Message: "现有私钥、SSH Agent 和密码未通过。输入本次连接密码重试；仅在连接成功后缓存，勾选保存才写入该会话的配置。", Secret: true, AllowSave: true})
			if !accepted || answer.Value == "" {
				return nil, "", errors.New("已取消 SSH 认证")
			}
			route.Hops[hopIndex].Credentials.Password = answer.Value
			answers[hopIndex] = answer
		}
		if candidate.cached {
			a.forgetRelay(host.ID, peerName)
		}
	}
	return nil, "", errors.Join(failures...)
}

func (a *App) routeCandidates(host config.Host, peerName string) ([]routeCandidate, error) {
	base, err := a.routeForHost(host)
	if err != nil {
		return nil, err
	}
	result := []routeCandidate{{route: base, owners: routeOwners(host, len(base.Hops))}}
	a.mu.RLock()
	cachedRelay := a.cachedRelayLocked(host.ID, peerName)
	var relayIDs []string
	if cachedRelay != "" {
		relayIDs = append(relayIDs, cachedRelay)
	}
	for relayID := range a.sessionSSH {
		if relayID != host.ID && relayID != cachedRelay {
			relayIDs = append(relayIDs, relayID)
		}
	}
	document := a.document.Clone()
	a.mu.RUnlock()
	// Routine browsing never sprays the destination across the entire SOCKS
	// pool. Only the host's explicit ###默认socks belongs in base; pool probing
	// is reserved for a transfer after direct methods fail.
	for _, relayID := range relayIDs {
		relay := document.HostByID(relayID)
		if relay == nil || relay.Disabled || relay.NoRelay {
			continue
		}
		relayRoute, routeErr := a.routeForHost(*relay)
		if routeErr != nil {
			continue
		}
		candidate := connector.Route{Timeout: base.Timeout, SOCKS: relayRoute.SOCKS}
		candidate.Hops = append(candidate.Hops, relayRoute.Hops...)
		candidate.Hops = append(candidate.Hops, base.Hops...)
		owners := append(routeOwners(*relay, len(relayRoute.Hops)), routeOwners(host, len(base.Hops))...)
		result = append(result, routeCandidate{route: candidate, relayID: relayID, cached: relayID == cachedRelay, owners: owners})
	}
	return result, nil
}

func (a *App) routeForHost(host config.Host) (connector.Route, error) {
	return a.routeForHostContext(context.Background(), host)
}

// A controller's route describes how to reach an endpoint from this machine,
// not from another remote. A peer uses the final authenticated account/key;
// only an explicitly selected transfer pool adds preceding hops.
func (a *App) peerTransferRoute(ctx context.Context, host config.Host, socks *connector.SOCKS5, prefix []connector.Hop) (connector.Route, error) {
	route, err := a.routeForHostContext(ctx, host)
	if err != nil {
		return connector.Route{}, err
	}
	if len(route.Hops) == 0 {
		return connector.Route{}, errors.New("传输对端 SSH 路由为空")
	}
	final := route.Hops[len(route.Hops)-1]
	route.Hops = append(append([]connector.Hop(nil), prefix...), final)
	route.SOCKS = socks
	return route, nil
}

func (a *App) routeForHostContext(ctx context.Context, host config.Host) (connector.Route, error) {
	settings := a.settingsFor(ctx)
	document := settings.document
	generation, version := settings.generation, settings.version
	runtimePasswords := settings.passwords[host.ID]
	confirmFingerprint := func(hop int, address, fingerprint string) bool {
		title := fmt.Sprintf("端点：%s\n第 %d 跳：%s", host.Name, hop+1, address)
		if hop < 0 {
			title = fmt.Sprintf("主机：%s", address)
		}
		_, accepted := a.ask(ctx, ChallengeModel{Kind: "confirm-host-key", Title: "首次连接 SSH 跳点", Message: title + "\n指纹：" + fingerprint + "\n\n确认固定该指纹并继续？"})
		if !accepted {
			return false
		}
		if err := a.storeFingerprint(host, hop, fingerprint, generation, version); err != nil {
			// The connector callback can return only bool. Surface the original
			// persistence/conflict error here instead of disguising it as a
			// network or password failure in the later handshake error.
			a.mu.RLock()
			message := a.redactKnownLocked(err.Error())
			a.mu.RUnlock()
			a.ask(ctx, ChallengeModel{Kind: "confirm-host-key", Title: "SSH 指纹未保存，连接已中止", Message: message})
			return false
		}
		return true
	}
	if host.RouteSpec != "" {
		hops, err := routespec.ParseSSH(host.RouteSpec, func(name string) (*config.PrivateKey, bool) {
			for index := range document.Keys {
				if document.Keys[index].Name == name || document.Keys[index].ID == name {
					return &document.Keys[index], true
				}
			}
			return nil, false
		})
		if err != nil {
			return connector.Route{}, err
		}
		for index := range hops {
			if runtimePasswords[index] != "" {
				// A password supplied in this unlocked session supersedes a stale
				// configured password. connector.authMethods still tries SSH Agent
				// and vault private keys before this password.
				hops[index].Credentials.Password = runtimePasswords[index]
			}
			fingerprint := ""
			if index < len(host.HopFingerprints) {
				fingerprint = host.HopFingerprints[index]
			}
			if fingerprint != "" {
				hops[index].HostKey = connector.HostKeyPolicy{PinnedSHA256: fingerprint}
				continue
			}
			hopIndex := index
			var confirmMu sync.Mutex
			confirmed := ""
			hops[index].HostKey = connector.HostKeyPolicy{ConfirmNew: func(address, fingerprint string) bool {
				confirmMu.Lock()
				defer confirmMu.Unlock()
				if confirmed != "" {
					return confirmed == fingerprint
				}
				if !confirmFingerprint(hopIndex, address, fingerprint) {
					return false
				}
				confirmed = fingerprint
				return true
			}}
		}
		route := connector.Route{Hops: hops, Timeout: 15 * time.Second}
		if host.DefaultSOCKSID != "" {
			proxy := document.SOCKSByID(host.DefaultSOCKSID)
			if proxy == nil || proxy.Disabled {
				return connector.Route{}, errors.New("默认 SOCKS 配置不可用")
			}
			parsed, err := routespec.ParseSOCKS(proxy.Spec)
			if err != nil {
				return connector.Route{}, err
			}
			route.SOCKS = &parsed
		}
		return route, nil
	}
	credentials := connector.Credentials{UseAgent: true, Password: host.Password}
	if runtimePasswords[0] != "" {
		credentials.Password = runtimePasswords[0]
	}
	for _, keyID := range host.KeyIDs {
		key := document.KeyByID(keyID)
		if key == nil {
			return connector.Route{}, fmt.Errorf("主机 %s 引用的私钥 %s 不存在", host.Name, keyID)
		}
		credentials.PrivateKeys = append(credentials.PrivateKeys, connector.PrivateKey{PEM: []byte(key.PEM), Passphrase: []byte(key.Passphrase)})
	}
	policy := connector.HostKeyPolicy{PinnedSHA256: host.HostFingerprint}
	if host.HostFingerprint == "" {
		policy.ConfirmNew = func(address, fingerprint string) bool {
			return confirmFingerprint(-1, address, fingerprint)
		}
	}
	port := host.Port
	if port == 0 {
		port = 22
	}
	return connector.Route{Hops: []connector.Hop{{Host: host.Address, Port: port, User: host.User, Credentials: credentials, HostKey: policy}}, Timeout: 15 * time.Second}, nil
}

func (a *App) ask(ctx context.Context, challenge ChallengeModel) (challengeAnswer, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	challenge.ID = a.nextID("challenge")
	response := make(chan challengeAnswer, 1)
	a.mu.Lock()
	if a.locking || (a.ctx == nil && a.eventSink == nil) {
		a.mu.Unlock()
		return challengeAnswer{}, false
	}
	a.challenges[challenge.ID] = response
	a.mu.Unlock()
	a.emit("challenge", challenge)
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case answer := <-response:
		if challenge.Secret {
			if settings, ok := ctx.Value(jobSettingsKey{}).(*jobSettings); ok {
				settings.addSecret(answer.Value)
			}
		}
		return answer, answer.Accepted
	case <-ctx.Done():
		a.dropChallenge(challenge.ID)
		return challengeAnswer{}, false
	case <-timer.C:
		a.dropChallenge(challenge.ID)
		return challengeAnswer{}, false
	}
}

func (a *App) ResolveChallenge(id string, accepted bool, value string, save bool) {
	a.mu.Lock()
	response := a.challenges[id]
	delete(a.challenges, id)
	a.mu.Unlock()
	if response != nil {
		response <- challengeAnswer{Accepted: accepted, Value: value, Save: save}
	}
}

func (a *App) SkipChallenge(id string) {
	a.mu.Lock()
	response := a.challenges[id]
	delete(a.challenges, id)
	a.mu.Unlock()
	if response != nil {
		response <- challengeAnswer{Skipped: true}
	}
}

func (a *App) dropChallenge(id string) {
	a.mu.Lock()
	delete(a.challenges, id)
	a.mu.Unlock()
}

func endpointID(document config.Document, name string) string {
	if name == "" || name == "本机" {
		return "local"
	}
	if host, ok := document.HostByName(name); ok {
		return host.ID
	}
	return "local"
}

func orderedPair(left, right string) (string, string) {
	if left > right {
		return right, left
	}
	return left, right
}

func (a *App) cachedRelayLocked(targetID, peerName string) string {
	return cachedRelay(a.document, targetID, peerName)
}

func cachedRelay(document config.Document, targetID, peerName string) string {
	left, right := orderedPair(targetID, endpointID(document, peerName))
	for _, item := range document.Relays {
		if item.EndpointAID == left && item.EndpointBID == right {
			return item.RelayHostID
		}
	}
	return ""
}

func (a *App) rememberRelay(targetID, peerName, relayID string) {
	a.rememberRelayContext(context.Background(), targetID, peerName, relayID)
}

func (a *App) rememberRelayContext(ctx context.Context, targetID, peerName, relayID string) {
	a.mu.Lock()
	if !a.settingsCurrentLocked(ctx) {
		a.mu.Unlock()
		return
	}
	if host := a.document.HostByID(relayID); host != nil && host.NoRelay {
		a.mu.Unlock()
		return
	}
	left, right := orderedPair(targetID, endpointID(a.document, peerName))
	found := false
	for index := range a.document.Relays {
		if a.document.Relays[index].EndpointAID == left && a.document.Relays[index].EndpointBID == right {
			a.document.Relays[index].RelayHostID, a.document.Relays[index].LastSuccess = relayID, time.Now()
			found = true
			break
		}
	}
	if !found {
		a.document.Relays = append(a.document.Relays, config.RelaySuccess{EndpointAID: left, EndpointBID: right, RelayHostID: relayID, LastSuccess: time.Now()})
	}
	a.mu.Unlock()
	_ = a.save()
}

func (a *App) forgetRelay(targetID, peerName string) {
	a.forgetRelayContext(context.Background(), targetID, peerName)
}

func (a *App) forgetRelayContext(ctx context.Context, targetID, peerName string) {
	a.mu.Lock()
	if !a.settingsCurrentLocked(ctx) {
		a.mu.Unlock()
		return
	}
	left, right := orderedPair(targetID, endpointID(a.document, peerName))
	for index := range a.document.Relays {
		if a.document.Relays[index].EndpointAID == left && a.document.Relays[index].EndpointBID == right {
			a.document.Relays = append(a.document.Relays[:index], a.document.Relays[index+1:]...)
			break
		}
	}
	a.mu.Unlock()
	_ = a.save()
}

var hopNumberPattern = regexp.MustCompile(`(?:configure|handshake) hop ([0-9]+)`)

func authenticationHop(err error) int {
	match := hopNumberPattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return -1
	}
	value, _ := strconv.Atoi(match[1])
	return value - 1
}

func isAuthenticationError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unable to authenticate") || strings.Contains(text, "no ssh authentication methods") || strings.Contains(text, "no supported methods remain")
}

func endpointOpen(value endpoint.Endpoint) bool {
	if remote, ok := value.(interface{ IsClosed() bool }); ok {
		return !remote.IsClosed()
	}
	return true
}
