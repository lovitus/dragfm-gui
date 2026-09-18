package agentservice

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func craftedArchive(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tarball := tar.NewWriter(gz)
	for _, header := range headers {
		if err := tarball.WriteHeader(header); err != nil { t.Fatal(err) }
		if header.Typeflag == tar.TypeReg { if _, err := tarball.Write(bytes.Repeat([]byte{'x'}, int(header.Size))); err != nil { t.Fatal(err) } }
	}
	if err := tarball.Close(); err != nil { t.Fatal(err) }
	if err := gz.Close(); err != nil { t.Fatal(err) }
	return data.Bytes()
}

func TestArchiveCannotWriteThroughSymlink(t *testing.T) {
	for _, link := range []string{"absolute", "relative", "root-file"} {
		t.Run(link, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(root, "outside")
			if err := os.Mkdir(outside, 0700); err != nil { t.Fatal(err) }
			linkname := outside
			if link == "relative" { linkname = "../outside" }
			headers := []*tar.Header{{Name: ".", Typeflag: tar.TypeDir, Mode: 0700}, {Name: "link", Typeflag: tar.TypeSymlink, Linkname: linkname}, {Name: "link/pwned", Typeflag: tar.TypeReg, Size: 4, Mode: 0600}}
			if link == "root-file" { headers = []*tar.Header{{Name: ".", Typeflag: tar.TypeSymlink, Linkname: outside}, {Name: "pwned", Typeflag: tar.TypeReg, Size: 4}} }
			if err := receiveArchive(bytes.NewReader(craftedArchive(t, headers...)), filepath.Join(root, "partial")); err == nil { t.Fatal("unsafe archive accepted") }
			if _, err := os.Stat(filepath.Join(outside, "pwned")); !os.IsNotExist(err) { t.Fatalf("outside destination was modified: %v", err) }
		})
	}
}

func TestArchiveRequiresRootAndValidGzipTrailer(t *testing.T) {
	valid := craftedArchive(t, &tar.Header{Name: ".", Typeflag: tar.TypeReg, Mode: 0600, Size: 4})
	badCRC := append([]byte(nil), valid...)
	badCRC[len(badCRC)-8] ^= 1
	for name, archive := range map[string][]byte{
		"empty": craftedArchive(t), "missing-root": craftedArchive(t, &tar.Header{Name: "file", Typeflag: tar.TypeReg}),
		"truncated": valid[:len(valid)-4], "checksum": badCRC,
		"duplicate": craftedArchive(t, &tar.Header{Name: ".", Typeflag: tar.TypeDir}, &tar.Header{Name: ".", Typeflag: tar.TypeDir}),
	} {
		t.Run(name, func(t *testing.T) { if err := receiveArchive(bytes.NewReader(archive), filepath.Join(t.TempDir(), "partial")); err == nil { t.Fatal("invalid archive accepted") } })
	}
}

func TestArchiveReadOnlyDirectoryRestoredAfterChildren(t *testing.T) {
	archive := craftedArchive(t, &tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0500}, &tar.Header{Name: "dir", Typeflag: tar.TypeDir, Mode: 0500}, &tar.Header{Name: "dir/file", Typeflag: tar.TypeReg, Size: 4, Mode: 0400})
	target := filepath.Join(t.TempDir(), "partial")
	t.Cleanup(func() { _ = os.Chmod(target, 0700); _ = os.Chmod(filepath.Join(target, "dir"), 0700) })
	if err := receiveArchive(bytes.NewReader(archive), target); err != nil { t.Fatal(err) }
	for _, name := range []string{target, filepath.Join(target, "dir")} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0500 { t.Fatalf("directory mode not restored: %v %v", info, err) }
	}
}
