//go:build !windows

package agentservice

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReceiveArchivePreservesModeTimeAndOwnershipWhenElevated(t *testing.T) {
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	modified := time.Unix(1_700_000_000, 0)
	uid, gid := os.Getuid(), os.Getgid()
	for _, header := range []*tar.Header{
		{Name: ".", Typeflag: tar.TypeDir, Mode: 0751, ModTime: modified, Uid: uid, Gid: gid},
		{Name: "file", Typeflag: tar.TypeReg, Mode: 0641, Size: 7, ModTime: modified, Uid: uid, Gid: gid},
		{Name: "link", Typeflag: tar.TypeSymlink, Mode: 0777, Linkname: "file", ModTime: modified, Uid: uid, Gid: gid},
	} {
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte("content")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "restored")
	if err := receiveArchiveWithOwnership(bytes.NewReader(archive.Bytes()), target, true); err != nil {
		t.Fatal(err)
	}
	for name, permissions := range map[string]os.FileMode{".": 0751, "file": 0641} {
		info, err := os.Stat(filepath.Join(target, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != permissions {
			t.Fatalf("%s mode=%o want=%o", name, info.Mode().Perm(), permissions)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if int(stat.Uid) != uid || int(stat.Gid) != gid {
			t.Fatalf("%s owner=%d:%d want=%d:%d", name, stat.Uid, stat.Gid, uid, gid)
		}
		if name == "file" && info.ModTime().Unix() != modified.Unix() {
			t.Fatalf("file mtime=%s want=%s", info.ModTime(), modified)
		}
	}
	link, err := os.Lstat(filepath.Join(target, "link"))
	if err != nil {
		t.Fatal(err)
	}
	linkStat := link.Sys().(*syscall.Stat_t)
	if int(linkStat.Uid) != uid || int(linkStat.Gid) != gid {
		t.Fatalf("link owner=%d:%d want=%d:%d", linkStat.Uid, linkStat.Gid, uid, gid)
	}
}
