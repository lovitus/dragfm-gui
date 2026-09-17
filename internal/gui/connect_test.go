package gui

import (
	"testing"

	"github.com/lovitus/dragfm-gui/internal/config"
)

func TestRouteCandidatesIncludeSOCKSAndSuccessfulSessionRelay(t *testing.T) {
	t.Parallel()
	target := config.Host{ID: "target", Name: "target", RouteSpec: "user@target:22", HopFingerprints: []string{"SHA256:target"}}
	jump := config.Host{ID: "jump", Name: "jump", RouteSpec: "user@jump:22", HopFingerprints: []string{"SHA256:jump"}}
	controller := &controller{document: config.Document{
		Hosts: []config.Host{target, jump},
		SOCKS: []config.SOCKSProxy{{ID: "proxy", Name: "proxy", Spec: "user:pass@127.0.0.1:1080"}},
	}, runtimePasswords: make(map[string]map[int]string), sessionSSH: map[string]bool{"jump": true}}
	routes, err := controller.routeCandidates(target, "本机")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 || routes[1].route.SOCKS == nil || len(routes[2].route.Hops) != 2 || routes[2].relayID != "jump" {
		t.Fatalf("unexpected candidates: %#v", routes)
	}
}

func TestRouteCandidatesPreferCachedRelayWithoutGlobalScanning(t *testing.T) {
	target := config.Host{ID: "target", Name: "target", RouteSpec: "user@target:22", HopFingerprints: []string{"SHA256:target"}}
	cached := config.Host{ID: "cached", Name: "cached", RouteSpec: "user@cached:22", HopFingerprints: []string{"SHA256:cached"}}
	unused := config.Host{ID: "unused", Name: "unused", RouteSpec: "user@unused:22", HopFingerprints: []string{"SHA256:unused"}}
	controller := &controller{document: config.Document{
		Hosts:  []config.Host{target, cached, unused},
		Relays: []config.RelaySuccess{{EndpointAID: "local", EndpointBID: "target", RelayHostID: "cached"}},
	}, runtimePasswords: make(map[string]map[int]string), sessionSSH: make(map[string]bool)}
	routes, err := controller.routeCandidates(target, "本机")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || !routes[1].cached || routes[1].relayID != "cached" {
		t.Fatalf("unexpected candidates: %#v", routes)
	}
}
