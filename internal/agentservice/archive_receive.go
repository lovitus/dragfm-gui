package agentservice

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// receiveArchiveWithOwnership only writes through directory-scoped handles.
// Lexical '..' checks alone are insufficient: an earlier archive member could
// create a symlink and make a later regular file escape the staging directory.
func receiveArchiveWithOwnership(reader io.Reader, target string, preserveOwner bool) error {
	target = filepath.Clean(target)
	parent, err := os.OpenRoot(filepath.Dir(target))
	if err != nil { return err }
	defer parent.Close()
	name := filepath.Base(target)
	if name == "." || name == string(filepath.Separator) { return errors.New("invalid archive target") }
	if _, err := parent.Lstat(name); err == nil { return os.ErrExist } else if !errors.Is(err, os.ErrNotExist) { return err }
	compressed, err := gzip.NewReader(reader)
	if err != nil { return err }
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	first, err := archive.Next()
	if err != nil { return fmt.Errorf("archive has no root member: %w", err) }
	if first.Name != "." { return errors.New("archive must start with exactly one root member") }
	var scope *os.Root
	defer func() { if scope != nil { _ = scope.Close() } }()
	var directories []*tar.Header
	if first.Typeflag == tar.TypeDir {
		if err := parent.Mkdir(name, 0700); err != nil { return err }
		scope, err = parent.OpenRoot(name)
		if err != nil { return err }
		directories = append(directories, first)
	} else if err := extractArchiveMember(parent, name, first, archive, preserveOwner); err != nil {
		return err
	}
	seen := map[string]bool{".": true}
	for {
		header, nextErr := archive.Next()
		if errors.Is(nextErr, io.EOF) { break }
		if nextErr != nil { return nextErr }
		if scope == nil { return errors.New("non-directory archive root has child members") }
		member := filepath.Clean(filepath.FromSlash(header.Name))
		if !filepath.IsLocal(member) || member == "." || seen[member] { return fmt.Errorf("invalid or duplicate archive member %q", header.Name) }
		seen[member] = true
		// Reject aliases through any symlink member, including links that happen
		// to resolve inside the root. os.Root additionally prevents escape if a
		// different process races these checks by replacing a parent directory.
		parts := strings.Split(member, string(filepath.Separator))
		for index := 1; index < len(parts); index++ {
			directory := filepath.Join(parts[:index]...)
			info, statErr := scope.Lstat(directory)
			if statErr != nil { return fmt.Errorf("archive parent %q: %w", directory, statErr) }
			if !info.IsDir() { return fmt.Errorf("archive parent %q is not a directory", directory) }
		}
		if header.Typeflag == tar.TypeDir {
			if err := scope.Mkdir(member, 0700); err != nil { return err }
			copy := *header
			copy.Name = member
			directories = append(directories, &copy)
			continue
		}
		if err := extractArchiveMember(scope, member, header, archive, preserveOwner); err != nil { return err }
	}
	// tar.Reader stops at the tar terminator, before gzip's checksum/trailer.
	// Drain only a bounded zero-padding tail and require a valid gzip checksum.
	var trailer [4096]byte
	remaining := 1 << 20
	for {
		count, readErr := compressed.Read(trailer[:])
		remaining -= count
		if remaining < 0 { return errors.New("excessive archive padding") }
		for _, value := range trailer[:count] { if value != 0 { return errors.New("unexpected trailing archive data") } }
		if errors.Is(readErr, io.EOF) { break }
		if readErr != nil { return readErr }
	}
	sort.SliceStable(directories, func(i, j int) bool { return len(directories[i].Name) > len(directories[j].Name) })
	for _, directory := range directories {
		if err := restoreArchiveMetadata(scope, directory.Name, directory, preserveOwner); err != nil { return err }
	}
	return nil
}

func extractArchiveMember(scope *os.Root, name string, header *tar.Header, reader io.Reader, preserveOwner bool) error {
	switch header.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		file, err := scope.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil { return err }
		count, copyErr := io.Copy(file, reader)
		if count != header.Size && copyErr == nil { copyErr = io.ErrUnexpectedEOF }
		if err := errors.Join(copyErr, file.Sync(), file.Close()); err != nil { return err }
		return restoreArchiveMetadata(scope, name, header, preserveOwner)
	case tar.TypeSymlink:
		if err := scope.Symlink(header.Linkname, name); err != nil { return err }
		if preserveOwner { return scope.Lchown(name, header.Uid, header.Gid) }
		return nil
	default:
		return fmt.Errorf("unsupported archive type %d", header.Typeflag)
	}
}

func restoreArchiveMetadata(scope *os.Root, name string, header *tar.Header, preserveOwner bool) error {
	// Ownership first: chown may clear special mode bits. Unprivileged copies
	// intentionally preserve only permission bits, never acquire setuid/setgid.
	if preserveOwner { if err := scope.Chown(name, header.Uid, header.Gid); err != nil { return err } }
	mode := header.FileInfo().Mode().Perm()
	if preserveOwner { mode |= header.FileInfo().Mode() & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky) }
	if err := scope.Chmod(name, mode); err != nil { return err }
	return scope.Chtimes(name, header.ModTime, header.ModTime)
}
