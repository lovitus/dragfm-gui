package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/lovitus/dragfm-gui/internal/config"
)

func TestCreateOpenAndWrongPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	document := appconfig.NewDocument()
	document.Hosts = append(document.Hosts, appconfig.Host{ID: "h1", Name: "server", Password: "secret"})
	store, err := Create(path, "my hint", []byte("correct horse battery staple"), document)
	if err != nil {
		t.Fatal(err)
	}
	if store.Header.Hint != "my hint" {
		t.Fatalf("hint = %q", store.Header.Hint)
	}
	header, err := ReadHeader(path)
	if err != nil || header.Hint != "my hint" {
		t.Fatalf("ReadHeader() = %#v, %v", header, err)
	}
	_, opened, err := Open(path, []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	if got := opened.Hosts[0].Password; got != "secret" {
		t.Fatalf("password = %q", got)
	}
	if _, _, err := Open(path, []byte("wrong")); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password error = %v", err)
	}
}

func TestLocatePrefersExistingSystemBeforeCreatingAdjacent(t *testing.T) {
	// The exact system path is platform owned; this test locks the important
	// adjacent-file precedence without modifying the real config directory.
	dir := t.TempDir()
	executable := filepath.Join(dir, "dragfm-gui")
	if err := os.WriteFile(executable, nil, 0700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, FileName)
	if err := os.WriteFile(want, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Locate(executable)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Locate() = %q, want %q", got, want)
	}
}

func TestLocatePathPrecedenceAndFallback(t *testing.T) {
	root := t.TempDir()
	adjacent := filepath.Join(root, "portable", FileName)
	system := filepath.Join(root, "system", FileName)
	if err := os.MkdirAll(filepath.Dir(adjacent), 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := locatePaths(adjacent, system); err != nil || got != adjacent {
		t.Fatalf("new writable install = %q, %v", got, err)
	}
	if err := os.MkdirAll(filepath.Dir(system), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(system, []byte("system"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := locatePaths(adjacent, system); err != nil || got != system {
		t.Fatalf("existing system vault = %q, %v", got, err)
	}
	if err := os.WriteFile(adjacent, []byte("adjacent"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := locatePaths(adjacent, system); err != nil || got != adjacent {
		t.Fatalf("existing adjacent vault = %q, %v", got, err)
	}
	missingAdjacent := filepath.Join(root, "missing-parent", FileName)
	missingSystem := filepath.Join(root, "fallback", FileName)
	if got, err := locatePaths(missingAdjacent, missingSystem); err != nil || got != missingSystem {
		t.Fatalf("unwritable adjacent fallback = %q, %v", got, err)
	}
}

func TestVaultModeAndStaleInterruptedTemporaryAreSafe(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, FileName)
	password := []byte("correct horse battery staple")
	document := appconfig.NewDocument()
	document.Hosts = []appconfig.Host{{ID: "one", Name: "before"}}
	if _, err := Create(path, "hint", password, document); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("vault permissions = %v", info.Mode().Perm())
	}
	if err := os.WriteFile(filepath.Join(directory, ".dragfm-vault-interrupted"), []byte("partial ciphertext"), 0600); err != nil {
		t.Fatal(err)
	}
	_, opened, err := Open(path, password)
	if err != nil || len(opened.Hosts) != 1 || opened.Hosts[0].Name != "before" {
		t.Fatalf("stale temporary affected committed vault: %#v, %v", opened, err)
	}
}

func TestMigrateVersionZero(t *testing.T) {
	document := appconfig.Document{}
	if err := migrate(&document); err != nil {
		t.Fatal(err)
	}
	if document.Version != appconfig.CurrentVersion || document.UI.Theme != "system" {
		t.Fatalf("migration result: %#v", document)
	}
}

func TestCreateRequiresHintAndStrongEnoughPassword(t *testing.T) {
	if _, err := Create(filepath.Join(t.TempDir(), FileName), "", []byte("long-enough"), appconfig.NewDocument()); err == nil {
		t.Fatal("expected empty hint to fail")
	}
	if _, err := Create(filepath.Join(t.TempDir(), FileName), "hint", []byte("short"), appconfig.NewDocument()); err == nil {
		t.Fatal("expected short password to fail")
	}
}
