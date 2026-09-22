package webgui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/routespec"
)

type routeCandidate struct {
	route   connector.Route
	relayID string
	cached  bool
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
	current := a.panes[paneID]
	if current != nil && current.endpoint != nil && current.name == name && endpointOpen(current.endpoint) {
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
	if a.store == nil || a.locking || generation != a.generation {
		a.mu.Unlock()
		_ = next.Close()
		return nil, context.Canceled
	}
	previous := a.panes[paneID]
	a.panes[paneID] = state
	if previous != nil && previous.endpoint != nil {
		a.retired = append(a.retired, previous.endpoint)
	}
	a.mu.Unlock()
	return state, nil
}

func (a *App) connectRemote(ctx context.Context, name, peerName string) (*endpoint.Remote, string, error) {
	a.mu.RLock()
	document := a.document.Clone()
	host, ok := document.HostByName(name)
	a.mu.RUnlock()
	if !ok || host.Disabled {
		return nil, "", fmt.Errorf("未找到可用主机 %q", name)
	}
	candidates, err := a.routeCandidates(host, peerName)
	if err != nil {
		return nil, "", err
	}
	var failures []error
	for _, candidate := range candidates {
		route := candidate.route
		for retry := 0; retry <= len(route.Hops); retry++ {
			remote, dialErr := endpoint.DialSSH(ctx, host.Name, host.HostFingerprint, route)
			if dialErr == nil {
				a.mu.Lock()
				a.sessionSSH[host.ID] = true
				a.mu.Unlock()
				if candidate.relayID != "" {
					a.rememberRelay(host.ID, peerName, candidate.relayID)
				}
				directory := host.LastDirectory
				if directory == "" {
					directory, dialErr = remote.Home(ctx)
				}
				if dialErr != nil {
					_ = remote.Close()
					return nil, "", dialErr
				}
				return remote, directory, nil
			}
			failures = append(failures, dialErr)
			if !isAuthenticationError(dialErr) {
				break
			}
			hopIndex := authenticationHop(dialErr)
			if hopIndex < 0 || hopIndex >= len(route.Hops) {
				break
			}
			answer, accepted := a.ask(ctx, ChallengeModel{Kind: "password", Title: fmt.Sprintf("SSH 认证 · %s · 第 %d 跳", name, hopIndex+1), Message: "现有私钥、SSH Agent 和已保存密码均未通过。输入本次连接密码后重试。", Secret: true, AllowSave: true})
			if !accepted || answer.Value == "" {
				break
			}
			route.Hops[hopIndex].Credentials.Password = answer.Value
			a.mu.Lock()
			if a.runtimePasswords[host.ID] == nil {
				a.runtimePasswords[host.ID] = make(map[int]string)
			}
			a.runtimePasswords[host.ID][hopIndex] = answer.Value
			if answer.Save {
				for index := range a.document.Hosts {
					if a.document.Hosts[index].ID == host.ID {
						for len(a.document.Hosts[index].HopPasswords) <= hopIndex {
							a.document.Hosts[index].HopPasswords = append(a.document.Hosts[index].HopPasswords, "")
						}
						a.document.Hosts[index].HopPasswords[hopIndex] = answer.Value
						break
					}
				}
			}
			a.mu.Unlock()
			if answer.Save {
				_ = a.save()
			}
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
	result := []routeCandidate{{route: base}}
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
		if relay == nil || relay.Disabled {
			continue
		}
		relayRoute, routeErr := a.routeForHost(*relay)
		if routeErr != nil {
			continue
		}
		candidate := connector.Route{Timeout: base.Timeout, SOCKS: relayRoute.SOCKS}
		candidate.Hops = append(candidate.Hops, relayRoute.Hops...)
		candidate.Hops = append(candidate.Hops, base.Hops...)
		result = append(result, routeCandidate{route: candidate, relayID: relayID, cached: relayID == cachedRelay})
	}
	return result, nil
}

func (a *App) routeForHost(host config.Host) (connector.Route, error) {
	a.mu.RLock()
	document := a.document.Clone()
	runtimePasswords := make(map[int]string)
	for index, value := range a.runtimePasswords[host.ID] {
		runtimePasswords[index] = value
	}
	a.mu.RUnlock()
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
			} else if hops[index].Credentials.Password == "" && index < len(host.HopPasswords) {
				hops[index].Credentials.Password = host.HopPasswords[index]
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
			hops[index].HostKey = connector.HostKeyPolicy{ConfirmNew: func(address, fingerprint string) bool {
				_, accepted := a.ask(context.Background(), ChallengeModel{Kind: "confirm-host-key", Title: "首次连接 SSH 跳点", Message: fmt.Sprintf("端点：%s\n第 %d 跳：%s\n指纹：%s\n\n确认固定该指纹并继续？", host.Name, hopIndex+1, address, fingerprint)})
				if accepted {
					a.storeHopFingerprint(host.ID, hopIndex, fingerprint)
				}
				return accepted
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
			_, accepted := a.ask(context.Background(), ChallengeModel{Kind: "confirm-host-key", Title: "首次连接主机", Message: fmt.Sprintf("主机：%s\n指纹：%s\n\n确认固定该指纹并继续？", address, fingerprint)})
			if accepted {
				a.storeHostFingerprint(host.ID, fingerprint)
			}
			return accepted
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

func (a *App) dropChallenge(id string) {
	a.mu.Lock()
	delete(a.challenges, id)
	a.mu.Unlock()
}

func (a *App) storeHopFingerprint(hostID string, hop int, fingerprint string) {
	a.mu.Lock()
	for index := range a.document.Hosts {
		if a.document.Hosts[index].ID == hostID {
			for len(a.document.Hosts[index].HopFingerprints) <= hop {
				a.document.Hosts[index].HopFingerprints = append(a.document.Hosts[index].HopFingerprints, "")
			}
			a.document.Hosts[index].HopFingerprints[hop] = fingerprint
			break
		}
	}
	a.mu.Unlock()
	_ = a.save()
}

func (a *App) storeHostFingerprint(hostID, fingerprint string) {
	a.mu.Lock()
	for index := range a.document.Hosts {
		if a.document.Hosts[index].ID == hostID {
			a.document.Hosts[index].HostFingerprint = fingerprint
			break
		}
	}
	a.mu.Unlock()
	_ = a.save()
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
	left, right := orderedPair(targetID, endpointID(a.document, peerName))
	for _, item := range a.document.Relays {
		if item.EndpointAID == left && item.EndpointBID == right {
			return item.RelayHostID
		}
	}
	return ""
}

func (a *App) rememberRelay(targetID, peerName, relayID string) {
	a.mu.Lock()
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
	a.mu.Lock()
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
