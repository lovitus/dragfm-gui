package configtext

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
)

func TestMarkdownInsertionsDoNotReuseExistingIdentity(t *testing.T) {
	for _, collection := range []string{"host", "key", "socks"} {
		t.Run(collection, func(t *testing.T) {
			text := func(insert bool) string {
				parts := []string{"#主机"}
				for _, section := range []string{"host", "key", "socks"} {
					if section == "key" {
						parts = append(parts, "#私钥")
					} else if section == "socks" {
						parts = append(parts, "#socks池")
					}
					body := map[string]string{"host": "user@192.0.2.1:22", "key": "fixture-pem", "socks": "u:p@192.0.2.2:1080"}[section]
					if insert && collection == section {
						parts = append(parts, "##inserted", body)
					}
					parts = append(parts, "##existing", body)
				}
				return strings.Join(parts, "\n")
			}
			old := config.NewDocument()
			var err error
			old.Hosts, old.Keys, old.SOCKS, err = ParseMarkdown(text(false), old)
			if err != nil {
				t.Fatal(err)
			}
			hosts, keys, proxies, err := ParseMarkdown(text(true), old)
			if err != nil {
				t.Fatal(err)
			}
			ids := make(map[string]bool)
			check := func(id, name, previous string) {
				t.Helper()
				if id == "" || ids[id] {
					t.Fatal("two visible entries share an identity after insertion")
				}
				ids[id] = true
				if name == "existing" && id != previous {
					t.Fatal("inserting text changed the retained entry's identity")
				}
			}
			for _, host := range hosts {
				check(host.ID, host.Name, old.Hosts[0].ID)
			}
			for _, key := range keys {
				check(key.ID, key.Name, old.Keys[0].ID)
			}
			for _, proxy := range proxies {
				check(proxy.ID, proxy.Name, old.SOCKS[0].ID)
			}
		})
	}
}

// Optional root references are an account boundary, not an alias for --keys.
// JSON keeps the overlay compilable on the baseline; a rejected valid document
// or lost field is a behavior failure, not a missing-symbol compiler failure.
func TestMarkdownExplicitRootKeyReferences(t *testing.T) {
	old := config.NewDocument()
	if err := json.Unmarshal([]byte(`{"hosts":[{"id":"h","name":"h","route_spec":"u@192.0.2.1:22","root_key_ids":["old-id"]}],"keys":[{"id":"old-id","name":"retired","pem":"old"}]}`), &old); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, field, declarations string
		want                      []string
		invalid                   bool
	}{
		{name: "single", field: "###root私钥\nkey-a\n", declarations: "##key-a\nfixture-pem\n", want: []string{"key-a"}},
		{name: "ordered-names", field: "###root私钥\nkey b,c\nkey-a\n", declarations: "##key-a\nfixture-pem\n##key b,c\nother-pem\n", want: []string{"key b,c", "key-a"}},
		{name: "removed-clears", declarations: "##retired\nold\n"},
		{name: "deleted-key", field: "###root私钥\nretired\n", invalid: true},
		{name: "unknown-key", field: "###root私钥\nmissing\n", invalid: true},
		{name: "repeated-key", field: "###root私钥\nkey-a\nkey-a\n", declarations: "##key-a\nfixture-pem\n", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			hosts, keys, proxies, err := ParseMarkdown("#主机\n##h\nu@192.0.2.1:22\n"+test.field+"#私钥\n"+test.declarations+"#socks池\n", old)
			if test.invalid {
				if err == nil {
					t.Fatal("invalid root reference accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			doc := config.NewDocument()
			doc.Hosts, doc.Keys, doc.SOCKS = hosts, keys, proxies
			wire, err := json.Marshal(doc.Hosts[0])
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				IDs []string `json:"root_key_ids"`
			}
			if err := json.Unmarshal(wire, &saved); err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, id := range saved.IDs {
				key := doc.KeyByID(id)
				if key == nil {
					t.Fatal("root reference is not a stable vault key ID")
				}
				names = append(names, key.Name)
			}
			if !reflect.DeepEqual(names, test.want) {
				t.Fatal("root references lost their account binding, ordering, or deletion")
			}
			text := Markdown(doc)
			if strings.Contains(text, "\n\n") {
				t.Fatal("root metadata inserted blank physical lines")
			}
			roundtrip, _, _, err := ParseMarkdown(text, doc)
			if err != nil || !reflect.DeepEqual(hosts, roundtrip) {
				t.Fatal("root metadata did not survive editing roundtrip")
			}
		})
	}
}

func TestMarkdownDeletedMetadataAndChangedSecretsAreAuthoritative(t *testing.T) {
	old := config.NewDocument()
	old.Hosts = []config.Host{{ID: "host", Name: "h", RouteSpec: "u@192.0.2.1:22", SudoPassword: "old-sudo", RootPassword: "old-root", RootUser: "root", Disabled: true, HopPasswords: []string{"hidden"}}}
	old.Keys = []config.PrivateKey{{ID: "key", Name: "k", PEM: "old-key\n", Passphrase: "old-passphrase", Fingerprint: "old-fingerprint"}}
	old.SOCKS = []config.SOCKSProxy{{ID: "proxy", Name: "p", Spec: "u:old@192.0.2.2:1080", Disabled: true, LastRTT: 3, LastSuccess: time.Now()}}
	text := "#主机\n##h\nu@192.0.2.1:22\n#私钥\n##k\nnew-key\n#socks池\n##p\nu:new@192.0.2.3:1080\n"
	hosts, keys, proxies, err := ParseMarkdown(text, old)
	if err != nil {
		t.Fatal(err)
	}
	if hosts[0].SudoPassword != "" || hosts[0].RootPassword != "" || hosts[0].RootUser != "" || hosts[0].Disabled || len(hosts[0].HopPasswords) != 0 {
		t.Fatal("removed host fields still affect the effective configuration")
	}
	if keys[0].Passphrase != "" || keys[0].Fingerprint != "" {
		t.Fatal("edited key retained removed credentials or old fingerprint")
	}
	if proxies[0].Disabled || proxies[0].LastRTT != 0 || !proxies[0].LastSuccess.IsZero() {
		t.Fatal("edited proxy retained removed disable flag or unrelated routing evidence")
	}
}

func TestEditingLoginPasswordDoesNotResetHostTrust(t *testing.T) {
	old := config.NewDocument()
	old.Hosts = []config.Host{{ID: "host", Name: "h", RouteSpec: `u:"before"@192.0.2.1:22 v@192.0.2.2:22`, HopFingerprints: []string{"first-pin", "last-pin"}}}
	for _, test := range []struct {
		name, route string
		pins        int
	}{
		{"password edit", `u:"after"@192.0.2.1:22 v@192.0.2.2:22`, 2},
		{"last server edit", `u:"after"@192.0.2.1:22 v@192.0.2.3:22`, 1},
		{"first server edit", `u:"after"@192.0.2.4:22 v@192.0.2.2:22`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			hosts, _, _, err := ParseMarkdown("#主机\n##h\n"+test.route+"\n#私钥\n#socks池\n", old)
			if err != nil {
				t.Fatal(err)
			}
			if len(hosts[0].HopFingerprints) != test.pins {
				t.Fatal("host trust did not follow the unchanged route prefix")
			}
			for index, pin := range hosts[0].HopFingerprints {
				if pin != old.Hosts[0].HopFingerprints[index] {
					t.Fatal("preserved pin changed")
				}
			}
		})
	}
}
