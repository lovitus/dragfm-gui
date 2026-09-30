package webgui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

func TestLiteralSOCKSAndRecoveryPasswordsDoNotLeakIntoLogs(t *testing.T) {
	for _, value := range []string{"user:p@ss@192.0.2.1:1080", "user:p:#@ss@192.0.2.1:1080", "###待核对旧密码\n[\"old-password\",\"other-password\"]\n#私钥"} {
		redacted := redact(value)
		for _, secret := range []string{"ss@", "old-password", "other-password"} {
			if strings.Contains(redacted, secret) {
				t.Fatal("accepted credential syntax left a secret suffix in logs")
			}
		}
	}
}

func TestConfigSaveFailureLeavesEffectiveAndPersistedStateUnchanged(t *testing.T) {
	app := unlockedTestApp(t)
	beforeText := "#主机\n##existing\nu:before@192.0.2.1:22\n#私钥\n#socks池\n"
	if _, err := app.SaveConfigTexts(beforeText); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	before := app.document.Clone()
	hostID := before.Hosts[0].ID
	app.runtimePasswords[hostID] = map[int]string{0: "session-before"}
	app.sessionSSH[hostID] = true
	actualPath := app.store.Path
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, nil, 0600); err != nil {
		app.mu.Unlock()
		t.Fatal(err)
	}
	app.store.Path = filepath.Join(parentFile, vault.FileName)
	app.mu.Unlock()
	defer func() { app.mu.Lock(); app.store.Path = actualPath; app.mu.Unlock() }()
	_, err := app.SaveConfigTexts("#主机\n##existing\nu:changed@192.0.2.2:22\n#私钥\n#socks池\n")
	if err == nil {
		t.Fatal("expected deterministic filesystem save failure")
	}
	app.mu.RLock()
	after := app.document.Clone()
	password, eligible := app.runtimePasswords[hostID][0], app.sessionSSH[hostID]
	app.mu.RUnlock()
	if !reflect.DeepEqual(before, after) || password != "session-before" || !eligible {
		t.Fatal("failed vault write changed the effective configuration or caches")
	}
	_, persisted, err := vault.Open(actualPath, []byte("test master password"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, persisted) {
		t.Fatal("failed edit changed the committed vault")
	}
}

func TestConfigEditInvalidatesOnlyAffectedSessionAndRelayCache(t *testing.T) {
	app := unlockedTestApp(t)
	text := "#主机\n##a\nu@192.0.2.1:22\n##b\nu@192.0.2.2:22\n##relay\nu@192.0.2.3:22\n#私钥\n#socks池\n"
	if _, err := app.SaveConfigTexts(text); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	aID, bID, relayID := app.document.Hosts[0].ID, app.document.Hosts[1].ID, app.document.Hosts[2].ID
	for _, id := range []string{aID, bID, relayID} {
		app.runtimePasswords[id] = map[int]string{0: "runtime-fixture"}
		app.sessionSSH[id] = true
	}
	app.document.Relays = []config.RelaySuccess{{EndpointAID: aID, EndpointBID: bID, RelayHostID: relayID}, {EndpointAID: "local", EndpointBID: bID, RelayHostID: relayID}}
	app.mu.Unlock()
	if _, err := app.SaveConfigTexts("#主机\n##a\nu@192.0.2.4:22\n##b\nu@192.0.2.2:22\n##relay\nu@192.0.2.3:22\n#私钥\n#socks池\n"); err != nil {
		t.Fatal(err)
	}
	app.mu.RLock()
	defer app.mu.RUnlock()
	if app.runtimePasswords[aID] != nil || app.sessionSSH[aID] {
		t.Fatal("edited endpoint retained previously authenticated credentials or relay eligibility")
	}
	if app.runtimePasswords[bID][0] != "runtime-fixture" || !app.sessionSSH[bID] {
		t.Fatal("unrelated session was invalidated")
	}
	if len(app.document.Relays) != 1 || app.document.Relays[0].EndpointAID != "local" {
		t.Fatal("successful route cache still refers to edited endpoint")
	}
}
