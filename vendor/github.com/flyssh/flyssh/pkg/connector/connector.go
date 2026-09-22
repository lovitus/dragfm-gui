// Package connector exposes FlySSH's reusable SSH route dialer.
package connector

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/flyssh/flyssh/pkg/socks"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Route is an ordered SSH route. Hops[0] is reached from the controller;
// later hops are reached through direct-tcpip channels on the previous hop.
type Route struct {
	Hops    []Hop
	SOCKS   *SOCKS5
	Timeout time.Duration
	Trace   func(Event)
}

type SOCKS5 struct {
	Address  string
	Username string
	Password string
}

type Hop struct {
	Host        string
	Port        int
	User        string
	Credentials Credentials
	HostKey     HostKeyPolicy
	Ciphers     []string
	MACs        []string
}

type Credentials struct {
	UseAgent    bool
	Password    string
	PrivateKeys []PrivateKey
}

type PrivateKey struct {
	PEM        []byte
	Passphrase []byte
}

// HostKeyPolicy supports exact fingerprint pinning and OpenSSH known_hosts.
// Unknown hosts are rejected unless AcceptNew is true. Changed keys are never
// accepted automatically.
type HostKeyPolicy struct {
	PinnedSHA256   string
	KnownHostsFile string
	AcceptNew      bool
	ConfirmNew     func(host, fingerprint string) bool
}

type Event struct {
	Hop   int
	Stage string
	Host  string
	Via   string
}

type Chain struct {
	Clients []*ssh.Client
}

func (c *Chain) Final() *ssh.Client {
	if c == nil || len(c.Clients) == 0 {
		return nil
	}
	return c.Clients[len(c.Clients)-1]
}

func (c *Chain) Close() error {
	if c == nil {
		return nil
	}
	var joined error
	for i := len(c.Clients) - 1; i >= 0; i-- {
		joined = errors.Join(joined, c.Clients[i].Close())
	}
	return joined
}

func Dial(ctx context.Context, route Route) (*Chain, error) {
	if len(route.Hops) == 0 {
		return nil, errors.New("connector: route has no hops")
	}
	timeout := route.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	chain := &Chain{}
	fail := func(err error) (*Chain, error) {
		_ = chain.Close()
		return nil, err
	}

	for index, hop := range route.Hops {
		if hop.Port == 0 {
			hop.Port = 22
		}
		if hop.Host == "" || hop.User == "" {
			return fail(fmt.Errorf("connector: hop %d has empty host or user", index+1))
		}
		address := net.JoinHostPort(hop.Host, fmt.Sprintf("%d", hop.Port))
		if route.Trace != nil {
			via := "direct"
			if index > 0 {
				via = "ssh-hop"
			} else if route.SOCKS != nil {
				via = "socks5"
			}
			route.Trace(Event{Hop: index + 1, Stage: "dial", Host: address, Via: via})
		}

		var conn net.Conn
		var err error
		if index == 0 {
			if route.SOCKS != nil {
				conn, err = dialSOCKSContext(ctx, *route.SOCKS, address, timeout)
			} else {
				dialer := net.Dialer{Timeout: timeout}
				conn, err = dialer.DialContext(ctx, "tcp", address)
			}
		} else {
			conn, err = dialThroughClient(ctx, chain.Final(), address, timeout)
		}
		if err != nil {
			return fail(fmt.Errorf("connector: dial hop %d %s: %w", index+1, address, err))
		}

		config, closeAuthentication, err := clientConfig(hop, timeout)
		if err != nil {
			_ = conn.Close()
			return fail(fmt.Errorf("connector: configure hop %d: %w", index+1, err))
		}
		sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
		closeAuthentication()
		if err != nil {
			_ = conn.Close()
			return fail(fmt.Errorf("connector: handshake hop %d %s: %w", index+1, address, err))
		}
		chain.Clients = append(chain.Clients, ssh.NewClient(sshConn, chans, reqs))
		if route.Trace != nil {
			route.Trace(Event{Hop: index + 1, Stage: "connected", Host: address})
		}
	}
	return chain, nil
}

func dialSOCKSContext(ctx context.Context, proxy SOCKS5, target string, timeout time.Duration) (net.Conn, error) {
	ch := make(chan dialResult, 1)
	go func() {
		conn, err := socks.DialViaSocks5(proxy.Address, target, proxy.Username, proxy.Password)
		ch <- dialResult{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		go closeLateDial(ch)
		return nil, ctx.Err()
	case out := <-ch:
		if out.conn != nil {
			_ = out.conn.SetDeadline(time.Time{})
		}
		return out.conn, out.err
	case <-time.After(timeout):
		go closeLateDial(ch)
		return nil, context.DeadlineExceeded
	}
}

func dialThroughClient(ctx context.Context, client *ssh.Client, address string, timeout time.Duration) (net.Conn, error) {
	ch := make(chan dialResult, 1)
	go func() {
		conn, err := client.Dial("tcp", address)
		ch <- dialResult{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		go closeLateDial(ch)
		return nil, ctx.Err()
	case out := <-ch:
		return out.conn, out.err
	case <-time.After(timeout):
		go closeLateDial(ch)
		return nil, context.DeadlineExceeded
	}
}

type dialResult struct {
	conn net.Conn
	err  error
}

func closeLateDial(results <-chan dialResult) {
	result := <-results
	if result.conn != nil {
		_ = result.conn.Close()
	}
}

func clientConfig(hop Hop, timeout time.Duration) (*ssh.ClientConfig, func(), error) {
	authMethods, closeAuthentication, err := authMethods(hop.Credentials)
	if err != nil {
		return nil, func() {}, err
	}
	callback, err := hostKeyCallback(hop.HostKey)
	if err != nil {
		closeAuthentication()
		return nil, func() {}, err
	}
	return &ssh.ClientConfig{
		Config: ssh.Config{Ciphers: hop.Ciphers, MACs: hop.MACs},
		User:   hop.User, Auth: authMethods, HostKeyCallback: callback, Timeout: timeout,
	}, closeAuthentication, nil
}

func authMethods(credentials Credentials) ([]ssh.AuthMethod, func(), error) {
	methods := make([]ssh.AuthMethod, 0, 4)
	var authenticationConnections []io.Closer
	closeAuthentication := func() {
		for _, connection := range authenticationConnections {
			_ = connection.Close()
		}
	}
	var signers []ssh.Signer
	for index, key := range credentials.PrivateKeys {
		var signer ssh.Signer
		var err error
		if len(key.Passphrase) > 0 {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key.PEM, key.Passphrase)
		} else {
			signer, err = ssh.ParsePrivateKey(key.PEM)
		}
		if err != nil {
			closeAuthentication()
			return nil, func() {}, fmt.Errorf("private key %d: %w", index+1, err)
		}
		signers = append(signers, signer)
	}
	if credentials.UseAgent {
		if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
			if conn, err := net.Dial("unix", socket); err == nil {
				agentSigners, signerErr := agent.NewClient(conn).Signers()
				if signerErr == nil && len(agentSigners) > 0 {
					// One publickey method must contain every candidate. Go's SSH
					// client does not retry a second AuthMethod with the same name,
					// so a reachable-but-empty agent used to suppress vault keys.
					signers = append(signers, agentSigners...)
					authenticationConnections = append(authenticationConnections, conn)
				} else {
					_ = conn.Close()
				}
			}
		}
	}
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	if credentials.Password != "" {
		password := credentials.Password
		methods = append(methods, ssh.Password(password))
		methods = append(methods, ssh.KeyboardInteractive(func(_ string, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = password
			}
			return answers, nil
		}))
	}
	if len(methods) == 0 {
		closeAuthentication()
		return nil, func() {}, errors.New("no SSH authentication methods")
	}
	return methods, closeAuthentication, nil
}

func hostKeyCallback(policy HostKeyPolicy) (ssh.HostKeyCallback, error) {
	if policy.PinnedSHA256 != "" {
		want := normalizeFingerprint(policy.PinnedSHA256)
		return func(_ string, _ net.Addr, key ssh.PublicKey) error {
			got := normalizeFingerprint(ssh.FingerprintSHA256(key))
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				return fmt.Errorf("host key fingerprint mismatch: got %s", got)
			}
			return nil
		}, nil
	}
	if policy.KnownHostsFile == "" {
		if policy.ConfirmNew == nil {
			return nil, errors.New("host key policy requires a pin, known_hosts file, or confirmation callback")
		}
		return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint := ssh.FingerprintSHA256(key)
			if !policy.ConfirmNew(hostname, fingerprint) {
				return fmt.Errorf("unknown host key %s", fingerprint)
			}
			return nil
		}, nil
	}
	callback, err := knownhosts.New(policy.KnownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("known_hosts: %w", err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
			return err
		}
		fingerprint := ssh.FingerprintSHA256(key)
		if !policy.AcceptNew && (policy.ConfirmNew == nil || !policy.ConfirmNew(hostname, fingerprint)) {
			return fmt.Errorf("unknown host key %s", fingerprint)
		}
		line := knownhosts.Line([]string{hostname}, key) + "\n"
		file, openErr := os.OpenFile(policy.KnownHostsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if openErr != nil {
			return openErr
		}
		defer file.Close()
		_, openErr = file.WriteString(line)
		return openErr
	}, nil
}

func normalizeFingerprint(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "SHA256:"))
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(decoded) == sha256.Size {
		return base64.RawStdEncoding.EncodeToString(decoded)
	}
	return value
}
