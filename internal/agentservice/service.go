package agentservice

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/flyssh/flyssh/pkg/socks"
	flytransfer "github.com/flyssh/flyssh/pkg/transfer"
	"github.com/lovitus/dragfm-gui/internal/agentproto"
	"github.com/lovitus/dragfm-gui/internal/agentroute"
	"github.com/lovitus/dragfm-gui/internal/rsyncbridge"
)

type Service struct {
	mu        sync.Mutex
	jobs      map[string]*listenerJob
	processes map[string]*processJob
	ctx       context.Context
}

type processJob struct {
	command *exec.Cmd
	done    chan error
}

type listenerJob struct {
	listener net.Listener
	done     chan error
	port     int
	pin      string
}

func New() *Service {
	return NewWithContext(context.Background())

}

func NewWithContext(ctx context.Context) *Service {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Service{jobs: make(map[string]*listenerJob), processes: make(map[string]*processJob), ctx: ctx}
}

func (s *Service) Handle(request agentproto.Request) agentproto.Response {
	response := agentproto.Response{Version: agentproto.ProtocolVersion, ID: request.ID, OK: true, Values: make(map[string]string)}
	if request.Version != agentproto.ProtocolVersion {
		response.OK, response.Error = false, "unsupported protocol version"
		return response
	}
	var err error
	switch request.Action {
	case "probe":
		response.Values["os"], response.Values["arch"] = runtime.GOOS, runtime.GOARCH
	case "sha256":
		response.Values["sha256"], err = fileSHA256(request.Options["path"])
	case "listen-receive", "listen-send":
		var job *listenerJob
		job, err = s.startListener(request)
		if err == nil {
			response.Values["port"] = strconv.Itoa(job.port)
			response.Values["pin"] = job.pin
			response.Values["addresses"] = strings.Join(localAddresses(), ",")
		}
	case "connect-send":
		err = connect(s.ctx, request, true)
	case "connect-receive":
		err = connect(s.ctx, request, false)
	case "scp-upload", "scp-download":
		err = runSCP(s.ctx, request)
	case "rsync-upload", "rsync-download":
		err = runRsync(s.ctx, request)
	case "tcp-probe":
		response.Values["median_ms"], err = tcpProbe(request.Options["address"])
	case "hans-server-start", "hans-client-start":
		response.Values["fingerprint"], err = s.startHans(request)
	case "process-stop":
		err = s.stopProcess(request.Options["job"])
	case "remove-owned-temp":
		err = removeOwnedTemp(request.Options["path"])
	case "cleanup-stale-temps":
		seconds, parseErr := strconv.ParseInt(request.Options["older_seconds"], 10, 64)
		if parseErr != nil || seconds < 3600 {
			err = errors.New("invalid stale cleanup age")
		} else {
			err = cleanupOwnedTemps("/tmp", request.Options["keep"], time.Duration(seconds)*time.Second)
		}
	case "path-commit":
		err = commitPath(request.Options["partial"], request.Options["target"], request.Options["overwrite"] == "true")
	case "path-remove":
		err = removePath(request.Options["path"], request.Options["directory"] == "true")
	case "filesystem-manifest":
		var manifest filesystemManifest
		manifest, err = buildFilesystemManifest(request.Options["path"])
		if err == nil {
			var encoded []byte
			encoded, err = json.Marshal(manifest)
			response.Values["manifest"] = string(encoded)
		}
	case "wait":
		err = s.wait(s.ctx, request.Options["job"])
	default:
		err = errors.New("unsupported action")
	}
	if err != nil {
		response.OK, response.Error = false, err.Error()
	}
	return response
}

// These structs deliberately match transfer.Manifest's JSON representation.
// Keeping the helper side independent avoids coupling the small Linux agent to
// controller-only endpoint implementations.
type filesystemManifest struct {
	Items      []filesystemManifestItem
	Bytes      int64
	RootDevice uint64
	RootInode  uint64
}

type filesystemManifestItem struct {
	Relative   string
	Mode       os.FileMode
	Size       int64
	ModifiedNS int64
	LinkTarget string
	SHA256     string
	SourcePath string
}

func buildFilesystemManifest(root string) (filesystemManifest, error) {
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return filesystemManifest{}, err
	}
	manifest := filesystemManifest{}
	manifest.RootDevice, manifest.RootInode = filesystemVersion(info)
	var visit func(string, string, os.FileInfo) error
	visit = func(current, relative string, currentInfo os.FileInfo) error {
		item := filesystemManifestItem{Relative: filepath.ToSlash(relative), Mode: currentInfo.Mode(), Size: currentInfo.Size(), ModifiedNS: currentInfo.ModTime().UnixNano(), SourcePath: current}
		if currentInfo.Mode()&os.ModeSymlink != 0 {
			item.LinkTarget, err = os.Readlink(current)
			if err != nil {
				return err
			}
		}
		if currentInfo.Mode().IsRegular() {
			item.SHA256, err = fileSHA256(current)
			if err != nil {
				return err
			}
			manifest.Bytes += currentInfo.Size()
		}
		manifest.Items = append(manifest.Items, item)
		if !currentInfo.IsDir() {
			return nil
		}
		children, readErr := os.ReadDir(current)
		if readErr != nil {
			return readErr
		}
		for _, child := range children {
			childPath := filepath.Join(current, child.Name())
			childInfo, infoErr := os.Lstat(childPath)
			if infoErr != nil {
				return infoErr
			}
			childRelative := child.Name()
			if relative != "" {
				childRelative = filepath.Join(relative, child.Name())
			}
			if visitErr := visit(childPath, childRelative, childInfo); visitErr != nil {
				return visitErr
			}
		}
		return nil
	}
	if err := visit(root, "", info); err != nil {
		return filesystemManifest{}, err
	}
	sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].Relative < manifest.Items[j].Relative })
	return manifest, nil
}

func removePath(target string, directory bool) error {
	if directory {
		return os.RemoveAll(target)
	}
	return os.Remove(target)
}

// commitPath keeps a complete incoming root under a random sibling name until
// the transfer has finished. Directory overwrite is merge semantics; replaced
// leaf nodes use a sibling backup so a failed rename can be rolled back.
func commitPath(partial, target string, overwrite bool) error {
	partial, target = filepath.Clean(partial), filepath.Clean(target)
	if partial == target || filepath.Dir(partial) != filepath.Dir(target) {
		return errors.New("partial and target must be distinct siblings")
	}
	return commitPathInternal(partial, target, overwrite)
}

func commitPathInternal(partial, target string, overwrite bool) error {
	partialInfo, err := os.Lstat(partial)
	if err != nil {
		return err
	}
	targetInfo, targetErr := os.Lstat(target)
	if errors.Is(targetErr, os.ErrNotExist) {
		return os.Rename(partial, target)
	}
	if targetErr != nil {
		return targetErr
	}
	if !overwrite {
		return os.ErrExist
	}
	if partialInfo.IsDir() && targetInfo.IsDir() {
		entries, readErr := os.ReadDir(partial)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if err := commitPathInternal(filepath.Join(partial, entry.Name()), filepath.Join(target, entry.Name()), true); err != nil {
				return err
			}
		}
		return os.Remove(partial)
	}
	backup := target + ".dragfm-backup-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(partial, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	return os.RemoveAll(backup)
}

func runRsync(ctx context.Context, request agentproto.Request) error {
	route, err := agentroute.Decode(request.Secret["route"])
	if err != nil {
		return err
	}
	chain, err := connector.Dial(ctx, route)
	if err != nil {
		return err
	}
	defer chain.Close()
	direction := rsyncbridge.Upload
	if request.Action == "rsync-download" {
		direction = rsyncbridge.Download
	}
	return rsyncbridge.Run(ctx, chain.Final(), direction, request.Options["source"], request.Options["target"])
}

func removeOwnedTemp(path string) error {
	clean := filepath.Clean(path)
	if filepath.Dir(clean) != "/tmp" || !strings.HasPrefix(filepath.Base(clean), ".dragfm-") {
		return errors.New("refusing to remove unscoped temporary path")
	}
	marker := filepath.Join(clean, ".dragfm-owner-v1")
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("temporary ownership marker is missing")
	}
	return os.RemoveAll(clean)
}

type ownershipMarker struct {
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	Nonce   string    `json:"nonce"`
}

func cleanupOwnedTemps(root, keep string, olderThan time.Duration) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".dragfm-") {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		if filepath.Clean(directory) == filepath.Clean(keep) {
			continue
		}
		markerPath := filepath.Join(directory, ".dragfm-owner-v1")
		info, statErr := os.Lstat(markerPath)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			continue
		}
		file, openErr := os.Open(markerPath)
		if openErr != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 4097))
		_ = file.Close()
		var marker ownershipMarker
		if readErr != nil || len(data) > 4096 || json.Unmarshal(data, &marker) != nil || marker.Version != 1 || marker.Nonce == "" || entry.Name() != ".dragfm-"+marker.Nonce || marker.Created.IsZero() || now.Sub(marker.Created) < olderThan {
			continue
		}
		_ = os.RemoveAll(directory)
	}
	return nil
}

func (s *Service) startHans(request agentproto.Request) (string, error) {
	binary, identity, jobID := request.Options["binary"], request.Options["identity"], request.Options["job"]
	passphrase := request.Secret["passphrase"]
	if binary == "" || identity == "" || jobID == "" || passphrase == "" {
		return "", errors.New("Hans process options are incomplete")
	}
	passphrasePath := "/proc/self/fd/3"
	if runtime.GOOS != "linux" {
		// The production helper runs only on Linux. /dev/fd keeps the process
		// contract testable on the macOS development host without changing the
		// descriptor inherited by Hans.
		passphrasePath = "/dev/fd/3"
	}
	args := []string{"-f", "--require-v5", "--passphrase-file", passphrasePath, "--identity-file", identity}
	fingerprint := ""
	if request.Action == "hans-server-start" {
		network, lease := request.Options["network"], request.Options["lease"]
		if net.ParseIP(network) == nil || lease == "" {
			return "", errors.New("invalid Hans server network or lease path")
		}
		identityCommand := exec.Command(binary, "--show-identity", "--identity-file", identity)
		output, err := identityCommand.Output()
		if err != nil {
			return "", err
		}
		fields := strings.Fields(string(output))
		if len(fields) == 0 {
			return "", errors.New("Hans returned no server fingerprint")
		}
		fingerprint = fields[len(fields)-1]
		args = append(args, "-s", network, "--lease-file", lease)
	} else {
		server, socks, pin := request.Options["server"], request.Options["socks"], request.Secret["fingerprint"]
		if server == "" || socks == "" || pin == "" {
			return "", errors.New("invalid Hans client server, SOCKS, or fingerprint")
		}
		args = append(args, "-c", server, "--feature", "userspace", "--socks5", socks, "--server-fingerprint", pin)
	}
	command := exec.Command(binary, args...)
	configureChildLifecycle(command)
	secretReader, secretWriter, err := os.Pipe()
	if err != nil {
		return "", err
	}
	command.ExtraFiles = []*os.File{secretReader}
	// Nil attaches the child's output to the null device without the copy
	// goroutines that io.Discard would create. That also makes cancellation
	// independent of any descriptors inherited by a child process.
	command.Stdout, command.Stderr = nil, nil
	if err := command.Start(); err != nil {
		_ = secretReader.Close()
		_ = secretWriter.Close()
		return "", err
	}
	_ = secretReader.Close()
	if _, err := io.WriteString(secretWriter, passphrase+"\n"); err != nil {
		_ = secretWriter.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
		return "", err
	}
	_ = secretWriter.Close()
	job := &processJob{command: command, done: make(chan error, 1)}
	s.mu.Lock()
	if _, exists := s.processes[jobID]; exists {
		s.mu.Unlock()
		_ = command.Process.Kill()
		_ = command.Wait()
		return "", errors.New("duplicate process job")
	}
	s.processes[jobID] = job
	s.mu.Unlock()
	go func() { job.done <- command.Wait() }()
	return fingerprint, nil
}

func (s *Service) stopProcess(jobID string) error {
	s.mu.Lock()
	job := s.processes[jobID]
	delete(s.processes, jobID)
	s.mu.Unlock()
	if job == nil {
		return nil
	}
	_ = job.command.Process.Signal(os.Interrupt)
	select {
	case err := <-job.done:
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return err
	case <-time.After(2 * time.Second):
		_ = job.command.Process.Kill()
		<-job.done
		return nil
	}
}

func tcpProbe(address string) (string, error) {
	if address == "" {
		return "", errors.New("probe address is empty")
	}
	values := make([]time.Duration, 0, 3)
	for index := 0; index < 3; index++ {
		started := time.Now()
		connection, err := net.DialTimeout("tcp", address, 2*time.Second)
		if err != nil {
			return "", err
		}
		_ = connection.Close()
		values = append(values, time.Since(started))
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return strconv.FormatInt(values[1].Milliseconds(), 10), nil
}

func runSCP(ctx context.Context, request agentproto.Request) error {
	route, err := agentroute.Decode(request.Secret["route"])
	if err != nil {
		return err
	}
	chain, err := connector.Dial(ctx, route)
	if err != nil {
		return err
	}
	defer chain.Close()
	direction := flytransfer.DirectionUpload
	if request.Action == "scp-download" {
		direction = flytransfer.DirectionDownload
	}
	flags := []string{"-p"}
	if request.Options["directory"] == "true" {
		flags = append(flags, "-r")
	}
	spec := &flytransfer.Spec{Mode: flytransfer.ModeSCP, Direction: direction, Flags: flags, Sources: []string{request.Options["source"]}, Target: request.Options["target"]}
	code, err := flytransfer.Run(chain.Final(), spec)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("scp exit code %d", code)
	}
	return nil
}

func (s *Service) startListener(request agentproto.Request) (*listenerJob, error) {
	jobID, token, path := request.Options["job"], request.Secret["token"], request.Options["path"]
	if jobID == "" || token == "" || path == "" {
		return nil, errors.New("listener requires job, path, and token")
	}
	certificate, pin, err := ephemeralCertificate()
	if err != nil {
		return nil, err
	}
	listener, err := tls.Listen("tcp", "0.0.0.0:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13})
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	job := &listenerJob{listener: listener, done: make(chan error, 1), port: port, pin: pin}
	s.mu.Lock()
	if _, exists := s.jobs[jobID]; exists {
		s.mu.Unlock()
		_ = listener.Close()
		return nil, errors.New("duplicate listener job")
	}
	s.jobs[jobID] = job
	s.mu.Unlock()
	go func() {
		stopListener := context.AfterFunc(s.ctx, func() { _ = listener.Close() })
		defer stopListener()
		connection, acceptErr := listener.Accept()
		_ = listener.Close()
		if acceptErr != nil {
			job.done <- acceptErr
			return
		}
		defer connection.Close()
		stopConnection := context.AfterFunc(s.ctx, func() { _ = connection.Close() })
		defer stopConnection()
		if err := authenticateDataChannel(connection, token); err != nil {
			job.done <- err
			return
		}
		if request.Action == "listen-receive" {
			job.done <- receiveArchiveWithOwnership(connection, path, request.Options["preserve_owner"] == "true")
		} else {
			job.done <- sendArchive(connection, path)
		}
	}()
	return job, nil
}

func (s *Service) wait(ctx context.Context, jobID string) error {
	s.mu.Lock()
	job := s.jobs[jobID]
	delete(s.jobs, jobID)
	s.mu.Unlock()
	if job == nil {
		return errors.New("unknown listener job")
	}
	select {
	case err := <-job.done:
		return err
	case <-ctx.Done():
		_ = job.listener.Close()
		return ctx.Err()
	case <-time.After(10 * time.Minute):
		_ = job.listener.Close()
		return errors.New("listener job timed out")
	}
}

func connect(ctx context.Context, request agentproto.Request, sending bool) error {
	address, token, pin, path := request.Options["address"], request.Secret["token"], request.Secret["pin"], request.Options["path"]
	if address == "" || token == "" || pin == "" || path == "" {
		return errors.New("connection requires address, path, token, and pin")
	}
	var raw net.Conn
	var err error
	var routeChain *connector.Chain
	if routePayload := request.Secret["route"]; routePayload != "" {
		route, decodeErr := agentroute.Decode(routePayload)
		if decodeErr != nil {
			return decodeErr
		}
		routeChain, err = connector.Dial(ctx, route)
		if err == nil {
			raw, err = routeChain.Final().Dial("tcp", address)
		}
	} else if proxyAddress := request.Secret["socks_address"]; proxyAddress != "" {
		raw, err = dialSOCKSContext(ctx, proxyAddress, address, request.Secret["socks_username"], request.Secret["socks_password"])
	} else {
		dialer := net.Dialer{Timeout: 8 * time.Second}
		raw, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		if routeChain != nil {
			_ = routeChain.Close()
		}
		return err
	}
	if routeChain != nil {
		defer routeChain.Close()
	}
	stopConnection := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stopConnection()
	connection := tls.Client(raw, &tls.Config{
		MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, // pinned below
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) != 1 {
				return errors.New("unexpected data-channel certificate chain")
			}
			digest := sha256.Sum256(state.PeerCertificates[0].Raw)
			if hex.EncodeToString(digest[:]) != pin {
				return errors.New("data-channel certificate pin mismatch")
			}
			return nil
		},
	})
	if err := connection.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return err
	}
	defer connection.Close()
	if _, err := io.WriteString(connection, token+"\n"); err != nil {
		return err
	}
	var acknowledged [1]byte
	if _, err := io.ReadFull(connection, acknowledged[:]); err != nil || acknowledged[0] != 1 {
		return errors.New("data-channel authentication failed")
	}
	if sending {
		return sendArchive(connection, path)
	}
	return receiveArchiveWithOwnership(connection, path, request.Options["preserve_owner"] == "true")
}

func dialSOCKSContext(ctx context.Context, proxyAddress, targetAddress, username, password string) (net.Conn, error) {
	type result struct {
		connection net.Conn
		err        error
	}
	done := make(chan result, 1)
	go func() {
		connection, err := socks.DialViaSocks5(proxyAddress, targetAddress, username, password)
		done <- result{connection: connection, err: err}
	}()
	select {
	case value := <-done:
		return value.connection, value.err
	case <-ctx.Done():
		go func() {
			value := <-done
			if value.connection != nil {
				_ = value.connection.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

func authenticateDataChannel(connection net.Conn, token string) error {
	_ = connection.SetDeadline(time.Now().Add(30 * time.Second))
	line := make([]byte, 0, len(token)+1)
	var one [1]byte
	for len(line) < 1024 {
		if _, err := io.ReadFull(connection, one[:]); err != nil {
			return errors.New("invalid data-channel token")
		}
		if one[0] == '\n' {
			break
		}
		line = append(line, one[0])
	}
	if len(line) >= 1024 || string(line) != token {
		return errors.New("invalid data-channel token")
	}
	if _, err := connection.Write([]byte{1}); err != nil {
		return err
	}
	return connection.SetDeadline(time.Time{})
}

func sendArchive(writer io.Writer, root string) error {
	gzipWriter := gzip.NewWriter(writer)
	tarWriter := tar.NewWriter(gzipWriter)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if header.Name == "." {
			header.Name = "."
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		return errors.Join(copyErr, file.Close())
	})
	return errors.Join(err, tarWriter.Close(), gzipWriter.Close())
}

func receiveArchive(reader io.Reader, root string) error {
	return receiveArchiveWithOwnership(reader, root, false)
}

func receiveArchiveWithOwnership(reader io.Reader, root string, preserveOwner bool) error {
	if _, err := os.Lstat(root); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var directories []struct {
		path     string
		mode     os.FileMode
		modified time.Time
		uid      int
		gid      int
	}
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("archive path escapes destination")
		}
		target := root
		if clean != "." {
			target = filepath.Join(root, clean)
		}
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return errors.New("archive path escapes destination")
		}
		mode := os.FileMode(header.Mode)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, mode.Perm()); err != nil {
				return err
			}
			directories = append(directories, struct {
				path     string
				mode     os.FileMode
				modified time.Time
				uid      int
				gid      int
			}{target, mode, header.ModTime, header.Uid, header.Gid})
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tarReader)
			syncErr := file.Sync()
			closeErr := file.Close()
			if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
				return err
			}
			if err := os.Chmod(target, mode.Perm()); err != nil {
				return err
			}
			if preserveOwner {
				if err := os.Chown(target, header.Uid, header.Gid); err != nil {
					return err
				}
			}
			_ = os.Chtimes(target, header.ModTime, header.ModTime)
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
			if preserveOwner {
				if err := os.Lchown(target, header.Uid, header.Gid); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unsupported archive type %d", header.Typeflag)
		}
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i].path) > len(directories[j].path) })
	for _, directory := range directories {
		_ = os.Chmod(directory.path, directory.mode.Perm())
		if preserveOwner {
			if err := os.Chown(directory.path, directory.uid, directory.gid); err != nil {
				return err
			}
		}
		_ = os.Chtimes(directory.path, directory.modified, directory.modified)
	}
	return nil
}

func ephemeralCertificate() (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "dragfm-ephemeral"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(15 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	digest := sha256.Sum256(der)
	return certificate, hex.EncodeToString(digest[:]), err
}

func localAddresses() []string {
	var result []string
	addresses, _ := net.InterfaceAddrs()
	for _, address := range addresses {
		var ip net.IP
		switch value := address.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		result = append(result, ip.String())
	}
	sort.Strings(result)
	return result
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
