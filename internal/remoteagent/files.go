package remoteagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"github.com/pkg/sftp"
)

// helperCommand keeps the credential prelude separate from protocol stdin.
// sudo -k does NOT guarantee consumption of a password under NOPASSWD. A
// random frame separates the optional password from the binary protocol.
// Authenticate and execute in ONE sudo invocation: timestamp_timeout=0 and
// per-parent timestamp policies must not break a correctly supplied password.
// A random prompt proves that sudo asked for the supplied credential; a
// NOPASSWD success must not be recorded as password authentication.
func helperCommand(binary, mode string, elevated, password bool) (string, string) {
	command := quotePOSIX(binary)
	if mode != "" {
		command += " " + quotePOSIX(mode)
	}
	return sudoCommand(command, elevated, password)
}

func sudoCommand(command string, elevated, password bool) (string, string) {
	if !elevated {
		return "exec " + command, ""
	}
	if !password {
		return "exec sudo -n -- " + command, ""
	}
	prompt := "dragfm-sudo-" + randomHex(16) + "?"
	frame := quotePOSIX(sudoFrameMarker(prompt))
	inner := "set +x; set +a; unset dragfm_frame; IFS= read -r dragfm_frame || exit 74; " +
		"if [ \"$dragfm_frame\" != " + frame + " ]; then unset dragfm_frame; IFS= read -r dragfm_frame || exit 74; fi; " +
		"[ \"$dragfm_frame\" = " + frame + " ] || exit 74; unset dragfm_frame; " +
		"command printf '%s\\n' " + quotePOSIX(sudoResultMarker(prompt)) + " >&2; exec " + command
	return "set +x; set +a; exec sudo -S -k -p " + quotePOSIX(prompt) + " -- /bin/sh -c " + quotePOSIX(inner), prompt
}

func sudoResultMarker(prompt string) string { return strings.TrimSuffix(prompt, "?") + ".done" }
func sudoFrameMarker(prompt string) string  { return strings.TrimSuffix(prompt, "?") + ".stdin" }
func sudoInput(password, prompt string) string {
	if prompt == "" {
		return ""
	}
	return password + "\n" + sudoFrameMarker(prompt) + "\n"
}

// OpenFiles opens a separate task-owned SFTP channel under the SAME approved
// account/sudo choice as this helper. No listener or controller temporary file
// is needed. Close files before closing the control helper, which owns cleanup.
// The returned view borrows SSH identity/probes, not transport ownership; Exec
// remains an ordinary-account probe, not a general-purpose privileged shell.
func (s *Session) OpenFiles(ctx context.Context) (_ *endpoint.Remote, result error) {
	if s == nil || s.remote == nil {
		return nil, errors.New("remote helper is not running")
	}
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	defer func() {
		if !transfer.Retryable(result) {
			// Close must retain the installation even if this incomplete view
			// was never added to files. A working control channel is not proof
			// that the separate file command stopped.
			s.filesErr = errors.Join(s.filesErr, result)
		}
	}()
	select {
	case <-s.done:
		return nil, errors.New("remote helper is closed")
	default:
	}
	handshakeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Close signals done before taking filesMu. Let it cancel an in-flight
	// initialization instead of waiting for that initialization's mutex.
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-handshakeCtx.Done():
		}
	}()
	if s.ctx != nil {
		stopParent := context.AfterFunc(s.ctx, cancel)
		defer stopParent()
	}
	if err := handshakeCtx.Err(); err != nil {
		return nil, err
	}
	stop := watchStartup(handshakeCtx, s.remote)
	defer stop()
	channel, err := s.remote.SSHClient().NewSession()
	if err != nil {
		return nil, err
	}
	input, err := channel.StdinPipe()
	if err != nil {
		_ = channel.Close()
		return nil, err
	}
	output, err := channel.StdoutPipe()
	if err != nil {
		_ = channel.Close()
		return nil, err
	}
	var diagnostics helperOutput
	channel.Stderr = &diagnostics
	command, prompt := helperCommand(s.remote.Join(s.Directory, "dragfm-agent"), "--sftp", s.elevated, s.sudoPassword != "")
	if err := channel.Start(command); err != nil {
		_ = channel.Close()
		return nil, transfer.PreserveSource(fmt.Errorf("start helper file channel: %w", err))
	}
	if s.elevated && s.sudoPassword != "" {
		if _, err := io.WriteString(input, sudoInput(s.sudoPassword, prompt)); err != nil {
			exitErr := waitStartupExit(channel, input, s.remote)
			failure := errors.Join(fmt.Errorf("send file channel credential: %w", err), exitErr)
			if exitErr != nil {
				failure = transfer.PreserveSource(failure)
			}
			return nil, failure
		}
	}
	client, err := sftp.NewClientPipe(output, input)
	if err != nil {
		exitErr := waitStartupExit(channel, input, s.remote)
		failure := errors.Join(fmt.Errorf("helper SFTP handshake: %w: %s", err, diagnostics.String()), handshakeCtx.Err(), exitErr)
		if exitErr != nil {
			failure = transfer.PreserveSource(failure)
		}
		return nil, failure
	}
	// Use the same EOF + remote Wait contract as the system SFTP server.
	// Closing an SSH channel alone leaves a race with installation cleanup.
	transport := &systemFileTransport{client: client, channel: channel, remote: s.remote}
	files := s.remote.BorrowFileChannel(client, transport, s.fileVersion, s.physicalPath, s.trackPartial, s.setOwner, s.syncPaths)
	stop()
	if err := handshakeCtx.Err(); err != nil {
		return nil, transfer.PreserveSource(errors.Join(err, files.Close()))
	}
	select {
	case <-s.done:
		return nil, transfer.PreserveSource(errors.Join(errors.New("remote helper closed during SFTP startup"), files.Close()))
	default:
	}
	s.files = append(s.files, files)
	return files, nil
}

func (s *Session) fileVersion(ctx context.Context, path string) (uint64, uint64, error) {
	values, err := s.CallContext(ctx, "filesystem-version", map[string]string{"path": path}, nil)
	if err != nil {
		return 0, 0, err
	}
	device, deviceErr := strconv.ParseUint(values["device"], 10, 64)
	inode, inodeErr := strconv.ParseUint(values["inode"], 10, 64)
	if err := errors.Join(deviceErr, inodeErr); err != nil || inode == 0 {
		return 0, 0, errors.Join(err, errors.New("helper returned invalid file identity"))
	}
	return device, inode, nil
}

func (s *Session) trackPartial(ctx context.Context, path string, track bool) error {
	action := "forget-partial"
	if track {
		action = "track-partial"
	}
	_, err := s.CallContext(ctx, action, map[string]string{"path": path}, nil)
	return err
}

func (s *Session) physicalPath(ctx context.Context, value string) (string, error) {
	values, err := s.CallContext(ctx, "filesystem-physical", map[string]string{"path": value}, nil)
	if err != nil {
		return "", err
	}
	if !path.IsAbs(values["path"]) {
		return "", errors.New("helper returned a non-absolute physical path")
	}
	return values["path"], nil
}

func (s *Session) setOwner(ctx context.Context, path string, uid, gid uint32) error {
	_, err := s.CallContext(ctx, "filesystem-owner", map[string]string{"path": path, "uid": strconv.FormatUint(uint64(uid), 10), "gid": strconv.FormatUint(uint64(gid), 10)}, nil)
	return err
}

func (s *Session) syncPaths(ctx context.Context, paths []string) error {
	// Bound protocol frames even for a large directory manifest.
	for len(paths) > 0 {
		count := min(64, len(paths))
		data, err := json.Marshal(paths[:count])
		if err != nil {
			return err
		}
		if _, err := s.CallContext(ctx, "filesystem-sync", map[string]string{"paths": string(data)}, nil); err != nil {
			return err
		}
		paths = paths[count:]
	}
	return nil
}
