package remoteagent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	flytransfer "github.com/flyssh/flyssh/pkg/transfer"
	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/agentproto"
	"github.com/lovitus/dragfm-gui/internal/assets"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

const markerName = ".dragfm-owner-v1"

type Session struct {
	Protocol          *agentproto.Conn
	Directory         string
	remote            *endpoint.Remote
	ssh               io.Closer
	ctx               context.Context
	done              chan struct{}
	stdin             io.WriteCloser
	once              sync.Once
	abortOnce         sync.Once
	closeErr          error
	callMu            sync.Mutex
	elevated          bool
	sudoAuthenticated bool
	sudoPassword      string
	filesMu           sync.Mutex
	files             []*endpoint.Remote
	filesErr          error // protected by filesMu; unconfirmed file startup
	installation      config.WorkspaceRecord
	journal           WorkspaceJournal
}

// WorkspaceJournal commits intent/inode records synchronously. remove=true is
// sent only after acknowledged cleanup, never merely after closing a channel.
type WorkspaceJournal func(record config.WorkspaceRecord, remove bool) error

// True only after a password-bearing sudo invocation completed the helper
// handshake. A configured root SSH route or an already-root account is not
// evidence that an offered sudo password was accepted.
func (s *Session) SudoAuthenticated() bool { return s.sudoAuthenticated }

type Listener struct {
	Job       string
	Port      string
	Pin       string
	Addresses []string
}

type marker struct {
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	Nonce   string    `json:"nonce"`
}

func (s *Session) Call(action string, options, secret map[string]string) (map[string]string, error) {
	return s.CallContext(context.Background(), action, options, secret)
}

// CallContext aborts the SSH helper channel when its owning transfer is
// cancelled. A helper session is intentionally single-use after cancellation;
// this guarantees a blocked native child (rsync/scp/Hans) cannot keep the
// global queue or shutdown path stuck indefinitely.
func (s *Session) call(ctx context.Context, action string, options, secret map[string]string) (map[string]string, error) {
	if s == nil || s.Protocol == nil {
		return nil, errors.New("remote helper is not running")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stopCancellation := context.AfterFunc(ctx, s.abortTransport)
	defer stopCancellation()
	s.callMu.Lock()
	defer s.callMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Receiving actions can create the first inode before sending a response.
	// Persist the scoped absent-path intent first, in a separate read-only
	// registration round trip, before permitting any payload writer to start.
	if s.journal != nil {
		incoming := ""
		switch action {
		case "listen-receive", "connect-receive":
			incoming = options["path"]
		case "scp-download", "rsync-download":
			incoming = options["target"]
		}
		if incoming != "" {
			if !validPartialPath(incoming) {
				return nil, transfer.PreserveSource(errors.New("journaled receiver requires a scoped partial path"))
			}
			if _, err := s.exchange(ctx, "track-partial", map[string]string{"path": incoming}, nil); err != nil {
				return nil, err
			}
		}
	}
	return s.exchange(ctx, action, options, secret)
}

// callMu serializes both protocol frames and the install's durable metadata.
func (s *Session) exchange(ctx context.Context, action string, options, secret map[string]string) (map[string]string, error) {
	id := randomHex(12)
	requestOptions := make(map[string]string, len(options)+1)
	for key, value := range options {
		requestOptions[key] = value
	}
	requestOptions["progress"] = "true"
	if s.journal != nil {
		requestOptions["journal"] = "true"
	}
	request := agentproto.Request{Version: agentproto.ProtocolVersion, ID: id, Action: action, Options: requestOptions, Secret: secret}
	if err := s.Protocol.Send(request); err != nil {
		return nil, &flytransfer.ExitUnconfirmedError{Cause: errors.Join(ctx.Err(), err)}
	}
	for {
		var response agentproto.Response
		if err := s.Protocol.Receive(&response); err != nil {
			return nil, &flytransfer.ExitUnconfirmedError{Cause: errors.Join(ctx.Err(), err)}
		}
		if response.ID != id || response.Version != agentproto.ProtocolVersion {
			return nil, &flytransfer.ExitUnconfirmedError{Cause: errors.New("invalid remote helper response")}
		}
		if response.Progress {
			count, err := strconv.ParseInt(response.Values["wire_bytes"], 10, 64)
			if err != nil || count < 0 {
				return nil, &flytransfer.ExitUnconfirmedError{Cause: errors.New("invalid helper activity counter")}
			}
			if s.ctx != nil {
				activity.Report(s.ctx, response.Values["action"], count)
			}
			continue
		}
		var journalErr error
		if s.journal != nil && action != "remove-owned-temp" && response.ErrorCode != "partial_identity_unconfirmed" {
			journalErr = s.recordPartials(response.Values["partials"])
		}
		if !response.OK {
			if response.ErrorCode == "exit_unconfirmed" {
				return nil, errors.Join(&flytransfer.ExitUnconfirmedError{Cause: errors.New(response.Error)}, journalErr)
			}
			if response.ErrorCode == "partial_identity_unconfirmed" {
				return nil, transfer.PreserveSource(errors.New(response.Error))
			}
			return nil, errors.Join(errors.New(response.Error), journalErr)
		}
		return response.Values, journalErr
	}
}

func (s *Session) Listen(action, job, path, token string, preserveOwner bool) (Listener, error) {
	return s.ListenAt(action, job, path, token, preserveOwner, "")
}
func (s *Session) ListenAt(action, job, path, token string, preserveOwner bool, bind string) (Listener, error) {
	return s.ListenPort(action, job, path, token, preserveOwner, bind, "")
}

func (s *Session) ListenPort(action, job, path, token string, preserveOwner bool, bind, port string) (Listener, error) {
	values, err := s.Call(action, map[string]string{"job": job, "path": path, "preserve_owner": strconv.FormatBool(preserveOwner), "bind": bind, "port": port, "ack": "true"}, map[string]string{"token": token})
	if err != nil {
		return Listener{}, err
	}
	addresses := strings.Split(values["addresses"], ",")
	filtered := addresses[:0]
	for _, address := range addresses {
		if strings.TrimSpace(address) != "" {
			filtered = append(filtered, strings.TrimSpace(address))
		}
	}
	if values["port"] == "" || values["pin"] == "" || len(filtered) == 0 {
		return Listener{}, errors.New("remote helper returned no reachable listener address")
	}
	return Listener{Job: job, Port: values["port"], Pin: values["pin"], Addresses: filtered}, nil
}

func (s *Session) Connect(action, path, address, token, pin string, proxy *connector.SOCKS5, route string, preserveOwner bool) error {
	return s.ConnectWithCarrier("", action, path, address, token, pin, proxy, route, preserveOwner)
}

func (s *Session) ConnectWithCarrier(carrier, action, path, address, token, pin string, proxy *connector.SOCKS5, route string, preserveOwner bool) error {
	secret := map[string]string{"token": token, "pin": pin}
	if proxy != nil {
		secret["socks_address"], secret["socks_username"], secret["socks_password"] = proxy.Address, proxy.Username, proxy.Password
	}
	if route != "" {
		secret["route"] = route
	}
	_, err := s.Call(action, map[string]string{"path": path, "address": address, "preserve_owner": strconv.FormatBool(preserveOwner), "carrier": carrier, "ack": "true"}, secret)
	return err
}

func (s *Session) Wait(job string) error {
	_, err := s.Call("wait", map[string]string{"job": job}, nil)
	return err
}

func (s *Session) StopListener(job string) error {
	_, err := s.Call("listener-stop", map[string]string{"job": job}, nil)
	return err
}

func (s *Session) DiscardPartial(path string) error {
	_, err := s.Call("discard-partial", map[string]string{"path": path}, nil)
	return err
}

func (s *Session) SCP(action, source, target, route string, directory bool) error {
	return s.SCPContext(context.Background(), action, source, target, route, directory)
}

func (s *Session) SCPContext(ctx context.Context, action, source, target, route string, directory bool, peerHelper ...string) error {
	options := map[string]string{"source": source, "target": target, "directory": fmt.Sprintf("%t", directory)}
	if len(peerHelper) > 0 {
		options["peer_helper"] = peerHelper[0]
	}
	_, err := s.CallContext(ctx, action, options, map[string]string{"route": route})
	return err
}

func (s *Session) Rsync(action, source, target, route string) error {
	return s.RsyncContext(context.Background(), action, source, target, route)
}

func (s *Session) RsyncContext(ctx context.Context, action, source, target, route string, peerHelper ...string) error {
	options := map[string]string{"source": source, "target": target}
	if len(peerHelper) > 0 {
		options["peer_helper"] = peerHelper[0]
	}
	_, err := s.CallContext(ctx, action, options, map[string]string{"route": route})
	return err
}

func (s *Session) ProbeTCP(address string) (time.Duration, error) {
	values, err := s.Call("tcp-probe", map[string]string{"address": address}, nil)
	if err != nil {
		return 0, err
	}
	milliseconds, err := strconv.ParseInt(values["median_ms"], 10, 64)
	return time.Duration(milliseconds) * time.Millisecond, err
}

func (s *Session) Commit(partial, target string, overwrite bool) error {
	_, err := s.Call("path-commit", map[string]string{"partial": partial, "target": target, "overwrite": strconv.FormatBool(overwrite)}, nil)
	return err
}

func (s *Session) RemovePath(target string, directory bool) error {
	_, err := s.Call("path-remove", map[string]string{"path": target, "directory": strconv.FormatBool(directory)}, nil)
	return err
}

func (s *Session) Manifest(target string) (transfer.Manifest, error) {
	values, err := s.Call("filesystem-manifest", map[string]string{"path": target}, nil)
	if err != nil {
		return transfer.Manifest{}, err
	}
	var manifest transfer.Manifest
	if err := json.Unmarshal([]byte(values["manifest"]), &manifest); err != nil {
		return transfer.Manifest{}, fmt.Errorf("decode helper manifest: %w", err)
	}
	return manifest, nil
}

func InstallHans(ctx context.Context, session *Session, architecture string) (string, error) {
	if session == nil || session.remote == nil {
		return "", errors.New("remote helper is not running")
	}
	payload, expectedHash, err := assets.LinuxHans(architecture)
	if err != nil {
		return "", err
	}
	path := session.remote.Join(session.Directory, "hans")
	if err := writeAtomic(ctx, session.remote, path, payload, 0700); err != nil {
		return "", err
	}
	actualHash, err := remoteHash(ctx, session.remote, path)
	if err != nil || actualHash != expectedHash {
		if err != nil {
			return "", err
		}
		return "", errors.New("remote Hans SHA-256 mismatch")
	}
	return path, nil
}

func (s *Session) StartHansServer(job, binary, network, identity, lease, passphrase string) (string, error) {
	values, err := s.Call("hans-server-start", map[string]string{"job": job, "binary": binary, "network": network, "identity": identity, "lease": lease}, map[string]string{"passphrase": passphrase})
	return values["fingerprint"], err
}

func (s *Session) StartHansClient(job, binary, server, socks, identity, passphrase, fingerprint string) error {
	_, err := s.Call("hans-client-start", map[string]string{"job": job, "binary": binary, "server": server, "socks": socks, "identity": identity}, map[string]string{"passphrase": passphrase, "fingerprint": fingerprint})
	return err
}

func (s *Session) ProcessDiagnostics(ctx context.Context, job string) (string, error) {
	values, err := s.CallContext(ctx, "process-diagnostics", map[string]string{"job": job}, nil)
	return values["output"], err
}
func (s *Session) ProcessStatus(ctx context.Context, job string) error {
	_, err := s.CallContext(ctx, "process-status", map[string]string{"job": job}, nil)
	return err
}
func (s *Session) ProbeSOCKSTCP(ctx context.Context, proxyAddress, targetAddress string) error {
	_, err := s.CallContext(ctx, "socks-tcp-probe", map[string]string{"proxy": proxyAddress, "target": targetAddress}, nil)
	return err
}
func (s *Session) StopProcess(job string) error {
	_, err := s.Call("process-stop", map[string]string{"job": job}, nil)
	return err
}

// Start requires a task-owned transport (normally Remote.Fork). Cancellation
// may close it to unblock local I/O; that is never remote process-exit evidence.
func Start(ctx context.Context, remote *endpoint.Remote, architecture string, journals ...WorkspaceJournal) (*Session, error) {
	return start(ctx, remote, architecture, false, "", journals...)
}

func StartElevated(ctx context.Context, remote *endpoint.Remote, architecture, sudoPassword string, journals ...WorkspaceJournal) (*Session, error) {
	return start(ctx, remote, architecture, true, sudoPassword, journals...)
}

func start(ctx context.Context, remote *endpoint.Remote, architecture string, elevated bool, sudoPassword string, journals ...WorkspaceJournal) (*Session, error) {
	if remote == nil {
		return nil, errors.New("remote endpoint is required")
	}
	if strings.ContainsAny(sudoPassword, "\r\n") {
		return nil, errors.New("sudo password must fit on one input line")
	}
	if len(journals) > 1 {
		return nil, errors.New("helper installation requires at most one recovery journal")
	}
	var journal WorkspaceJournal
	var registrars []func(config.WorkspaceRecord) error
	if len(journals) == 1 && journals[0] != nil {
		journal = journals[0]
		registrars = append(registrars, func(record config.WorkspaceRecord) error { return journal(record, false) })
	}
	payload, expectedHash, err := assets.LinuxAgent(architecture)
	if err != nil {
		return nil, err
	}
	installation, err := createWorkspaceDirectory(ctx, remote, "/tmp", registrars...)
	if err != nil {
		return nil, err
	}
	directory := installation.Directory
	// Before exec there cannot be a helper child. Still use the pinned marker
	// and exclusive directory lock, never a blind recursive absolute-path rm.
	cleanup := func(failure error) error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := installation.remove(cleanupCtx); err != nil {
			return errors.Join(failure, transfer.PreserveSource(fmt.Errorf("helper installation %q retained: %w", directory, err)))
		}
		if journal != nil {
			if err := journal(installation.Record(), true); err != nil {
				return errors.Join(failure, transfer.PreserveSource(fmt.Errorf("helper installation recovery record could not be retired: %w", err)))
			}
		}
		return failure
	}
	binaryPath := remote.Join(directory, "dragfm-agent")
	if err := writeAtomic(ctx, remote, binaryPath, payload, 0700); err != nil {
		return nil, cleanup(err)
	}
	actualHash, err := remoteHash(ctx, remote, binaryPath)
	if err != nil || actualHash != expectedHash {
		if err != nil {
			return nil, cleanup(err)
		}
		return nil, cleanup(errors.New("remote helper SHA-256 mismatch"))
	}
	handshakeCtx, stopDeadline := context.WithTimeout(ctx, 15*time.Second)
	defer stopDeadline()
	stopHandshake := watchStartup(handshakeCtx, remote)
	defer stopHandshake()
	sshSession, err := remote.SSHClient().NewSession()
	if err != nil {
		return nil, cleanup(err)
	}
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		_ = sshSession.Close()
		return nil, cleanup(err)
	}
	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		_ = sshSession.Close()
		return nil, cleanup(err)
	}
	var stderr helperOutput
	sshSession.Stderr = &stderr
	if elevated {
		var uid bytes.Buffer
		if err := remote.Exec(ctx, "id -u", endpoint.ExecOptions{Stdout: &uid}); err == nil && strings.TrimSpace(uid.String()) == "0" {
			elevated = false
		}
	}
	command, sudoPrompt := helperCommand(binaryPath, "", elevated, sudoPassword != "")
	if err := sshSession.Start(command); err != nil {
		_ = sshSession.Close()
		// The exec request may have reached the server before its reply was
		// lost. Keep the marked directory rather than guessing it never ran.
		return nil, &flytransfer.ExitUnconfirmedError{Cause: fmt.Errorf("start remote helper: %w", err)}
	}
	if elevated && sudoPassword != "" {
		if _, err := io.WriteString(stdin, sudoInput(sudoPassword, sudoPrompt)); err != nil {
			exitErr := waitStartupExit(sshSession, stdin, remote)
			failure := fmt.Errorf("send sudo credential: %w", err)
			if exitErr != nil {
				return nil, transfer.PreserveSource(errors.Join(failure, exitErr))
			}
			return nil, cleanup(failure)
		}
	}
	protocol, err := agentproto.Client(stdout, stdin)
	if err == nil && sudoPrompt != "" {
		// stdout and stderr are drained concurrently. Wait for the explicit
		// stderr boundary before deciding whether sudo requested a password.
		err = stderr.waitFor(handshakeCtx, sudoResultMarker(sudoPrompt))
	}
	stopHandshake()
	err = errors.Join(err, handshakeCtx.Err())
	if err != nil {
		exitErr := waitStartupExit(sshSession, stdin, remote)
		failure := fmt.Errorf("helper handshake: %w: %s", err, stderr.String())
		if exitErr != nil {
			return nil, transfer.PreserveSource(errors.Join(failure, exitErr))
		}
		return nil, cleanup(failure)
	}
	session := &Session{Protocol: protocol, Directory: directory, remote: remote, ssh: sshSession, stdin: stdin, elevated: elevated, sudoAuthenticated: sudoPrompt != "" && strings.Contains(stderr.String(), sudoPrompt), ctx: ctx, done: make(chan struct{}), installation: installation.Record(), journal: journal}
	if elevated {
		session.sudoPassword = sudoPassword
	}
	if journal == nil {
		// GUI recovery is an explicit queued operation against exact vault
		// records. Starting a new helper must not also scan unrelated installs.
		if _, err := session.Call("cleanup-stale-temps", map[string]string{"keep": directory, "older_seconds": strconv.FormatInt(int64((24*time.Hour)/time.Second), 10)}, nil); err != nil {
			return nil, errors.Join(fmt.Errorf("clean stale remote helpers: %w", err), session.Close())
		}
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-session.done:
		}
	}()
	return session, nil
}

func quotePOSIX(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// CreateDirectory creates a lease-aware helper installation. Every entrypoint
// locks it before consuming input; helper children inherit the directory fd.
// mkdir is exclusive: an existing path, including a symlink, is never adopted.
func CreateDirectory(ctx context.Context, remote endpoint.Endpoint, parent string) (string, error) {
	work, err := createWorkspaceDirectory(ctx, remote, parent)
	if err != nil {
		return "", err
	}
	return work.Directory, nil
}

func CleanupStale(ctx context.Context, remote *endpoint.Remote, olderThan time.Duration) error {
	return CleanupWorkspaces(ctx, remote, "/tmp", olderThan)
}

func writeAtomic(ctx context.Context, remote endpoint.Endpoint, path string, data []byte, mode fs.FileMode) error {
	writer, err := remote.CreateAtomic(ctx, path, mode)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Abort()
		return err
	}
	return writer.Commit()
}

func remoteHash(ctx context.Context, remote endpoint.Endpoint, path string) (string, error) {
	reader, err := remote.Open(ctx, path)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func randomHex(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
