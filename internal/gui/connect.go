package gui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
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

func (c *controller) connectEndpoint(name, peerName string, callback func(endpoint.Endpoint, string, error)) {
	if name == "本机" {
		local := endpoint.NewLocal()
		home, err := local.Home(context.Background())
		callback(local, home, err)
		return
	}
	host, ok := c.document.HostByName(name)
	if !ok {
		callback(nil, "", fmt.Errorf("未找到主机 %q", name))
		return
	}
	go func() {
		routes, err := c.routeCandidates(host, peerName)
		if err != nil {
			fyne.Do(func() { callback(nil, "", err) })
			return
		}
		var remote *endpoint.Remote
		for routeIndex, candidate := range routes {
			route := candidate.route
			for tries := 0; tries <= len(route.Hops); tries++ {
				remote, err = endpoint.DialSSH(context.Background(), host.Name, host.HostFingerprint, route)
				if err == nil || !isAuthenticationError(err) || routeIndex != 0 {
					break
				}
				hopIndex := authenticationHop(err)
				if hopIndex < 0 || hopIndex >= len(route.Hops) {
					break
				}
				password, save, accepted := c.requestConnectionPassword(host.Name, hopIndex+1)
				if !accepted {
					break
				}
				c.mu.Lock()
				if c.runtimePasswords[host.ID] == nil {
					c.runtimePasswords[host.ID] = make(map[int]string)
				}
				c.runtimePasswords[host.ID][hopIndex] = password
				if save {
					for index := range c.document.Hosts {
						if c.document.Hosts[index].ID == host.ID {
							for len(c.document.Hosts[index].HopPasswords) <= hopIndex {
								c.document.Hosts[index].HopPasswords = append(c.document.Hosts[index].HopPasswords, "")
							}
							c.document.Hosts[index].HopPasswords[hopIndex] = password
							host = c.document.Hosts[index]
							break
						}
					}
				}
				c.mu.Unlock()
				if save {
					_ = c.save()
				}
				route, err = c.routeForHost(host)
				if err != nil {
					break
				}
			}
			if err == nil {
				c.mu.Lock()
				c.sessionSSH[host.ID] = true
				c.mu.Unlock()
				if candidate.relayID != "" {
					c.rememberRelay(host.ID, peerName, candidate.relayID)
				} else if candidate.cached {
					c.forgetRelay(host.ID, peerName)
				}
				break
			}
			if candidate.cached {
				c.forgetRelay(host.ID, peerName)
			}
		}
		if err != nil {
			fyne.Do(func() { callback(nil, "", err) })
			return
		}
		directory := host.LastDirectory
		if directory == "" {
			directory, err = remote.Home(context.Background())
		}
		fyne.Do(func() { callback(remote, directory, err) })
	}()
}

func (c *controller) routeCandidates(host config.Host, peerName string) ([]routeCandidate, error) {
	base, err := c.routeForHost(host)
	if err != nil {
		return nil, err
	}
	routes := []routeCandidate{{route: base}}
	seenSOCKS := ""
	if base.SOCKS != nil {
		seenSOCKS = base.SOCKS.Address
	}
	for _, item := range c.document.SOCKS {
		if item.Disabled {
			continue
		}
		proxy, err := routespec.ParseSOCKS(item.Spec)
		if err != nil || proxy.Address == seenSOCKS {
			continue
		}
		candidate := base
		candidate.SOCKS = &proxy
		routes = append(routes, routeCandidate{route: candidate})
	}
	cachedRelay := c.cachedRelay(host.ID, peerName)
	var relayIDs []string
	if cachedRelay != "" {
		relayIDs = append(relayIDs, cachedRelay)
	}
	c.mu.Lock()
	for relayID := range c.sessionSSH {
		if relayID != host.ID && relayID != cachedRelay {
			relayIDs = append(relayIDs, relayID)
		}
	}
	c.mu.Unlock()
	for _, relayID := range relayIDs {
		relay := c.document.HostByID(relayID)
		if relay == nil || relay.Disabled {
			continue
		}
		relayRoute, routeErr := c.routeForHost(*relay)
		if routeErr != nil {
			continue
		}
		candidate := connector.Route{Timeout: base.Timeout, SOCKS: relayRoute.SOCKS}
		candidate.Hops = append(candidate.Hops, relayRoute.Hops...)
		candidate.Hops = append(candidate.Hops, base.Hops...)
		routes = append(routes, routeCandidate{route: candidate, relayID: relayID, cached: relayID == cachedRelay})
	}
	return routes, nil
}

func (c *controller) endpointID(name string) string {
	if name == "" || name == "本机" {
		return "local"
	}
	if host, ok := c.document.HostByName(name); ok {
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

func (c *controller) cachedRelay(targetID, peerName string) string {
	left, right := orderedPair(targetID, c.endpointID(peerName))
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range c.document.Relays {
		if item.EndpointAID == left && item.EndpointBID == right {
			return item.RelayHostID
		}
	}
	return ""
}

func (c *controller) rememberRelay(targetID, peerName, relayID string) {
	left, right := orderedPair(targetID, c.endpointID(peerName))
	c.mu.Lock()
	found := false
	for index := range c.document.Relays {
		if c.document.Relays[index].EndpointAID == left && c.document.Relays[index].EndpointBID == right {
			c.document.Relays[index].RelayHostID = relayID
			c.document.Relays[index].LastSuccess = time.Now()
			found = true
			break
		}
	}
	if !found {
		c.document.Relays = append(c.document.Relays, config.RelaySuccess{EndpointAID: left, EndpointBID: right, RelayHostID: relayID, LastSuccess: time.Now()})
	}
	c.mu.Unlock()
	go func() { _ = c.save() }()
}

func (c *controller) forgetRelay(targetID, peerName string) {
	left, right := orderedPair(targetID, c.endpointID(peerName))
	c.mu.Lock()
	for index := range c.document.Relays {
		if c.document.Relays[index].EndpointAID == left && c.document.Relays[index].EndpointBID == right {
			c.document.Relays = append(c.document.Relays[:index], c.document.Relays[index+1:]...)
			break
		}
	}
	c.mu.Unlock()
	go func() { _ = c.save() }()
}

func (c *controller) routeForHost(host config.Host) (connector.Route, error) {
	if host.RouteSpec != "" {
		hops, err := routespec.ParseSSH(host.RouteSpec, func(name string) (*config.PrivateKey, bool) {
			for index := range c.document.Keys {
				key := &c.document.Keys[index]
				if key.Name == name || key.ID == name {
					return key, true
				}
			}
			return nil, false
		})
		if err != nil {
			return connector.Route{}, err
		}
		for index := range hops {
			if password := c.cachedHopPassword(host, index); hops[index].Credentials.Password == "" && password != "" {
				hops[index].Credentials.Password = password
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
				accepted := c.confirmBlocking("首次连接 SSH 跳点", fmt.Sprintf("端点：%s\n第 %d 跳：%s\n指纹：%s\n\n确认保存并继续连接？", host.Name, hopIndex+1, address, fingerprint))
				if accepted {
					c.mu.Lock()
					for itemIndex := range c.document.Hosts {
						if c.document.Hosts[itemIndex].ID == host.ID {
							for len(c.document.Hosts[itemIndex].HopFingerprints) <= hopIndex {
								c.document.Hosts[itemIndex].HopFingerprints = append(c.document.Hosts[itemIndex].HopFingerprints, "")
							}
							c.document.Hosts[itemIndex].HopFingerprints[hopIndex] = fingerprint
							break
						}
					}
					c.mu.Unlock()
					go func() { _ = c.save() }()
				}
				return accepted
			}}
		}
		route := connector.Route{Hops: hops, Timeout: 15 * time.Second}
		if host.DefaultSOCKSID != "" {
			proxy := c.document.SOCKSByID(host.DefaultSOCKSID)
			if proxy == nil || proxy.Disabled {
				return connector.Route{}, fmt.Errorf("默认 SOCKS 配置不可用")
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
		key := c.document.KeyByID(keyID)
		if key == nil {
			return connector.Route{}, fmt.Errorf("主机 %s 引用的私钥 %s 不存在", host.Name, keyID)
		}
		credentials.PrivateKeys = append(credentials.PrivateKeys, connector.PrivateKey{PEM: []byte(key.PEM), Passphrase: []byte(key.Passphrase)})
	}
	policy := connector.HostKeyPolicy{PinnedSHA256: host.HostFingerprint}
	if host.HostFingerprint == "" {
		policy.AcceptNew = true
		policy.ConfirmNew = func(address, fingerprint string) bool {
			accepted := c.confirmBlocking("首次连接主机", fmt.Sprintf("主机：%s\n指纹：%s\n\n确认保存并继续连接？", address, fingerprint))
			if accepted {
				c.mu.Lock()
				for index := range c.document.Hosts {
					if c.document.Hosts[index].ID == host.ID {
						c.document.Hosts[index].HostFingerprint = fingerprint
						break
					}
				}
				c.mu.Unlock()
				go func() { _ = c.save() }()
			}
			return accepted
		}
	}
	hop := connector.Hop{Host: host.Address, Port: host.Port, User: host.User, Credentials: credentials, HostKey: policy}
	if hop.Port == 0 {
		hop.Port = 22
	}
	return connector.Route{Hops: []connector.Hop{hop}, Timeout: 15 * time.Second}, nil
}

func (c *controller) cachedHopPassword(host config.Host, index int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if passwords := c.runtimePasswords[host.ID]; passwords != nil && passwords[index] != "" {
		return passwords[index]
	}
	if index < len(host.HopPasswords) {
		return host.HopPasswords[index]
	}
	return ""
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

func (c *controller) requestConnectionPassword(host string, hop int) (string, bool, bool) {
	type answer struct {
		password string
		save     bool
		accepted bool
	}
	result := make(chan answer, 1)
	fyne.Do(func() {
		password := widget.NewPasswordEntry()
		save := widget.NewCheck("保存到加密保险库", nil)
		items := []*widget.FormItem{widget.NewFormItem("密码", password), widget.NewFormItem("", save)}
		dialog.ShowForm("SSH 认证 · "+host+" · 第 "+strconv.Itoa(hop)+" 跳", "重试", "取消", items, func(ok bool) {
			result <- answer{password: password.Text, save: save.Checked, accepted: ok && password.Text != ""}
		}, c.window)
	})
	response := <-result
	return response.password, response.save, response.accepted
}

func (c *controller) confirmBlocking(title, message string) bool {
	result := make(chan bool, 1)
	fyne.Do(func() {
		dialog.ShowConfirm(title, message, func(value bool) { result <- value }, c.window)
	})
	select {
	case accepted := <-result:
		return accepted
	case <-time.After(5 * time.Minute):
		return false
	}
}

func hostValidation(host config.Host) error {
	if host.Name == "" || (host.RouteSpec == "" && (host.Address == "" || host.User == "")) {
		return errors.New("主机名称和 SSH 路由不能为空")
	}
	return nil
}
