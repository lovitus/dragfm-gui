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
	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/agentproto"
	"github.com/lovitus/dragfm-gui/internal/assets"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

const markerName = ".dragfm-owner-v1"

type Session struct {
	Protocol  *agentproto.Conn
	Directory string
	remote    *endpoint.Remote
	ssh       io.Closer
	ctx       context.Context
	done      chan struct{}
	stdin     io.WriteCloser
	once      sync.Once
	abortOnce sync.Once
	closeErr  error
	callMu    sync.Mutex
	elevated  bool
}

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
	id := randomHex(12)
	requestOptions := make(map[string]string, len(options)+1)
	for key, value := range options {
		requestOptions[key] = value
	}
	requestOptions["progress"] = "true"
	request := agentproto.Request{Version: agentproto.ProtocolVersion, ID: id, Action: action, Options: requestOptions, Secret: secret}
	if err := s.Protocol.Send(request); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	for {
		var response agentproto.Response
		if err := s.Protocol.Receive(&response); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if response.ID != id || response.Version != agentproto.ProtocolVersion {
			return nil, errors.New("invalid remote helper response")
		}
		if response.Progress {
			count, err := strconv.ParseInt(response.Values["wire_bytes"], 10, 64)
			if err != nil || count < 0 {
				return nil, errors.New("invalid helper activity counter")
			}
			if s.ctx != nil {
				activity.Report(s.ctx, response.Values["action"], count)
			}
			continue
		}
		if !response.OK {
			return nil, errors.New(response.Error)
		}
		return response.Values, nil
	}
}

func (s *Session) Listen(action, job, path, token string, preserveOwner bool) (Listener, error) {
	return s.ListenAt(action, job, path, token, preserveOwner, "")
}
func (s *Session) ListenAt(action, job, path, token string, preserveOwner bool, bind string) (Listener, error) {
	values, err := s.Call(action, map[string]string{"job": job, "path": path, "preserve_owner": strconv.FormatBool(preserveOwner), "bind": bind, "ack": "true"}, map[string]string{"token": token})
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

func (s *Session) SCP(action, source, target, route string, directory bool) error {
	return s.SCPContext(context.Background(), action, source, target, route, directory)
}

func (s *Session) SCPContext(ctx context.Context, action, source, target, route string, directory bool) error {
	_, err := s.CallContext(ctx, action, map[string]string{"source": source, "target": target, "directory": fmt.Sprintf("%t", directory)}, map[string]string{"route": route})
	return err
}

func (s *Session) Rsync(action, source, target, route string) error {
	return s.RsyncContext(context.Background(), action, source, target, route)
}

func (s *Session) RsyncContext(ctx context.Context, action, source, target, route string) error {
	_, err := s.CallContext(ctx, action, map[string]string{"source": source, "target": target}, map[string]string{"route": route})
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

func Start(ctx context.Context, remote *endpoint.Remote, architecture string) (*Session, error) {
	return start(ctx, remote, architecture, false, "")
}

func StartElevated(ctx context.Context, remote *endpoint.Remote, architecture, sudoPassword string) (*Session, error) {
	return start(ctx, remote, architecture, true, sudoPassword)
}

func start(ctx context.Context, remote *endpoint.Remote, architecture string, elevated bool, sudoPassword string) (*Session, error) {
	if remote == nil {
		return nil, errors.New("remote endpoint is required")
	}
	payload, expectedHash, err := assets.LinuxAgent(architecture)
	if err != nil {
		return nil, err
	}
	nonce := randomHex(16)
	directory := "/tmp/.dragfm-" + nonce
	if err := remote.MkdirAll(ctx, directory, 0700); err != nil {
		return nil, err
	}
	cleanup := func() { _ = remote.Remove(context.Background(), directory, true) }
	markerData, _ := json.Marshal(marker{Version: 1, Created: time.Now().UTC(), Nonce: nonce})
	if err := writeAtomic(ctx, remote, remote.Join(directory, markerName), markerData, 0600); err != nil {
		cleanup()
		return nil, err
	}
	binaryPath := remote.Join(directory, "dragfm-agent")
	if err := writeAtomic(ctx, remote, binaryPath, payload, 0700); err != nil {
		cleanup()
		return nil, err
	}
	actualHash, err := remoteHash(ctx, remote, binaryPath)
	if err != nil || actualHash != expectedHash {
		cleanup()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("remote helper SHA-256 mismatch")
	}
	sshSession, err := remote.SSHClient().NewSession()
	if err != nil {
		cleanup()
		return nil, err
	}
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, err
	}
	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, err
	}
	var stderr helperOutput
	sshSession.Stderr = &stderr
	if elevated {
		var uid bytes.Buffer
		if err := remote.Exec(ctx, "id -u", endpoint.ExecOptions{Stdout: &uid}); err == nil && strings.TrimSpace(uid.String()) == "0" {
			elevated = false
		}
	}
	command := "exec " + quotePOSIX(binaryPath)
	if elevated {
		command = "exec sudo -n -- " + quotePOSIX(binaryPath)
		if sudoPassword != "" {
			// -k guarantees sudo consumes exactly the password line before the
			// helper protocol starts. A cached sudo ticket must never leave the
			// password bytes for the helper to mistake for its handshake.
			command = "sudo -S -k -p '' -v && exec sudo -n -- " + quotePOSIX(binaryPath)
		}
	}
	if err := sshSession.Start(command); err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, fmt.Errorf("start remote helper: %w", err)
	}
	if elevated && sudoPassword != "" {
		if _, err := io.WriteString(stdin, sudoPassword+"\n"); err != nil {
			_ = sshSession.Close()
			cleanup()
			return nil, fmt.Errorf("send sudo credential: %w", err)
		}
	}
	handshakeCtx, stopDeadline := context.WithTimeout(ctx, 15*time.Second)
	stopHandshake := context.AfterFunc(handshakeCtx, func() { _ = sshSession.Close() })
	protocol, err := agentproto.Client(stdout, stdin)
	stopHandshake()
	stopDeadline()
	if err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, fmt.Errorf("helper handshake: %w: %s", err, stderr.String())
	}
	session := &Session{Protocol: protocol, Directory: directory, remote: remote, ssh: sshSession, stdin: stdin, elevated: elevated, ctx: ctx, done: make(chan struct{})}
	if _, err := session.Call("cleanup-stale-temps", map[string]string{"keep": directory, "older_seconds": strconv.FormatInt(int64((24*time.Hour)/time.Second), 10)}, nil); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("clean stale remote helpers: %w", err)
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

func CleanupStale(ctx context.Context, remote *endpoint.Remote, olderThan time.Duration) error {
	entries, err := remote.List(ctx, "/tmp")
	if err != nil {
		return err
	}
	now := time.Now()
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name) < len(".dragfm-")+16 || entry.Name[:len(".dragfm-")] != ".dragfm-" || now.Sub(entry.Modified) < olderThan {
			continue
		}
		markerPath := remote.Join(entry.Path, markerName)
		reader, err := remote.Open(ctx, markerPath)
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, 4096))
		_ = reader.Close()
		var owner marker
		if readErr != nil || json.Unmarshal(data, &owner) != nil || owner.Version != 1 || owner.Nonce == "" || entry.Name != ".dragfm-"+owner.Nonce || now.Sub(owner.Created) < olderThan {
			continue
		}
		_ = remote.Remove(ctx, entry.Path, true)
	}
	return nil
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
