package endpoint

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemChildFrameAndExclusiveRename(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	nonce := strings.Repeat("a", 64)
	args := []string{FilesystemChildFlag, "write", nonce, src}
	if handled, code := FilesystemChildMain(args, strings.NewReader("unframed"), io.Discard, io.Discard); !handled || code == 0 {
		t.Fatal("unframed writer accepted")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("unframed writer modified filesystem")
	}
	body := []byte("binary\x00data\n")
	if _, code := FilesystemChildMain(args, bytes.NewReader(append([]byte("ignored-password\ndragfm-fs-v1:"+nonce+"\n"), body...)), io.Discard, io.Discard); code != 0 {
		t.Fatalf("framed write exit %d", code)
	}
	got, err := os.ReadFile(src)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("file contains frame/credential: %q %v", got, err)
	}
	if err := os.WriteFile(dst, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, code := FilesystemChildMain([]string{FilesystemChildFlag, "rename", nonce, src, dst, "false"}, strings.NewReader("dragfm-fs-v1:"+nonce+"\n"), io.Discard, io.Discard); code != 73 {
		t.Fatalf("exclusive rename exit %d", code)
	}
	got, _ = os.ReadFile(dst)
	if string(got) != "existing" {
		t.Fatal("no-overwrite rename clobbered target")
	}
}
