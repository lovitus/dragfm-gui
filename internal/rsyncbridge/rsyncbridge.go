package rsyncbridge

import (
	"bufio"
	"bytes"
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

func Run(ctx context.Context, client *ssh.Client, direction Direction, source, target string) error {
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
	rsh := quoteShellWord(executable) + " " + ChildFlag + " " + listener.Addr().String() + " " + token
	args := []string{"-a", "-e", rsh}
	switch direction {
	case Upload:
		args = append(args, source, "dragfm:"+target)
	case Download:
		args = append(args, "dragfm:"+source, target)
	default:
		return fmt.Errorf("unknown rsync direction %q", direction)
	}
	command := exec.CommandContext(ctx, rsync, args...)
	configureChildLifecycle(command)
	var localError bytes.Buffer
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = &localError

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
	go func() { commandDone <- command.Wait() }()
	killAndWait := func() error {
		_ = listener.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		return <-commandDone
	}
	var acceptedResult accepted
	select {
	case acceptedResult = <-acceptedChannel:
	case localErr := <-commandDone:
		return fmt.Errorf("rsync exited before opening its transport: %w: %s", localErr, strings.TrimSpace(localError.String()))
	case <-ctx.Done():
		_ = killAndWait()
		return ctx.Err()
	case <-time.After(transportHandshakeTimeout):
		localErr := killAndWait()
		return fmt.Errorf("rsync did not open its transport within %s: %w: %s", transportHandshakeTimeout, localErr, strings.TrimSpace(localError.String()))
	}
	if acceptedResult.err != nil {
		_ = killAndWait()
		return acceptedResult.err
	}
	remoteErr := bridgeSSH(ctx, client, acceptedResult.connection, acceptedResult.request.Args)
	if remoteErr != nil && command.Process != nil {
		_ = command.Process.Kill()
	}
	var localErr error
	select {
	case localErr = <-commandDone:
	case <-ctx.Done():
		localErr = killAndWait()
		remoteErr = errors.Join(remoteErr, ctx.Err())
	}
	if remoteErr != nil || localErr != nil {
		return fmt.Errorf("rsync failed: %w: %s", errors.Join(remoteErr, localErr), strings.TrimSpace(localError.String()))
	}
	return nil
}

func bridgeSSH(ctx context.Context, client *ssh.Client, connection net.Conn, arguments []string) error {
	defer connection.Close()
	commandArgs, err := rsyncServerArguments(arguments)
	if err != nil {
		return err
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
	var stderr bytes.Buffer
	session.Stderr = &stderr
	if err := session.Start(joinShellWords(commandArgs)); err != nil {
		return err
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
	if tcp, ok := connection.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	waitErr := session.Wait()
	cancel()
	inputErr := <-inputDone
	if waitErr == nil && outputErr == nil && (errors.Is(inputErr, net.ErrClosed) || errors.Is(inputErr, io.ErrClosedPipe)) {
		inputErr = nil // Our shutdown unblocked an otherwise successful upload.
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
			if len(result) < 2 || !strings.HasPrefix(result[1], "--server") {
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
