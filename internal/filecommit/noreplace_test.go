package filecommit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNoReplaceNeverClobbersConcurrentTarget(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := NoReplace(source, target); err == nil {
		t.Fatal("existing target replaced")
	}
	for name, want := range map[string]string{source: "new", target: "original"} {
		data, err := os.ReadFile(name)
		if err != nil || string(data) != want {
			t.Fatalf("%s=%q: %v", name, data, err)
		}
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := NoReplace(source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source after rename: %v", err)
	}
}
