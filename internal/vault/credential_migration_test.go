package vault

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/configtext"
	"github.com/lovitus/dragfm-gui/internal/routespec"
)

func TestVersionOneCredentialOverlaysRequireOwnerReview(t *testing.T) {
	for _, password := range []string{"updated-password", `p%#@:'"\$ value`, "password\n###root用户\nnot-a-field"} {
		t.Run(password, func(t *testing.T) {
			document := config.Document{Version: 1,
				Hosts: []config.Host{{ID: "host", Name: "h", RouteSpec: `u:"old-password"@192.0.2.1:22 v@192.0.2.2:22 --keys ",key"`, HopPasswords: []string{password, "second-updated"}}},
				SOCKS: []config.SOCKSProxy{{ID: "proxy", Name: "p", Spec: "u:p%40ss@192.0.2.3:1080"}},
				Keys:  []config.PrivateKey{{ID: "key", Name: "key", PEM: "fixture-pem\n"}},
			}
			if err := migrate(&document); err != nil {
				t.Fatal(err)
			}
			lookup := func(string) (*config.PrivateKey, bool) { return &document.Keys[0], true }
			hops, err := routespec.ParseSSH(document.Hosts[0].RouteSpec, lookup)
			if err != nil {
				t.Fatal(err)
			}
			if hops[0].Credentials.Password != "old-password" || hops[1].Credentials.Password != "" || len(hops[1].Credentials.PrivateKeys) != 1 || len(document.Hosts[0].HopPasswords) != 0 {
				t.Fatal("migration activated a legacy overlay whose credential owner is unproven")
			}
			proxy, err := routespec.ParseSOCKS(document.SOCKS[0].Spec)
			if err != nil || proxy.Password != "p@ss" {
				t.Fatal("migration changed legacy URL-escaped password bytes")
			}
			text := configtext.Markdown(document)
			encoded, _ := json.Marshal([]string{password, "second-updated"})
			recovery := "###待核对旧密码\n" + string(encoded) + "\n"
			if !strings.Contains(text, recovery) {
				t.Fatal("migration lost the recoverable credentials or hid them from the editor")
			}
			// Reopening v2 must not double escape or reapply hidden passwords.
			if err := migrate(&document); err != nil {
				t.Fatal(err)
			}
			if configtext.Markdown(document) != text {
				t.Fatal("credential migration is not idempotent")
			}
			hosts, keys, proxies, err := configtext.ParseMarkdown(text, document)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip := document.Clone()
			roundTrip.Hosts, roundTrip.Keys, roundTrip.SOCKS = hosts, keys, proxies
			if configtext.Markdown(roundTrip) != text {
				t.Fatal("editing changed the inert recovery field")
			}
			hosts, keys, proxies, err = configtext.ParseMarkdown(strings.Replace(text, recovery, "", 1), document)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip.Hosts, roundTrip.Keys, roundTrip.SOCKS = hosts, keys, proxies
			if strings.Contains(configtext.Markdown(roundTrip), "###待核对旧密码") {
				t.Fatal("deleting recovery field did not discard it")
			}
		})
	}
}
