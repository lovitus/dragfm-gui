package webgui

import (
	"path/filepath"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/config"
)

func TestSessionPasswordSupersedesConfiguredInlinePassword(t *testing.T) {
	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	host := config.Host{ID: "host-1", Name: "target", RouteSpec: `user:"configured-password"@127.0.0.1:22`, HopFingerprints: []string{"SHA256:test"}}
	app.document.Hosts = []config.Host{host}

	route, err := app.routeForHost(host)
	if err != nil {
		t.Fatal(err)
	}
	if route.Hops[0].Credentials.Password != "configured-password" {
		t.Fatalf("configured password=%q", route.Hops[0].Credentials.Password)
	}

	app.runtimePasswords[host.ID] = map[int]string{0: "session-password"}
	route, err = app.routeForHost(host)
	if err != nil {
		t.Fatal(err)
	}
	if route.Hops[0].Credentials.Password != "session-password" {
		t.Fatalf("session password did not supersede stale configured value: %q", route.Hops[0].Credentials.Password)
	}
}
