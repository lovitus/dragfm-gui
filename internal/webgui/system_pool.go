package webgui

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/agentroute"
	"github.com/lovitus/dragfm-gui/internal/boundedbuf"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// This optional adapter invokes only already-installed Python libraries. The
// program is static (no secrets in argv); versioned task data goes over SSH
// stdin/fd6 and never to a remote file or environment variable. Protocols are
// implemented by PySocks/Paramiko, not another SSH/SOCKS implementation.
//
//go:embed system_pool_stream.py
var systemPoolStream string

type systemPoolRequest struct {
	Version int                  `json:"version"`
	Mode    string               `json:"mode"`
	Host    string               `json:"host"`
	Port    int                  `json:"port"`
	Route   agentroute.Route     `json:"route"`
	Native  *systemNativeRequest `json:"native,omitempty"`
}

type systemTCPProber struct {
	ctx    context.Context
	remote *endpoint.Remote
}

func (p *systemTCPProber) ProbeTCP(address string) (time.Duration, error) {
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("invalid TCP probe port")
	}
	data, err := json.Marshal(systemPoolRequest{Version: 1, Mode: "probe", Host: host, Port: port})
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(p.ctx, 6*time.Second)
	defer cancel()
	var output bytes.Buffer
	var diagnostic boundedbuf.Buffer
	err = p.remote.Exec(ctx, "exec 6<&0; exec python3 -I -B -c "+shellQuote(systemPoolStream), endpoint.ExecOptions{Stdin: bytes.NewReader(data), Stdout: &output, Stderr: &diagnostic})
	if err != nil {
		return 0, fmt.Errorf("system TCP probe: %w: %s", err, diagnostic.String())
	}
	nanos, err := strconv.ParseInt(strings.TrimSpace(output.String()), 10, 64)
	if err != nil || nanos < 0 || nanos > int64(5*time.Second) {
		return 0, errors.New("invalid remote TCP timing response")
	}
	return time.Duration(nanos), nil
}

func (a *App) openPoolProber(ctx context.Context, remote *endpoint.Remote, architecture string) (tcpProber, func() error, error) {
	agent, cleanup, err := a.startTransferAgent(ctx, remote, architecture, false, "")
	if err == nil {
		return agent, cleanup, nil
	}
	if ctx.Err() != nil || !transfer.Retryable(err) {
		return nil, nil, err
	}
	owned, openErr := remote.Fork(ctx)
	if openErr != nil {
		return nil, nil, errors.Join(err, openErr)
	}
	check, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if checkErr := owned.Exec(check, "python3 -I -B -c 'import socket, time'", endpoint.ExecOptions{}); checkErr != nil {
		_ = owned.Close()
		return nil, nil, errors.Join(err, fmt.Errorf("system TCP probe requires installed Python 3: %w", checkErr))
	}
	return &systemTCPProber{ctx: ctx, remote: owned}, func() error { _ = owned.Close(); return nil }, nil
}

func (a *App) runPooledMethod(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, direction strategy.Direction, elevated bool, password string, proxy *connector.SOCKS5, prefix []connector.Hop, method strategy.Method) error {
	if method != strategy.EncryptedStream && method != strategy.NcatTar {
		return a.runNativeMethod(ctx, operation, preflight, direction, elevated, password, proxy, prefix, method)
	}
	err := a.runAgentStreamWithCarrier(ctx, operation, preflight, direction, elevated, password, proxy, prefix, "", carrierForMethod(method))
	if err == nil || method != strategy.NcatTar || ctx.Err() != nil || !transfer.Retryable(err) {
		return err
	}
	route := connector.Route{SOCKS: proxy}
	if len(prefix) != 0 {
		source, target, ok := remoteRemotePair(operation)
		if !ok {
			return err
		}
		other := target
		if direction == strategy.TargetPull {
			other = source
		}
		host, found := a.settingsFor(ctx).document.HostByName(other.Name())
		if !found {
			return errors.Join(err, errors.New("system relay destination route is missing"))
		}
		peer, routeErr := a.peerTransferRoute(ctx, host, nil, prefix)
		if routeErr != nil {
			return errors.Join(err, routeErr)
		}
		// Only the selected pool precedes the final endpoint. The saved
		// controller login chain is not a remote-to-remote route.
		route.Hops = peer.Hops
	}
	payload, encodeErr := agentroute.Encode(route)
	if encodeErr != nil {
		return errors.Join(err, encodeErr)
	}
	var wire agentroute.Route
	if decodeErr := json.Unmarshal([]byte(payload), &wire); decodeErr != nil {
		return errors.Join(err, decodeErr)
	}
	if fallbackErr := a.runSystemNcatTarRoute(ctx, operation, direction, elevated, &wire); fallbackErr != nil {
		return errors.Join(err, fallbackErr)
	}
	return nil
}

func checkSystemPoolRuntime(ctx context.Context, remote endpoint.Endpoint, route agentroute.Route) error {
	imports := "import socket, resource, time"
	if route.SOCKS != nil {
		imports += "; import socks"
	}
	if len(route.Hops) > 0 {
		imports += "; import paramiko"
	}
	probe, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var diagnostic bytes.Buffer
	if err := remote.Exec(probe, "python3 -I -B -c "+shellQuote(imports), endpoint.ExecOptions{Stderr: &diagnostic}); err != nil {
		return fmt.Errorf("system pool requires preinstalled Python/PySocks/Paramiko for the selected route (nothing was installed): %w: %s", err, diagnostic.String())
	}
	return nil
}

func systemProxyOrigins(ctx context.Context, remote endpoint.Endpoint, address string) ([]string, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	// Resolve where the actual initiating machine connects, not on the GUI.
	// Proxies with unrelated NAT egress will fail this narrow allow list and
	// continue fallback; never disclose a source archive to an open listener.
	script := "import socket, sys; print('\\n'.join(sorted({v[4][0] for v in socket.getaddrinfo(sys.argv[1], None, socket.AF_INET, socket.SOCK_STREAM)})))"
	probe, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if err := remote.Exec(probe, "python3 -I -B -c "+shellQuote(script)+" "+shellQuote(host), endpoint.ExecOptions{Stdout: &output}); err != nil {
		return nil, fmt.Errorf("resolve scoped proxy egress allow list: %w", err)
	}
	var addresses []string
	for _, field := range strings.Fields(output.String()) {
		if ip := net.ParseIP(field); ip != nil && ip.To4() != nil {
			addresses = append(addresses, ip.String())
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("proxy has no confirmed IPv4 address for scoped ncat listener")
	}
	return addresses, nil
}
