package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSmokeRequiresExplicitIsolatedDirectory(t *testing.T) {
	if smoke, err := readNativeSmoke(nil); err != nil || smoke != nil {
		t.Fatal("normal startup enabled smoke hooks")
	}
	if _, err := readNativeSmoke([]string{"--native-smoke-dir", "relative"}); err == nil {
		t.Fatal("relative vault override accepted")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".dragfm-native-test")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeSmoke([]string{"--native-smoke-dir", dir}); err == nil {
		t.Fatal("unmarked directory accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "SMOKE_ONLY"), []byte("dragfm-native-smoke-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "smoke.js"), []byte("void 0;"), 0600); err != nil {
		t.Fatal(err)
	}
	if smoke, err := readNativeSmoke([]string{"--native-smoke-dir", dir}); err != nil || smoke == nil {
		t.Fatalf("valid isolated fixture rejected: %v", err)
	}
}
