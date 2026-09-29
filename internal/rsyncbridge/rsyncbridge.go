package rsyncbridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	flytransfer "github.com/flyssh/flyssh/pkg/transfer"
	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/agentlease"
	"github.com/lovitus/dragfm-gui/internal/boundedbuf"
	"golang.org/x/crypto/ssh"
)

const ChildFlag = "--dragfm-rsync-transport"

const transportHandshakeTimeout = 15 * time.Second

type Direction string

const (
	Upload   Direction = "upload"
	Download Direction = "download"
)

type request struct {
	Token string   `json:"token"`
	Args  []string `json:"args"`
}

// ChildMain handles the private rsh subprocess launched by rsync. The only
// command-line values are a loopback address and a random one-use token; SSH
// credentials and routes remain in the already-unlocked parent process.
func ChildMain(args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != ChildFlag {
		return false, 0
	}
	if len(args) < 3 {
		fmt.Fprintln(stderr, "dragfm rsync transport: missing loopback address or token")
		return true, 2
	}
	connection, err := net.DialTimeout("tcp", args[1], transportHandshakeTimeout)
	if err != nil {
		fmt.Fprintln(stderr, "dragfm rsync transport:", err)
		return true, 1
	}
	_ = connection.SetDeadline(time.Now().Add(transportHandshakeTimeout))
	tcp, _ := connection.(*net.TCPConn)
	if err := json.NewEncoder(connection).Encode(request{Token: args[2], Args: append([]string(nil), args[3:]...)}); err != nil {
		_ = connection.Close()
		fmt.Fprintln(stderr, "dragfm rsync transport:", err)
		return true, 1
	}
	var acknowledged [1]byte
	if _, err := io.ReadFull(connection, acknowledged[:]); err != nil || acknowledged[0] != 1 {
		_ = connection.Close()
		fmt.Fprintln(stderr, "dragfm rsync transport: parent handshake failed")
		return true, 1
	}
	_ = connection.SetDeadline(time.Time{})
	writeDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(connection, stdin)
		if tcp != nil {
			_ = tcp.CloseWrite()
		}
		writeDone <- copyErr
	}()
	_, readErr := io.Copy(stdout, connection)
	_ = connection.Close()
	// rsync waits for its rsh subprocess to exit before closing the input pipe.
	// Waiting for that pipe here creates a cycle after the remote EOF. This
	// transport subprocess owns stdin; close it when possible and never wait
	// on an arbitrary Reader once the authenticated parent has finished.
	if closer, ok := stdin.(io.Closer); ok {
		_ = closer.Close()
	}
	var writeErr error
	select {
	case writeErr = <-writeDone:
		if errors.Is(writeErr, net.ErrClosed) || errors.Is(writeErr, os.ErrClosed) || errors.Is(writeErr, io.ErrClosedPipe) {
			writeErr = nil
		}
	default:
	}
	if err := errors.Join(readErr, writeErr); err != nil {
		fmt.Fprintln(stderr, "dragfm rsync transport:", err)
		return true, 1
	}
	return true, 0
}

func Run(ctx context.Context, client *ssh.Client, direction Direction, source, target string, peerHelper ...string) error {
	if client == nil {
		return errors.New("SSH client is required")
	}
	rsync, err := exec.LookPath("rsync")
	if err != nil {
		return errors.New("控制机未安装 rsync")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	token := randomToken()
	// -e uses rsync's own parser, which doubles quotes rather than using
	// POSIX backslash escapes. The SSH command below is a different layer.
	rsh := "'" + strings.ReplaceAll(executable, "'", "''") + "' " + ChildFlag + " " + listener.Addr().String() + " " + token
	args := []string{"-a", "-e", rsh, "--"}
	var remotePath string
	switch direction {
	case Upload:
		remotePath = target
		args = append(args, source, "dragfm:"+target)
	case Download:
		remotePath = source
		args = append(args, "dragfm:"+source, target)
	default:
		return fmt.Errorf("unknown rsync direction %q", direction)
	}
	command := exec.CommandContext(ctx, rsync, args...)
	// The app owns the protocol/argv policy. Inherited RSYNC_PROTECT_ARGS or
	// RSYNC_OLD_ARGS must not silently change it. No user credential is added.
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "RSYNC_") {
			command.Env = append(command.Env, variable)
		}
	}
	configureChildLifecycle(command)
	command.WaitDelay = 2 * time.Second
	var localError boundedbuf.Buffer
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = &localError
	if err := agentlease.Attach(ctx, command); err != nil {
		return err
	}

	type accepted struct {
		connection net.Conn
		request    request
		err        error
	}
	acceptedChannel := make(chan accepted, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			acceptedChannel <- accepted{err: acceptErr}
			return
		}
		_ = connection.SetDeadline(time.Now().Add(transportHandshakeTimeout))
		line, readErr := bufio.NewReader(io.LimitReader(connection, 65537)).ReadBytes('\n')
		if len(line) > 65536 {
			readErr = errors.New("rsync transport handshake is too large")
		}
		var payload request
		if readErr == nil {
			readErr = json.Unmarshal(line, &payload)
		}
		if readErr != nil || payload.Token != token {
			_ = connection.Close()
			if readErr == nil {
				readErr = errors.New("invalid one-use token")
			}
			acceptedChannel <- accepted{err: readErr}
			return
		}
		if _, writeErr := connection.Write([]byte{1}); writeErr != nil {
			_ = connection.Close()
			acceptedChannel <- accepted{err: writeErr}
			return
		}
		_ = connection.SetDeadline(time.Time{})
		acceptedChannel <- accepted{connection: connection, request: payload}
	}()
	if err := command.Start(); err != nil {
		return err
	}
	commandDone := make(chan error, 1)
	go func() {
		err := command.Wait()
		var exited *exec.ExitError
		if err != nil && (!errors.As(err, &exited) || !exited.ProcessState.Exited()) {
			// A signal/WaitDelay can reap the rsync parent without proving
			// that its receiver workers are gone. Leases still protect them.
			err = &flytransfer.ExitUnconfirmedError{Cause: err}
		}
		commandDone <- err
	}()
	killAndWait := func() error {
		_ = listener.Close()
		if command.Process != nil {
			_ = killChild(command)
		}
		select {
		case err := <-commandDone:
			return err
		case <-time.After(3 * time.Second):
			return &flytransfer.ExitUnconfirmedError{Cause: errors.New("local rsync did not exit; installation lease retained by child")}
		}
	}
	var acceptedResult accepted
	select {
	case acceptedResult = <-acceptedChannel:
	case localErr := <-commandDone:
		return fmt.Errorf("rsync exited before opening its transport: %w: %s", localErr, strings.TrimSpace(localError.String()))
	case <-ctx.Done():
		return errors.Join(ctx.Err(), killAndWait())
	case <-time.After(transportHandshakeTimeout):
		localErr := killAndWait()
		return fmt.Errorf("rsync did not open its transport within %s: %w: %s", transportHandshakeTimeout, localErr, strings.TrimSpace(localError.String()))
	}
	if acceptedResult.err != nil {
		return errors.Join(acceptedResult.err, killAndWait())
	}
	remoteErr := bridgeSSH(ctx, client, acceptedResult.connection, acceptedResult.request.Args, remotePath, peerHelper...)
	var exitDeadline <-chan time.Time
	if remoteErr != nil {
		// bridgeSSH has closed the transport. A normal remote rejection (for
		// example no installed rsync) lets the local rsync exit on EOF. Killing
		// it immediately manufactures an unconfirmed signal exit and wrongly
		// prevents SCP/stream fallback. A stuck child still takes the existing
		// kill-and-wait path, whose uncertain-worker protection is unchanged.
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		exitDeadline = timer.C
	}
	var localErr error
	select {
	case localErr = <-commandDone:
	case <-ctx.Done():
		localErr = killAndWait()
		remoteErr = errors.Join(remoteErr, ctx.Err())
	case <-exitDeadline:
		localErr = killAndWait()
	}
	if remoteErr != nil || localErr != nil {
		return fmt.Errorf("rsync failed: %w: %s", errors.Join(remoteErr, localErr), strings.TrimSpace(localError.String()))
	}
	return nil
}

func bridgeSSH(ctx context.Context, client *ssh.Client, connection net.Conn, arguments []string, remotePath string, peerHelper ...string) error {
	connection = activity.Conn(ctx, connection)
	defer connection.Close()
	commandArgs, err := rsyncServerArguments(arguments)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(remotePath, "/") || strings.ContainsRune(remotePath, 0) {
		return errors.New("rsync remote path must be absolute")
	}
	// rsync's rsh argv may already contain shell escapes. We have the exact
	// single operand from the operation, so do not decode/requote those bytes
	// or enable -s (which still expands remote glob patterns). Replace only
	// that operand, then quote every SSH argv word exactly once. Standard
	// rsync still owns server options, the protocol and receiver validation.
	commandArgs[len(commandArgs)-1] = remotePath
	if len(peerHelper) > 0 && peerHelper[0] != "" {
		commandArgs = append([]string{peerHelper[0], "--transfer-server"}, commandArgs...)
	}
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr boundedbuf.Buffer
	session.Stderr = &stderr
	if err := session.Start(joinShellWords(commandArgs)); err != nil {
		return &flytransfer.ExitUnconfirmedError{Cause: err}
	}
	var once sync.Once
	cancel := func() { once.Do(func() { _ = session.Close(); _ = connection.Close() }) }
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	inputDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(stdin, connection)
		_ = stdin.Close()
		inputDone <- copyErr
	}()
	_, outputErr := io.Copy(connection, stdout)
	if tcp, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = tcp.CloseWrite()
	}
	waitErr := session.Wait()
	var exited *ssh.ExitError
	confirmedExit := waitErr == nil
	if waitErr != nil && (!errors.As(waitErr, &exited) || exited.Signal() != "") {
		waitErr = &flytransfer.ExitUnconfirmedError{Cause: waitErr}
	} else {
		confirmedExit = true
	}
	cancel()
	inputErr := <-inputDone
	if confirmedExit && outputErr == nil && (errors.Is(inputErr, net.ErrClosed) || errors.Is(inputErr, io.ErrClosedPipe)) {
		inputErr = nil // Our shutdown; keep any actual remote rejection below.
	}
	if waitErr != nil && stderr.Len() > 0 {
		waitErr = fmt.Errorf("remote rsync: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return errors.Join(ctx.Err(), inputErr, outputErr, waitErr)
}

func rsyncServerArguments(arguments []string) ([]string, error) {
	for index, argument := range arguments {
		if argument == "rsync" {
			result := append([]string(nil), arguments[index:]...)
			if len(result) < 5 || result[1] != "--server" || result[len(result)-2] != "." {
				return nil, errors.New("refusing non-server rsync command")
			}
			return result, nil
		}
	}
	return nil, errors.New("rsync transport did not receive a server command")
}

func joinShellWords(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = quoteShellWord(value)
	}
	return strings.Join(quoted, " ")
}

func quoteShellWord(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func randomToken() string {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}
