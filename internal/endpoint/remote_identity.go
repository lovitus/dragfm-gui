package endpoint

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"strings"
	"time"
)

// Identity is advisory for native same-machine optimizations. Prefer reading
// the well-defined machine-id files through SFTP rather than opening a shell
// for every comparison. Missing IDs (e.g. BSD/macOS) mean cross-machine safety,
// not a fabricated match or a command that can wait indefinitely.
func (r *Remote) Identity(ctx context.Context) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := Identity{Kind: SSHKind, Name: r.name, Fingerprint: r.fingerprint}
	if r.sftp == nil {
		identity, err := r.identityViaCommand(ctx)
		if !validMachineID(identity.MachineID) {
			identity.MachineID = ""
		}
		return identity, err
	}
	id, err := readMachineID(ctx, r.Open)
	result.MachineID = id
	return result, err
}

func validMachineID(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16 && strings.Trim(value, "0") != ""
}

func readMachineID(ctx context.Context, open func(context.Context, string) (io.ReadCloser, error)) (string, error) {
	for _, candidate := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		reader, err := open(ctx, candidate)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			continue
		}
		if err != nil {
			return "", err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, 257))
		closeErr := reader.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return "", err
		}
		value := strings.TrimSpace(string(data))
		if len(data) <= 256 && validMachineID(value) {
			return strings.ToLower(value), nil
		}
	}
	return "", ctx.Err()
}
