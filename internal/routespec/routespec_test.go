package routespec

import (
	"testing"

	"github.com/lovitus/dragfm-gui/internal/config"
)

func TestParseFlySSHVaultKeys(t *testing.T) {
	raw := `uhome:"first!pass^value"@192.0.2.10:41122  uhome:"second#pass"@10.0.0.11:41122   uhome@10.0.0.12:41122 --keys ",,prikeyname1"`
	hops, err := ParseSSH(raw, func(name string) (*config.PrivateKey, bool) {
		if name != "prikeyname1" {
			return nil, false
		}
		return &config.PrivateKey{Name: name, PEM: "pem"}, true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hops) != 3 || hops[0].Credentials.Password != "first!pass^value" || hops[1].Credentials.Password != "second#pass" {
		t.Fatalf("unexpected hops: %#v", hops)
	}
	if len(hops[0].Credentials.PrivateKeys) != 0 || len(hops[2].Credentials.PrivateKeys) != 1 {
		t.Fatalf("key slots were not preserved: %#v", hops)
	}
}

func TestParseSOCKSInline(t *testing.T) {
	proxy, err := ParseSOCKS("user:p%40ss@127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Username != "user" || proxy.Password != "p@ss" || proxy.Address != "127.0.0.1:1080" {
		t.Fatalf("unexpected proxy: %#v", proxy)
	}
}
