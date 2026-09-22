package transfer

import (
	"context"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryAliasesCannotHideRecursiveSelfCopy(t *testing.T) {
	root := t.TempDir()
	source, alias := filepath.Join(root, "source"), filepath.Join(root, "alias")
	if err := os.MkdirAll(filepath.Join(source, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "keep"), []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, target := range []string{filepath.Join(alias, "child", "nested"), filepath.Join(alias, "new", "nested")} {
		op := Operation{Source: endpoint.NewLocal(), Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target, Overwrite: true}
		if _, err := Preflight(context.Background(), op); err == nil {
			t.Fatal("aliased recursive preflight accepted")
		}
		if _, err := Run(context.Background(), op); err == nil {
			t.Fatal("aliased recursive copy accepted")
		}
	}
	data, err := os.ReadFile(filepath.Join(source, "keep"))
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("source damaged: %q %v", data, err)
	}
}
