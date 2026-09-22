package configtext

import (
	"strings"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/config"
)

func TestConnectionsRoundTripAndPreservesTrust(t *testing.T) {
	doc := config.NewDocument()
	doc.Hosts = []config.Host{{ID: "h1", Name: "deep", RouteSpec: `u:"p"@192.0.2.1 u@192.0.2.2 --keys ",vault"`, HopFingerprints: []string{"a", "b"}}}
	doc.SOCKS = []config.SOCKSProxy{{ID: "s1", Name: "edge", Spec: "u:p@127.0.0.1:1080"}}
	text := Connections(doc)
	if !strings.Contains(text, `uhome:"EXAMPLE_PASSWORD_2"@192.0.2.10:41122`) || !strings.Contains(text, `socks 示例代理 = user:pass@ip:port`) {
		t.Fatalf("missing inline reference: %s", text)
	}
	hosts, socks, err := ParseConnections(text, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].ID != "h1" || len(hosts[0].HopFingerprints) != 2 {
		t.Fatalf("host trust was not preserved: %#v", hosts)
	}
	if len(socks) != 1 || socks[0].ID != "s1" {
		t.Fatalf("SOCKS identity was not preserved: %#v", socks)
	}
}

func TestConnectionsReportsPhysicalLine(t *testing.T) {
	_, _, err := ParseConnections("# comment\nssh broken", config.NewDocument())
	if err == nil || !strings.Contains(err.Error(), "第 2 行") {
		t.Fatalf("expected line number, got %v", err)
	}
}

func TestKeysRoundTrip(t *testing.T) {
	doc := config.NewDocument()
	doc.Keys = []config.PrivateKey{{ID: "k1", Name: "vault", PEM: "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n", Passphrase: "a b"}}
	keys, err := ParseKeys(Keys(doc), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != "k1" || keys[0].Passphrase != "a b" || !strings.Contains(keys[0].PEM, "abc") {
		t.Fatalf("unexpected keys: %#v", keys)
	}
}

func TestMarkdownFormatRoundTripWithoutBlankLines(t *testing.T) {
	input := `#主机

##主机名1

uhome:"EXAMPLE_PASSWORD_1"@10.1.1.100:41122,/bin/bash

##主机名2

uhome:"EXAMPLE_PASSWORD_2"@1.2.3.4:41122  uhome:"EXAMPLE_PASSWORD_3"@5.6.7.8:41122   uhome@1.1.2.2:41122 --keys ",,keyname1" ,/bin/zsh

##主机名3我不写shell你用登录默认的

1:1@1.1.1.1:22

#私钥

##keyname1

abc...

#socks池

##socksname1

user1:pass1@1.1.1.1:1080`
	hosts, keys, socks, err := ParseMarkdown(input, config.NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 || hosts[0].Name != "主机名1" || hosts[0].Shell != "/bin/bash" || hosts[1].Shell != "/bin/zsh" || hosts[2].Shell != "" {
		t.Fatalf("unexpected hosts: %#v", hosts)
	}
	if len(keys) != 1 || keys[0].Name != "keyname1" || keys[0].PEM != "abc...\n" {
		t.Fatalf("unexpected keys: %#v", keys)
	}
	if len(socks) != 1 || socks[0].Name != "socksname1" || socks[0].Spec != "user1:pass1@1.1.1.1:1080" {
		t.Fatalf("unexpected SOCKS: %#v", socks)
	}
	doc := config.NewDocument()
	doc.Hosts, doc.Keys, doc.SOCKS = hosts, keys, socks
	rendered := Markdown(doc)
	if strings.Contains(rendered, "\n\n") {
		t.Fatalf("rendered Markdown contains blank lines:\n%s", rendered)
	}
	if !strings.Contains(rendered, "##主机名2\n"+hosts[1].RouteSpec+",/bin/zsh") {
		t.Fatalf("route or shell changed:\n%s", rendered)
	}
}

func TestMarkdownOptionalSecurityFieldsRoundTrip(t *testing.T) {
	doc := config.NewDocument()
	doc.SOCKS = []config.SOCKSProxy{{ID: "s1", Name: "pool-a", Spec: "user:pass@127.0.0.1:1080", Disabled: true}}
	doc.Hosts = []config.Host{{
		ID: "h1", Name: "protected", RouteSpec: `user:"ssh-pass"@192.0.2.10:22`, Shell: "/bin/zsh",
		SudoPassword: "sudo-secret", RootUser: "root", RootPassword: "root-secret", DefaultSOCKSID: "s1", Disabled: true,
	}}
	doc.Keys = []config.PrivateKey{{ID: "k1", Name: "key-a", PEM: "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n", Passphrase: "key-secret"}}

	rendered := Markdown(doc)
	for _, wanted := range []string{"###sudo密码\nsudo-secret", "###root用户\nroot", "###root密码\nroot-secret", "###默认socks\npool-a", "###禁用\ntrue", "###口令\nkey-secret\n###私钥"} {
		if !strings.Contains(rendered, wanted) {
			t.Fatalf("missing %q in:\n%s", wanted, rendered)
		}
	}
	if strings.Contains(rendered, "\n\n") {
		t.Fatalf("rendered Markdown contains blank lines:\n%s", rendered)
	}

	hosts, keys, proxies, err := ParseMarkdown(rendered, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].SudoPassword != "sudo-secret" || hosts[0].RootUser != "root" || hosts[0].RootPassword != "root-secret" || hosts[0].DefaultSOCKSID != "pool-a" || !hosts[0].Disabled {
		t.Fatalf("host metadata lost: %#v", hosts)
	}
	if len(keys) != 1 || keys[0].Passphrase != "key-secret" || !strings.Contains(keys[0].PEM, "abc") {
		t.Fatalf("key metadata lost: %#v", keys)
	}
	if len(proxies) != 1 || !proxies[0].Disabled {
		t.Fatalf("SOCKS metadata lost: %#v", proxies)
	}
}

func TestMarkdownRejectsUnknownOrDuplicateMetadata(t *testing.T) {
	_, _, _, err := ParseMarkdown("#主机\n##h\nu@host:22\n###未知\nx\n#私钥\n#socks池\n", config.NewDocument())
	if err == nil || !strings.Contains(err.Error(), "未知字段") {
		t.Fatalf("unknown field error = %v", err)
	}
	_, _, _, err = ParseMarkdown("#主机\n##h\nu@host:22\n###禁用\ntrue\n###禁用\nfalse\n#私钥\n#socks池\n", config.NewDocument())
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("duplicate field error = %v", err)
	}
}
