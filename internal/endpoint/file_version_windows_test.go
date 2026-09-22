//go:build windows

package endpoint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsFileIdentitySurvivesRenameAndDetectsReplacement(t *testing.T) {
	root := t.TempDir()
	source, renamed := filepath.Join(root, "source"), filepath.Join(root, "renamed")
	if err := os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	device, inode, err := localFileVersion(source)
	if err != nil || inode == 0 {
		t.Fatalf("identity: %d/%d %v", device, inode, err)
	}
	if err := os.Rename(source, renamed); err != nil {
		t.Fatal(err)
	}
	d2, i2, err := localFileVersion(renamed)
	if err != nil || d2 != device || i2 != inode {
		t.Fatalf("rename changed identity: %d/%d %v", d2, i2, err)
	}
	if err := os.WriteFile(source, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	_, i3, err := localFileVersion(source)
	if err != nil || i3 == inode {
		t.Fatalf("replacement not detected: %d %v", i3, err)
	}
}
