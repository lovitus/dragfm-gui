package endpoint

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalAtomicWriter(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	target := filepath.Join(directory, "file.txt")
	local := NewLocal()
	writer, err := local.CreateAtomic(context.Background(), target, 0640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "complete"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "complete" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestLocalListDirectoriesFirst(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "a-file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "z-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	entries, err := NewLocal().List(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].IsDir() {
		t.Fatalf("unexpected order: %#v", entries)
	}
}

func TestLocalAbsBareTildeIsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewLocal().Abs(context.Background(), "~")
	if err != nil || got != home {
		t.Fatalf("home=%q got=%q err=%v", home, got, err)
	}
}
