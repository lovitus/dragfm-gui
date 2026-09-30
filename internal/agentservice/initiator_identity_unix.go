//go:build !windows

package agentservice

import (
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/flyssh/flyssh/pkg/connector"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

func initiatingAccountIdentity() ([]connector.PrivateKey, bool) {
	uid := strconv.Itoa(os.Geteuid())
	agentAllowed := false
	if original := os.Getenv("SUDO_UID"); original == "" || original == uid {
		if info, err := os.Stat(os.Getenv("SSH_AUTH_SOCK")); err == nil {
			agentAllowed = info.Mode()&os.ModeSocket != 0 && effectiveOwner(info)
		}
	}
	account, err := user.LookupId(uid)
	if err != nil || !filepath.IsAbs(account.HomeDir) {
		return nil, agentAllowed
	}
	// HOME may survive sudo and point at another account. Use the OS account
	// database, then keep discovery inside its private, owned .ssh directory.
	directory, err := os.OpenFile(filepath.Join(account.HomeDir, ".ssh"), os.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, agentAllowed
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() || !effectiveOwner(info) || info.Mode().Perm()&0022 != 0 {
		return nil, agentAllowed
	}
	var keys []connector.PrivateKey
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		// Do not follow a key symlink or wait on a FIFO substituted for a key.
		fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		file := os.NewFile(uintptr(fd), name)
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || !effectiveOwner(info) || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
			_ = file.Close()
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 65537))
		_ = file.Close()
		if readErr != nil || len(data) > 65536 {
			continue
		}
		// An encrypted/unreadable local key must not suppress a usable vault
		// key or password. Its passphrase is not guessed or taken from another
		// vault entry. Explicit encrypted keys still use their bound phrase.
		if _, err := ssh.ParsePrivateKey(data); err == nil {
			keys = append(keys, connector.PrivateKey{PEM: data})
		}
	}
	return keys, agentAllowed
}
