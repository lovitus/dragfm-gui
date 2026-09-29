package endpoint

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestLocalListMatchesModificationOrder(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, item := range []struct {
		name      string
		seconds   int64
		directory bool
	}{
		{"z-dir", 10, true}, {"new-file", 30, false}, {".hidden", 40, false}, {"a-tie", 20, false}, {"B-tie", 20, false},
	} {
		target := filepath.Join(directory, item.name)
		var err error
		if item.directory {
			err = os.Mkdir(target, 0700)
		} else {
			err = os.WriteFile(target, nil, 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		modified := time.Unix(1700000000+item.seconds, 0)
		if err := os.Chtimes(target, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := NewLocal().List(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".hidden", "new-file", "B-tie", "a-tie", "z-dir"}
	if len(entries) != len(want) {
		t.Fatalf("listing count=%d, want=%d", len(entries), len(want))
	}
	for i, name := range want {
		if entries[i].Name != name {
			t.Fatalf("entry %d=%q want=%q", i, entries[i].Name, name)
		}
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
