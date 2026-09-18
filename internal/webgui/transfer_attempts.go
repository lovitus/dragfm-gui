package webgui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	flytransfer "github.com/flyssh/flyssh/pkg/transfer"
	"github.com/lovitus/dragfm-gui/internal/agentroute"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
	"github.com/lovitus/dragfm-gui/internal/routespec"
	"github.com/lovitus/dragfm-gui/internal/rsyncbridge"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func (a *App) transferAttempts(operation transfer.Operation, preflight transfer.PreflightReport) ([]strategy.Attempt, strategy.Approval) {
	return a.transferAttemptsWithSudoProbe(operation, preflight, probeDirectoryWritable)
}

func (a *App) transferAttemptsWithSudoProbe(operation transfer.Operation, preflight transfer.PreflightReport, probe func(string) error) ([]strategy.Attempt, strategy.Approval) {
	var attempts []strategy.Attempt
	remoteSudo := make(map[strategy.Direction]string)
	requiresSudo, protectedDirectory := localDestinationRequiresSudoWithProbe(context.Background(), operation, probe)
	if !requiresSudo {
		if preflight.SameMachine {
			attempts = append(attempts, strategy.Attempt{Tier: strategy.SameHost, Direction: strategy.SourcePush, Method: strategy.MemoryStream, Run: func(ctx context.Context) error {
				_, err := transfer.Run(ctx, operation)
				return err
			}})
		}
		if remote, direction, ok := localRemotePair(operation); ok {
			attempts = append(attempts, strategy.Attempt{Tier: strategy.Direct, Direction: direction, Method: strategy.Rsync, Run: func(ctx context.Context) error {
				return runRsync(ctx, remote, operation)
			}})
			attempts = append(attempts, strategy.Attempt{Tier: strategy.Direct, Direction: direction, Method: strategy.SCP, Run: func(ctx context.Context) error {
				return runFlySSHSCP(ctx, remote, operation)
			}})
		}
		if _, _, ok := remoteRemotePair(operation); ok {
			for _, descriptor := range directRemotePlan(preflight, privateConnectionHosts(operation)) {
				descriptor := descriptor
				switch descriptor.Method {
				case strategy.Rsync, strategy.SCP:
					descriptor.Run = func(ctx context.Context) error {
						return a.runAgentMethod(ctx, operation, preflight, descriptor.Direction, descriptor.Elevated, remoteSudo[descriptor.Direction], nil, nil, descriptor.Method)
					}
				case strategy.EncryptedStream:
					descriptor.Run = func(ctx context.Context) error {
						return a.runAgentStream(ctx, operation, preflight, descriptor.Direction, descriptor.Elevated, remoteSudo[descriptor.Direction], nil, nil, "")
					}
				case strategy.NcatTar:
					descriptor.Run = func(ctx context.Context) error {
						return runNcatTar(ctx, operation, descriptor.Direction)
					}
				}
				attempts = append(attempts, descriptor)
			}
			attempts = append(attempts, strategy.Attempt{Tier: strategy.SOCKSPool, Direction: strategy.SourcePush, Method: strategy.SCP, Run: func(ctx context.Context) error {
				return a.runSOCKSPool(ctx, operation, preflight, remoteSudo)
			}})
			attempts = append(attempts, strategy.Attempt{Tier: strategy.JumpPool, Direction: strategy.SourcePush, Method: strategy.SCP, Run: func(ctx context.Context) error {
				return a.runJumpPool(ctx, operation, preflight, remoteSudo)
			}})
			attempts = append(attempts, strategy.Attempt{Tier: strategy.Hans, Direction: strategy.SourcePush, Method: strategy.SCP, Risk: strategy.HansRisk, Run: func(ctx context.Context) error {
				return a.runHans(ctx, operation, preflight, remoteSudo)
			}})
		}
		attempts = append(attempts, strategy.Attempt{Tier: strategy.ControllerRelay, Direction: strategy.SourcePush, Method: strategy.MemoryStream, Run: func(ctx context.Context) error {
			_, err := transfer.Run(ctx, operation)
			return err
		}})
	}
	if !requiresSudo {
		return attempts, a.transferApproval("", nil, operation, remoteSudo)
	}
	var elevated *endpoint.SudoLocal
	attempts = append(attempts, strategy.Attempt{Tier: strategy.ControllerRelay, Direction: strategy.TargetPull, Elevated: true, Method: strategy.MemoryStream, Risk: strategy.SudoRisk, Run: func(ctx context.Context) error {
		if elevated == nil {
			return errors.New("sudo 尚未获准")
		}
		elevatedOperation := operation
		elevatedOperation.Destination = elevated
		_, err := transfer.Run(ctx, elevatedOperation)
		return err
	}})
	return attempts, a.transferApproval(protectedDirectory, func(candidate *endpoint.SudoLocal) { elevated = candidate }, operation, remoteSudo)
}

func directRemotePlan(preflight transfer.PreflightReport, privateHosts bool) []strategy.Attempt {
	var attempts []strategy.Attempt
	for _, elevated := range []bool{false, true} {
		for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
			risk := strategy.Risk("")
			if elevated {
				risk = strategy.SourceSudoRisk
				if direction == strategy.TargetPull {
					risk = strategy.TargetSudoRisk
				}
			}
			for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream} {
				methodRisk := risk
				if method == strategy.EncryptedStream && !elevated {
					methodRisk = strategy.ListenRisk
				}
				attempts = append(attempts, strategy.Attempt{Tier: strategy.Direct, Direction: direction, Elevated: elevated, Method: method, Risk: methodRisk})
			}
			if !elevated && preflight.SourceCapabilities.Tools["ncat"] && preflight.TargetCapabilities.Tools["ncat"] {
				ncatRisk := strategy.PlaintextRisk
				if privateHosts {
					ncatRisk = strategy.ListenRisk
				}
				attempts = append(attempts, strategy.Attempt{Tier: strategy.Direct, Direction: direction, Method: strategy.NcatTar, Risk: ncatRisk})
			}
		}
	}
	return attempts
}

func (a *App) runAgentMethod(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, direction strategy.Direction, elevated bool, sudoPassword string, socks *connector.SOCKS5, prefix []connector.Hop, method strategy.Method) error {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("远端发起 SCP 仅用于两个 Linux SSH 端点")
	}
	elevatedTarget := elevated && direction == strategy.TargetPull
	initiator, other, architecture := source, target, preflight.SourceCapabilities.Architecture
	action, remoteSource, remoteTarget := string(method)+"-upload", operation.SourcePath, operation.TargetPath+".dragfm-partial-"+randomTransferToken(8)
	if direction == strategy.TargetPull {
		initiator, other, architecture = target, source, preflight.TargetCapabilities.Architecture
		action = string(method) + "-download"
	}
	a.mu.RLock()
	host, exists := a.document.HostByName(other.Name())
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("未找到端点 %q 的路由", other.Name())
	}
	route, err := a.routeForHost(host)
	if err != nil {
		return err
	}
	route.SOCKS = socks
	if len(prefix) > 0 {
		route.Hops = append(append([]connector.Hop(nil), prefix...), route.Hops...)
	}
	routePayload, err := agentroute.Encode(route)
	if err != nil {
		return err
	}
	agent, cleanupAgent, err := a.startTransferAgent(ctx, initiator, architecture, elevated, sudoPassword)
	if err != nil {
		return err
	}
	defer cleanupAgent()
	before, err := snapshotForAgentAttempt(ctx, operation, elevated && direction == strategy.SourcePush, agent)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if method == strategy.SCP && item.Mode&fs.ModeSymlink != 0 {
			return errors.New("SCP 不保证符号链接语义，改用加密流")
		}
	}
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); !elevatedTarget && (err == nil || !errors.Is(err, fs.ErrNotExist)) {
		return errors.New("目标已存在，远端 SCP 原子根路径跳过并改用流式合并")
	}
	if method == strategy.Rsync {
		err = agent.RsyncContext(ctx, action, remoteSource, remoteTarget, routePayload)
	} else {
		err = agent.SCPContext(ctx, action, remoteSource, remoteTarget, routePayload, before.Items[0].Mode.IsDir())
	}
	if err != nil {
		if elevatedTarget {
			_ = agent.RemovePath(remoteTarget, before.Items[0].Mode.IsDir())
		} else {
			_ = operation.Destination.Remove(context.Background(), remoteTarget, before.Items[0].Mode.IsDir())
		}
		return err
	}
	if elevatedTarget {
		err = agent.Commit(remoteTarget, operation.TargetPath, operation.Overwrite)
	} else {
		err = operation.Destination.Rename(ctx, remoteTarget, operation.TargetPath, false)
	}
	if err != nil {
		if elevatedTarget {
			_ = agent.RemovePath(remoteTarget, before.Items[0].Mode.IsDir())
		} else {
			_ = operation.Destination.Remove(context.Background(), remoteTarget, before.Items[0].Mode.IsDir())
		}
		return err
	}
	if elevatedTarget {
		return finishAcceleratedMoveWithAgentTarget(ctx, operation, before, "远端 "+string(method), agent)
	}
	if elevated && direction == strategy.SourcePush {
		return finishAcceleratedMoveWithAgentSource(ctx, operation, before, "远端 "+string(method), agent)
	}
	return finishAcceleratedMove(ctx, operation, before, "远端 "+string(method))
}

type probedSOCKS struct {
	config               config.SOCKSProxy
	parsed               connector.SOCKS5
	sourceRTT, targetRTT time.Duration
	sourceOK, targetOK   bool
}

type tcpProber interface {
	ProbeTCP(string) (time.Duration, error)
}

func (a *App) runSOCKSPool(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, sudoPasswords map[strategy.Direction]string) error {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("SOCKS 池仅用于两个 SSH 端点")
	}
	a.mu.RLock()
	configured := append([]config.SOCKSProxy(nil), a.document.SOCKS...)
	a.mu.RUnlock()
	if len(configured) == 0 {
		return errors.New("SOCKS 池为空")
	}
	sourceAgent, err := remoteagent.Start(ctx, source, preflight.SourceCapabilities.Architecture)
	if err != nil {
		return err
	}
	defer sourceAgent.Close()
	targetAgent, err := remoteagent.Start(ctx, target, preflight.TargetCapabilities.Architecture)
	if err != nil {
		return err
	}
	defer targetAgent.Close()
	var candidates []probedSOCKS
	for _, candidate := range configured {
		if candidate.Disabled {
			continue
		}
		parsed, parseErr := routespec.ParseSOCKS(candidate.Spec)
		if parseErr != nil {
			continue
		}
		probe := probedSOCKS{config: candidate, parsed: parsed}
		probe.sourceRTT, probe.sourceOK = probeTCPMedian(sourceAgent, parsed.Address)
		probe.targetRTT, probe.targetOK = probeTCPMedian(targetAgent, parsed.Address)
		if probe.sourceOK || probe.targetOK {
			candidates = append(candidates, probe)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].config.LastSuccess.IsZero() != candidates[j].config.LastSuccess.IsZero() {
			return !candidates[i].config.LastSuccess.IsZero()
		}
		left, right := bestRTT(candidates[i]), bestRTT(candidates[j])
		if left != right {
			return left < right
		}
		return candidates[i].config.LastSuccess.After(candidates[j].config.LastSuccess)
	})
	var failures []error
	for _, candidate := range candidates {
		for _, elevated := range []bool{false, true} {
			for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
				if direction == strategy.SourcePush && !candidate.sourceOK || direction == strategy.TargetPull && !candidate.targetOK {
					continue
				}
				rtt := candidate.sourceRTT
				if direction == strategy.TargetPull {
					rtt = candidate.targetRTT
				}
				proxy := candidate.parsed
				for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream} {
					if method == strategy.EncryptedStream {
						if runErr := a.runAgentStream(ctx, operation, preflight, direction, elevated, sudoPasswords[direction], &proxy, nil, ""); runErr != nil {
							if !transfer.Retryable(runErr) {
								return runErr
							}
							failures = append(failures, fmt.Errorf("%s/%s/%s/elevated=%t: %w", candidate.config.Name, direction, method, elevated, runErr))
							continue
						}
						a.rememberSOCKS(candidate.config.ID, rtt)
						return nil
					}
					if runErr := a.runAgentMethod(ctx, operation, preflight, direction, elevated, sudoPasswords[direction], &proxy, nil, method); runErr != nil {
						if !transfer.Retryable(runErr) {
							return runErr
						}
						failures = append(failures, fmt.Errorf("%s/%s/%s/elevated=%t: %w", candidate.config.Name, direction, method, elevated, runErr))
						continue
					}
					a.rememberSOCKS(candidate.config.ID, rtt)
					return nil
				}
			}
		}
	}
	if len(failures) == 0 {
		return errors.New("没有可从实际发起端访问的 SOCKS")
	}
	return errors.Join(failures...)
}

type probedRelay struct {
	host                 config.Host
	hops                 []connector.Hop
	sourceRTT, targetRTT time.Duration
	sourceOK, targetOK   bool
	cached               bool
}

func (a *App) runJumpPool(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, sudoPasswords map[strategy.Direction]string) error {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("SSH 会话池仅用于两个 SSH 端点")
	}
	a.mu.RLock()
	document := a.document.Clone()
	sourceHost, sourceFound := document.HostByName(source.Name())
	targetHost, targetFound := document.HostByName(target.Name())
	if !sourceFound || !targetFound {
		a.mu.RUnlock()
		return errors.New("端点主机配置不存在")
	}
	cached := a.cachedRelayLocked(sourceHost.ID, target.Name())
	ids := make([]string, 0, len(a.sessionSSH)+1)
	if cached != "" {
		ids = append(ids, cached)
	}
	for id := range a.sessionSSH {
		if id != sourceHost.ID && id != targetHost.ID && id != cached {
			ids = append(ids, id)
		}
	}
	a.mu.RUnlock()
	if len(ids) == 0 {
		return errors.New("没有已成功登录或缓存的 SSH 跳板会话")
	}
	sourceAgent, err := remoteagent.Start(ctx, source, preflight.SourceCapabilities.Architecture)
	if err != nil {
		return err
	}
	defer sourceAgent.Close()
	targetAgent, err := remoteagent.Start(ctx, target, preflight.TargetCapabilities.Architecture)
	if err != nil {
		return err
	}
	defer targetAgent.Close()
	var candidates []probedRelay
	for _, id := range ids {
		host := document.HostByID(id)
		if host == nil || host.Disabled {
			continue
		}
		route, routeErr := a.routeForHost(*host)
		if routeErr != nil || len(route.Hops) == 0 {
			continue
		}
		route.SOCKS = nil
		first := net.JoinHostPort(route.Hops[0].Host, fmt.Sprintf("%d", route.Hops[0].Port))
		probe := probedRelay{host: *host, hops: route.Hops, cached: id == cached}
		probe.sourceRTT, probe.sourceOK = probeTCPMedian(sourceAgent, first)
		probe.targetRTT, probe.targetOK = probeTCPMedian(targetAgent, first)
		if probe.sourceOK || probe.targetOK {
			candidates = append(candidates, probe)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].cached != candidates[j].cached {
			return candidates[i].cached
		}
		return bestRelayRTT(candidates[i]) < bestRelayRTT(candidates[j])
	})
	var failures []error
	for _, candidate := range candidates {
		for _, elevated := range []bool{false, true} {
			for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
				if direction == strategy.SourcePush && !candidate.sourceOK || direction == strategy.TargetPull && !candidate.targetOK {
					continue
				}
				for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream} {
					if method == strategy.EncryptedStream {
						if runErr := a.runAgentStream(ctx, operation, preflight, direction, elevated, sudoPasswords[direction], nil, candidate.hops, ""); runErr != nil {
							if !transfer.Retryable(runErr) {
								return runErr
							}
							failures = append(failures, fmt.Errorf("%s/%s/%s/elevated=%t: %w", candidate.host.Name, direction, method, elevated, runErr))
							continue
						}
						a.rememberRelay(sourceHost.ID, target.Name(), candidate.host.ID)
						return nil
					}
					if runErr := a.runAgentMethod(ctx, operation, preflight, direction, elevated, sudoPasswords[direction], nil, candidate.hops, method); runErr != nil {
						if !transfer.Retryable(runErr) {
							return runErr
						}
						failures = append(failures, fmt.Errorf("%s/%s/%s/elevated=%t: %w", candidate.host.Name, direction, method, elevated, runErr))
						continue
					}
					a.rememberRelay(sourceHost.ID, target.Name(), candidate.host.ID)
					return nil
				}
			}
		}
		if candidate.cached {
			a.forgetRelay(sourceHost.ID, target.Name())
		}
	}
	if len(failures) == 0 {
		return errors.New("没有可从实际发起端访问的 SSH 跳板")
	}
	return errors.Join(failures...)
}

func bestRelayRTT(candidate probedRelay) time.Duration {
	if candidate.sourceOK && candidate.targetOK && candidate.targetRTT < candidate.sourceRTT {
		return candidate.targetRTT
	}
	if candidate.sourceOK {
		return candidate.sourceRTT
	}
	return candidate.targetRTT
}

func probeTCPMedian(prober tcpProber, address string) (time.Duration, bool) {
	samples := make([]time.Duration, 0, 3)
	for attempt := 0; attempt < 3; attempt++ {
		if elapsed, err := prober.ProbeTCP(address); err == nil {
			samples = append(samples, elapsed)
		}
	}
	if len(samples) < 2 {
		return 0, false
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2], true
}

func (a *App) runHans(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, sudoPasswords map[strategy.Direction]string) error {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("Hans 仅用于两个 Linux SSH 端点")
	}
	type role struct {
		server, client         *endpoint.Remote
		direction              strategy.Direction
		serverArch, clientArch string
		serverSudo, clientSudo string
	}
	roles := []role{
		{server: target, client: source, direction: strategy.SourcePush, serverArch: preflight.TargetCapabilities.Architecture, clientArch: preflight.SourceCapabilities.Architecture, serverSudo: sudoPasswords[strategy.TargetPull], clientSudo: sudoPasswords[strategy.SourcePush]},
		{server: source, client: target, direction: strategy.TargetPull, serverArch: preflight.SourceCapabilities.Architecture, clientArch: preflight.TargetCapabilities.Architecture, serverSudo: sudoPasswords[strategy.SourcePush], clientSudo: sudoPasswords[strategy.TargetPull]},
	}
	var failures []error
	for _, candidate := range roles {
		if err := a.runHansRole(ctx, operation, preflight, candidate.server, candidate.client, candidate.serverArch, candidate.clientArch, candidate.direction, candidate.serverSudo, candidate.clientSudo); err != nil {
			if !transfer.Retryable(err) {
				return err
			}
			failures = append(failures, fmt.Errorf("server=%s: %w", candidate.server.Name(), err))
			continue
		}
		return nil
	}
	return errors.Join(failures...)
}

func (a *App) runHansRole(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, server, client *endpoint.Remote, serverArchitecture, clientArchitecture string, direction strategy.Direction, serverSudoPassword, clientSudoPassword string) error {
	serverRoot, err := remoteIsRoot(ctx, server)
	if err != nil {
		return err
	}
	serverAgent, cleanupServerAgent, err := a.startTransferAgent(ctx, server, serverArchitecture, !serverRoot, serverSudoPassword)
	if err != nil {
		return fmt.Errorf("启动 Hans server agent: %w", err)
	}
	defer cleanupServerAgent()
	clientAgent, err := remoteagent.Start(ctx, client, clientArchitecture)
	if err != nil {
		return fmt.Errorf("启动 Hans client agent: %w", err)
	}
	defer clientAgent.Close()
	serverBinary, err := remoteagent.InstallHans(ctx, serverAgent, serverArchitecture)
	if err != nil {
		return err
	}
	clientBinary, err := remoteagent.InstallHans(ctx, clientAgent, clientArchitecture)
	if err != nil {
		return err
	}
	network := randomHansNetwork()
	serverTunnelIP := strings.TrimSuffix(network, ".0") + ".1"
	passphrase := randomTransferToken(32)
	serverJob, clientJob := "hans-server-"+randomTransferToken(8), "hans-client-"+randomTransferToken(8)
	serverIdentity := server.Join(serverAgent.Directory, "hans-server.key")
	serverLease := server.Join(serverAgent.Directory, "hans-leases")
	fingerprint, err := serverAgent.StartHansServer(serverJob, serverBinary, network, serverIdentity, serverLease, passphrase)
	if err != nil {
		return err
	}
	defer serverAgent.StopProcess(serverJob)
	clientIdentity := client.Join(clientAgent.Directory, "hans-client.key")
	socksPort := randomPorts(1)[0]
	socksAddress := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", socksPort))
	addresses, _ := remoteIPv4s(ctx, server)
	addresses = prependAddress(addresses, server.ConnectionHost())
	var clientFailures []error
	started := false
	for _, address := range addresses {
		if net.ParseIP(strings.Trim(address, "[]")) == nil && strings.ContainsAny(address, " /\t\r\n") {
			continue
		}
		if err := clientAgent.StartHansClient(clientJob, clientBinary, address, socksAddress, clientIdentity, passphrase, fingerprint); err != nil {
			clientFailures = append(clientFailures, fmt.Errorf("%s: %w", address, err))
			continue
		}
		if err := waitForHansSOCKS(ctx, clientAgent, socksAddress); err != nil {
			_ = clientAgent.StopProcess(clientJob)
			clientFailures = append(clientFailures, fmt.Errorf("%s: %w", address, err))
			continue
		}
		started = true
		break
	}
	if !started {
		if len(clientFailures) == 0 {
			return errors.New("Hans server 没有可供 client 使用的真实地址")
		}
		return errors.Join(clientFailures...)
	}
	defer clientAgent.StopProcess(clientJob)
	proxy, err := routespec.ParseSOCKS(socksAddress)
	if err != nil {
		return err
	}
	var transferFailures []error
	for _, elevated := range []bool{false, true} {
		transferAgent := clientAgent
		var cleanupTransferAgent func()
		if elevated {
			transferAgent, cleanupTransferAgent, err = a.startTransferAgent(ctx, client, clientArchitecture, true, clientSudoPassword)
			if err != nil {
				transferFailures = append(transferFailures, fmt.Errorf("elevated client: %w", err))
				continue
			}
		}
		for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream} {
			if method == strategy.EncryptedStream {
				err = a.runAgentStream(ctx, operation, preflight, direction, elevated, clientSudoPassword, &proxy, nil, serverTunnelIP)
			} else {
				err = a.runHansMethod(ctx, operation, transferAgent, server, direction, elevated, serverTunnelIP, socksAddress, method)
			}
			if err != nil {
				transferFailures = append(transferFailures, fmt.Errorf("%s/elevated=%t: %w", method, elevated, err))
				continue
			}
			if cleanupTransferAgent != nil {
				cleanupTransferAgent()
			}
			return nil
		}
		if cleanupTransferAgent != nil {
			cleanupTransferAgent()
		}
	}
	return errors.Join(transferFailures...)
}

func (a *App) runHansMethod(ctx context.Context, operation transfer.Operation, agent *remoteagent.Session, server *endpoint.Remote, direction strategy.Direction, elevated bool, tunnelHost, socksAddress string, method strategy.Method) error {
	before, err := snapshotForAgentAttempt(ctx, operation, elevated && direction == strategy.SourcePush, agent)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if method == strategy.SCP && item.Mode&fs.ModeSymlink != 0 {
			return errors.New("Hans SCP 不保证符号链接语义")
		}
	}
	elevatedTarget := elevated && direction == strategy.TargetPull
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); !elevatedTarget && (err == nil || !errors.Is(err, fs.ErrNotExist)) {
		return errors.New("Hans SCP 要求目标根路径不存在")
	}
	a.mu.RLock()
	host, ok := a.document.HostByName(server.Name())
	a.mu.RUnlock()
	if !ok {
		return errors.New("Hans server 主机配置不存在")
	}
	route, err := a.routeForHost(host)
	if err != nil {
		return err
	}
	if len(route.Hops) == 0 {
		return errors.New("Hans server SSH 路由为空")
	}
	final := route.Hops[len(route.Hops)-1]
	final.Host = tunnelHost
	proxy, err := routespec.ParseSOCKS(socksAddress)
	if err != nil {
		return err
	}
	route = connector.Route{Hops: []connector.Hop{final}, Timeout: 15 * time.Second, SOCKS: &proxy}
	payload, err := agentroute.Encode(route)
	if err != nil {
		return err
	}
	partial := operation.TargetPath + ".dragfm-partial-" + randomTransferToken(8)
	action := string(method) + "-upload"
	if direction == strategy.TargetPull {
		action = string(method) + "-download"
	}
	if method == strategy.Rsync {
		err = agent.RsyncContext(ctx, action, operation.SourcePath, partial, payload)
	} else {
		err = agent.SCPContext(ctx, action, operation.SourcePath, partial, payload, before.Items[0].Mode.IsDir())
	}
	if err != nil {
		if elevatedTarget {
			_ = agent.RemovePath(partial, before.Items[0].Mode.IsDir())
		} else {
			_ = operation.Destination.Remove(context.Background(), partial, before.Items[0].Mode.IsDir())
		}
		return err
	}
	if elevatedTarget {
		err = agent.Commit(partial, operation.TargetPath, operation.Overwrite)
	} else {
		err = operation.Destination.Rename(ctx, partial, operation.TargetPath, false)
	}
	if err != nil {
		return err
	}
	if elevatedTarget {
		return finishAcceleratedMoveWithAgentTarget(ctx, operation, before, "Hans v5 "+string(method), agent)
	}
	if elevated && direction == strategy.SourcePush {
		return finishAcceleratedMoveWithAgentSource(ctx, operation, before, "Hans v5 "+string(method), agent)
	}
	return finishAcceleratedMove(ctx, operation, before, "Hans v5 "+string(method))
}

func remoteIsRoot(ctx context.Context, remote *endpoint.Remote) (bool, error) {
	var output bytes.Buffer
	if err := remote.Exec(ctx, "id -u", endpoint.ExecOptions{Stdout: &output}); err != nil {
		return false, err
	}
	return strings.TrimSpace(output.String()) == "0", nil
}

func waitForHansSOCKS(ctx context.Context, agent *remoteagent.Session, address string) error {
	var last error
	for attempt := 0; attempt < 6; attempt++ {
		if _, err := agent.ProbeTCP(address); err == nil {
			return nil
		} else {
			last = err
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("Hans SOCKS 未就绪: %w", last)
}

func randomHansNetwork() string {
	var value [2]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return fmt.Sprintf("10.%d.%d.0", 100+int(value[0])%120, 1+int(value[1])%253)
}

func bestRTT(candidate probedSOCKS) time.Duration {
	if candidate.sourceOK && candidate.targetOK && candidate.targetRTT < candidate.sourceRTT {
		return candidate.targetRTT
	}
	if candidate.sourceOK {
		return candidate.sourceRTT
	}
	return candidate.targetRTT
}

func (a *App) rememberSOCKS(id string, rtt time.Duration) {
	a.mu.Lock()
	for index := range a.document.SOCKS {
		if a.document.SOCKS[index].ID == id {
			a.document.SOCKS[index].LastRTT = rtt.Milliseconds()
			a.document.SOCKS[index].LastSuccess = time.Now()
			break
		}
	}
	a.mu.Unlock()
	_ = a.save()
}

func (a *App) transferApproval(protectedDirectory string, setSudo func(*endpoint.SudoLocal), operation transfer.Operation, remoteSudo map[strategy.Direction]string) strategy.Approval {
	return func(ctx context.Context, risk strategy.Risk, attempt strategy.Attempt) error {
		if risk == strategy.ListenRisk {
			_, accepted := a.ask(ctx, ChallengeModel{Kind: "confirm", Title: "允许一次加密直连监听", Message: "两端临时 agent 将在随机端口监听一次。数据通道使用 TLS 1.3、临时证书指纹固定和一次性随机令牌；任务结束或取消时监听和临时目录都会关闭。"})
			if !accepted {
				return errors.New("已取消临时监听，源文件未修改")
			}
			return nil
		}
		if risk == strategy.PlaintextRisk {
			_, accepted := a.ask(ctx, ChallengeModel{Kind: "confirm", Title: "允许明文 tar+ncat", Message: "无法确认两端监听地址均为私网。继续会在随机端口用未加密的 tar+ncat 传输；文件内容可能被链路观察或篡改。建议仅在可信隔离网络中使用。"})
			if !accepted {
				return errors.New("已取消明文 ncat 传输")
			}
			return nil
		}
		if risk == strategy.HansRisk {
			_, accepted := a.ask(ctx, ChallengeModel{Kind: "confirm", Title: "允许 Hans v5 ICMP 隧道", Message: "将选择一端以 root/sudo 启动临时 Hans server（会创建临时 TUN/veth 与 ICMP 状态），另一端启动无需 TUN 的 userspace SOCKS client。使用随机网段、随机强口令、独立临时身份、服务端指纹固定和 --require-v5；任务结束或取消后停止进程并清理所属临时目录。"})
			if !accepted {
				return errors.New("已取消 Hans 隧道")
			}
			return nil
		}
		if risk == strategy.SourceSudoRisk || risk == strategy.TargetSudoRisk {
			remote := remoteForElevatedAttempt(operation, attempt.Direction)
			title := "源端提权"
			if attempt.Direction == strategy.TargetPull {
				title = "目标端提权"
			}
			if remote == nil {
				return errors.New("提权端点不是 SSH 主机")
			}
			stored := a.savedSudoPassword(remote.Name())
			answer, accepted := a.ask(ctx, ChallengeModel{Kind: "password", Title: title + " · " + remote.Name(), Message: "将仅为本次传输使用 sudo 启动临时 agent。留空会先用保险库中已保存的 sudo 密码，否则尝试 sudo -n；不会启动 root Shell。", Secret: true, AllowSave: true})
			if !accepted {
				return errors.New("已取消远端 sudo，源文件未修改")
			}
			password := answer.Value
			if password == "" {
				password = stored
			}
			remoteSudo[attempt.Direction] = password
			if answer.Save && answer.Value != "" {
				a.saveSudoPassword(remote.Name(), answer.Value)
			}
			return nil
		}
		if risk != strategy.SudoRisk {
			return nil
		}
		answer, accepted := a.ask(ctx, ChallengeModel{
			Kind:    "password",
			Title:   "目标目录需要管理员权限",
			Message: fmt.Sprintf("当前用户不能写入：\n%s\n\n程序将仅对这次传输使用 sudo，在目标目录创建随机 .dragfm-partial 文件，完成后原子改名。密码仅通过 stdin 传给 sudo；留空会尝试已有的 sudo 授权。", protectedDirectory),
			Secret:  true,
		})
		if !accepted {
			return errors.New("已取消管理员权限传输，源文件未修改")
		}
		candidate := endpoint.NewSudoLocal(answer.Value)
		if err := candidate.Check(ctx); err != nil {
			return fmt.Errorf("sudo 验证失败，源文件未修改: %w", err)
		}
		if setSudo != nil {
			setSudo(candidate)
		}
		return nil
	}
}

func remoteForElevatedAttempt(operation transfer.Operation, direction strategy.Direction) *endpoint.Remote {
	if direction == strategy.SourcePush {
		remote, _ := operation.Source.(*endpoint.Remote)
		return remote
	}
	remote, _ := operation.Destination.(*endpoint.Remote)
	return remote
}

func (a *App) savedSudoPassword(name string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	host, ok := a.document.HostByName(name)
	if !ok {
		return ""
	}
	return host.SudoPassword
}

func (a *App) saveSudoPassword(name, password string) {
	a.mu.Lock()
	for index := range a.document.Hosts {
		if a.document.Hosts[index].Name == name {
			a.document.Hosts[index].SudoPassword = password
			break
		}
	}
	a.mu.Unlock()
	_ = a.save()
}

// startTransferAgent uses an explicitly configured root SSH identity only
// inside an already-approved elevated attempt. It never adds root routes to
// browsing, capability probes, SOCKS sorting, or relay discovery. If root SSH
// is unavailable, the same attempt falls back to the scoped sudo helper.
func (a *App) startTransferAgent(ctx context.Context, remote *endpoint.Remote, architecture string, elevated bool, sudoPassword string) (*remoteagent.Session, func(), error) {
	if !elevated {
		agent, err := remoteagent.Start(ctx, remote, architecture)
		if err != nil {
			return nil, nil, err
		}
		return agent, func() { _ = agent.Close() }, nil
	}

	var rootErr error
	a.mu.RLock()
	host, configured := a.document.HostByName(remote.Name())
	a.mu.RUnlock()
	if configured && host.RootUser != "" {
		route, err := a.routeForHost(host)
		if err == nil {
			route, err = configuredRootRoute(route, host.RootUser, host.RootPassword)
		}
		if err == nil {
			final := &route.Hops[len(route.Hops)-1]
			rootRemote, dialErr := endpoint.DialSSH(ctx, remote.Name(), final.HostKey.PinnedSHA256, route)
			if dialErr == nil {
				agent, startErr := remoteagent.Start(ctx, rootRemote, architecture)
				if startErr == nil {
					return agent, func() {
						_ = agent.Close()
						_ = rootRemote.Close()
					}, nil
				}
				_ = rootRemote.Close()
				rootErr = fmt.Errorf("root SSH helper: %w", startErr)
			} else {
				rootErr = fmt.Errorf("root SSH: %w", dialErr)
			}
		} else if err != nil {
			rootErr = fmt.Errorf("root SSH route: %w", err)
		}
	}
	agent, sudoErr := remoteagent.StartElevated(ctx, remote, architecture, sudoPassword)
	if sudoErr != nil {
		return nil, nil, errors.Join(rootErr, fmt.Errorf("scoped sudo helper: %w", sudoErr))
	}
	return agent, func() { _ = agent.Close() }, nil
}

func configuredRootRoute(route connector.Route, user, password string) (connector.Route, error) {
	if user == "" || len(route.Hops) == 0 {
		return connector.Route{}, errors.New("root SSH route is incomplete")
	}
	result := route
	result.Hops = append([]connector.Hop(nil), route.Hops...)
	final := &result.Hops[len(result.Hops)-1]
	final.User = user
	final.Credentials.Password = password
	return result, nil
}

func remoteRemotePair(operation transfer.Operation) (*endpoint.Remote, *endpoint.Remote, bool) {
	source, sourceOK := operation.Source.(*endpoint.Remote)
	target, targetOK := operation.Destination.(*endpoint.Remote)
	return source, target, sourceOK && targetOK
}

func (a *App) runAgentStream(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, direction strategy.Direction, elevated bool, sudoPassword string, socksProxy *connector.SOCKS5, prefix []connector.Hop, preferredListenerAddress string) error {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("加密直连流仅用于两个 Linux SSH 端点")
	}
	sourceAgent, cleanupSourceAgent, err := a.startTransferAgent(ctx, source, preflight.SourceCapabilities.Architecture, elevated && direction == strategy.SourcePush, sudoPassword)
	if err != nil {
		return fmt.Errorf("启动源端 agent: %w", err)
	}
	defer cleanupSourceAgent()
	targetAgent, cleanupTargetAgent, err := a.startTransferAgent(ctx, target, preflight.TargetCapabilities.Architecture, elevated && direction == strategy.TargetPull, sudoPassword)
	if err != nil {
		return fmt.Errorf("启动目标端 agent: %w", err)
	}
	defer cleanupTargetAgent()
	before, err := snapshotForAgentAttempt(ctx, operation, elevated && direction == strategy.SourcePush, sourceAgent)
	if err != nil {
		return err
	}
	elevatedTarget := elevated && direction == strategy.TargetPull
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); !elevatedTarget && (err == nil || !errors.Is(err, fs.ErrNotExist)) {
		return errors.New("目标已存在，加密流原子根路径跳过并改用流式合并")
	}
	partial := operation.TargetPath + ".dragfm-partial-" + randomTransferToken(8)
	job, token := "job-"+randomTransferToken(12), randomTransferToken(32)
	routePayload := ""
	if len(prefix) > 0 {
		other := target
		if direction == strategy.TargetPull {
			other = source
		}
		a.mu.RLock()
		host, exists := a.document.HostByName(other.Name())
		a.mu.RUnlock()
		if !exists {
			return fmt.Errorf("未找到端点 %q 的路由", other.Name())
		}
		route, routeErr := a.routeForHost(host)
		if routeErr != nil {
			return routeErr
		}
		route.SOCKS = nil
		route.Hops = append(append([]connector.Hop(nil), prefix...), route.Hops...)
		routePayload, err = agentroute.Encode(route)
		if err != nil {
			return err
		}
	}
	cleanup := func() {
		if elevatedTarget {
			_ = targetAgent.RemovePath(partial, before.Items[0].Mode.IsDir())
		} else {
			_ = operation.Destination.Remove(context.Background(), partial, before.Items[0].Mode.IsDir())
		}
	}
	var listener remoteagent.Listener
	var connector *remoteagent.Session
	connectAction := "connect-send"
	waiter := targetAgent
	if direction == strategy.SourcePush {
		listener, err = targetAgent.Listen("listen-receive", job, partial, token, false)
		listener.Addresses = prependAddress(listener.Addresses, target.ConnectionHost())
		connector = sourceAgent
	} else {
		listener, err = sourceAgent.Listen("listen-send", job, operation.SourcePath, token, false)
		listener.Addresses = prependAddress(listener.Addresses, source.ConnectionHost())
		connector, connectAction, waiter = targetAgent, "connect-receive", sourceAgent
	}
	listener.Addresses = prependAddress(listener.Addresses, preferredListenerAddress)
	if err != nil {
		cleanup()
		return err
	}
	var failures []error
	connected := false
	for _, address := range listener.Addresses {
		if connectErr := connector.Connect(connectAction, map[bool]string{true: operation.SourcePath, false: partial}[direction == strategy.SourcePush], net.JoinHostPort(address, listener.Port), token, listener.Pin, socksProxy, routePayload, elevatedTarget && direction == strategy.TargetPull); connectErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", address, connectErr))
			continue
		}
		connected = true
		break
	}
	if !connected {
		cleanup()
		return errors.Join(failures...)
	}
	if err := waiter.Wait(job); err != nil {
		cleanup()
		return err
	}
	if elevatedTarget {
		err = targetAgent.Commit(partial, operation.TargetPath, operation.Overwrite)
	} else {
		err = operation.Destination.Rename(ctx, partial, operation.TargetPath, false)
	}
	if err != nil {
		cleanup()
		return err
	}
	if elevatedTarget {
		return finishAcceleratedMoveWithAgentTarget(ctx, operation, before, "加密直连流", targetAgent)
	}
	if elevated && direction == strategy.SourcePush {
		return finishAcceleratedMoveWithAgentSource(ctx, operation, before, "加密直连流", sourceAgent)
	}
	return finishAcceleratedMove(ctx, operation, before, "加密直连流")
}

func prependAddress(addresses []string, candidate string) []string {
	if candidate == "" {
		return addresses
	}
	result := []string{candidate}
	for _, address := range addresses {
		if address != candidate {
			result = append(result, address)
		}
	}
	return result
}

func privateConnectionHosts(operation transfer.Operation) bool {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return false
	}
	for _, value := range []string{source.ConnectionHost(), target.ConnectionHost()} {
		ip := net.ParseIP(strings.Trim(value, "[]"))
		if ip == nil || !ip.IsPrivate() {
			return false
		}
	}
	return true
}

func runNcatTar(ctx context.Context, operation transfer.Operation, direction strategy.Direction) error {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("tar+ncat 仅用于两个 SSH 端点")
	}
	before, err := transfer.Snapshot(ctx, operation.Source, operation.SourcePath, operation.Move)
	if err != nil {
		return err
	}
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return errors.New("目标已存在，ncat 原子根路径跳过并改用控制机流式合并")
	}
	listenerEndpoint, connectorEndpoint := target, source
	if direction == strategy.TargetPull {
		listenerEndpoint, connectorEndpoint = source, target
	}
	addresses, err := remoteIPv4s(ctx, listenerEndpoint)
	if err != nil {
		return err
	}
	base := path.Base(operation.SourcePath)
	partial := operation.TargetPath + ".dragfm-partial-" + randomTransferToken(8)
	stage := partial + ".stage"
	producer := "tar -czf - -C " + quoteRemote(path.Dir(operation.SourcePath)) + " -- " + quoteRemote(base)
	consumer := ncatTarConsumer(stage, base, partial)
	var failures []error
	for _, port := range randomPorts(5) {
		for _, address := range addresses {
			listenScript, connectScript := "", ""
			endpointArguments := quoteRemote(address) + " " + fmt.Sprintf("%d", port)
			if direction == strategy.SourcePush {
				listenScript = "ncat -l " + quoteRemote(address) + " " + fmt.Sprintf("%d", port) + " --recv-only | " + consumer
				connectScript = producer + " | ncat --send-only " + endpointArguments
			} else {
				listenScript = producer + " | ncat -l " + quoteRemote(address) + " " + fmt.Sprintf("%d", port) + " --send-only"
				connectScript = "ncat --recv-only " + endpointArguments + " | " + consumer
			}
			attemptCtx, cancel := context.WithCancel(ctx)
			listenerDone := make(chan error, 1)
			var listenerOutput bytes.Buffer
			go func() {
				listenerDone <- listenerEndpoint.Exec(attemptCtx, bashPipefail(listenScript), endpoint.ExecOptions{Stdout: &listenerOutput, Stderr: &listenerOutput})
			}()
			// The SSH exec request returning does not mean ncat has completed its
			// bind yet. Give old/busy hosts a bounded startup window; failed ports
			// still advance through the five random candidates.
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case listenerErr := <-listenerDone:
				cancel()
				timer.Stop()
				failures = append(failures, fmt.Errorf("%s:%d listener: %w: %s", address, port, listenerErr, strings.TrimSpace(listenerOutput.String())))
				continue
			case <-timer.C:
			case <-ctx.Done():
				cancel()
				return ctx.Err()
			}
			var connectorOutput bytes.Buffer
			connectErr := connectorEndpoint.Exec(attemptCtx, bashPipefail(connectScript), endpoint.ExecOptions{Stdout: &connectorOutput, Stderr: &connectorOutput})
			if connectErr != nil {
				cancel()
			}
			listenerErr := <-listenerDone
			cancel()
			if connectErr != nil || listenerErr != nil {
				_ = operation.Destination.Remove(context.Background(), stage, true)
				_ = operation.Destination.Remove(context.Background(), partial, before.Items[0].Mode.IsDir())
				failures = append(failures, fmt.Errorf("%s:%d: %w: %s %s", address, port, errors.Join(connectErr, listenerErr), strings.TrimSpace(connectorOutput.String()), strings.TrimSpace(listenerOutput.String())))
				continue
			}
			if err := operation.Destination.Rename(ctx, partial, operation.TargetPath, false); err != nil {
				return err
			}
			return finishAcceleratedMove(ctx, operation, before, "tar+ncat")
		}
	}
	return errors.Join(failures...)
}

func remoteIPv4s(ctx context.Context, remote *endpoint.Remote) ([]string, error) {
	var output bytes.Buffer
	command := `hostname -I 2>/dev/null || ip -o -4 addr show scope global 2>/dev/null | awk '{print $4}'`
	if err := remote.Exec(ctx, command, endpoint.ExecOptions{Stdout: &output}); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var result []string
	for _, field := range strings.Fields(output.String()) {
		field = strings.SplitN(field, "/", 2)[0]
		ip := net.ParseIP(field)
		if ip == nil || ip.To4() == nil || ip.IsLoopback() || seen[field] {
			continue
		}
		seen[field] = true
		result = append(result, field)
	}
	if len(result) == 0 {
		return nil, errors.New("监听端没有可绑定的 IPv4 地址")
	}
	return result, nil
}

func randomPorts(count int) []int {
	result := make([]int, 0, count)
	seen := make(map[int]bool)
	for len(result) < count {
		var raw [2]byte
		if _, err := rand.Read(raw[:]); err != nil {
			panic(err)
		}
		rawValue := int(raw[0])<<8 | int(raw[1])
		port := 20000 + rawValue%41000
		if !seen[port] {
			seen[port] = true
			result = append(result, port)
		}
	}
	return result
}

func ncatTarConsumer(stage, base, partial string) string {
	// The braces are required. Without them, `ncat | mkdir && tar` pipes the
	// archive into mkdir and leaves tar reading the SSH session's stdin forever.
	return "{ mkdir -m 0700 -- " + quoteRemote(stage) + " && tar -xzf - -C " + quoteRemote(stage) + " && mv -- " + quoteRemote(path.Join(stage, base)) + " " + quoteRemote(partial) + " && rmdir -- " + quoteRemote(stage) + "; }"
}

func quoteRemote(value string) string   { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func bashPipefail(script string) string { return "bash -o pipefail -c " + quoteRemote(script) }

func randomTransferToken(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}

func runRsync(ctx context.Context, remote *endpoint.Remote, operation transfer.Operation) error {
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return errors.New("目标已存在，rsync 原子根路径跳过并改用流式合并")
	}
	before, err := transfer.Snapshot(ctx, operation.Source, operation.SourcePath, operation.Move)
	if err != nil {
		return err
	}
	partial := operation.TargetPath + fmt.Sprintf(".dragfm-partial-%d", time.Now().UnixNano())
	direction := rsyncbridge.Upload
	if _, ok := operation.Source.(*endpoint.Remote); ok {
		direction = rsyncbridge.Download
	}
	if err := rsyncbridge.Run(ctx, remote.SSHClient(), direction, operation.SourcePath, partial); err != nil {
		_ = operation.Destination.Remove(context.Background(), partial, before.Items[0].Mode.IsDir())
		return err
	}
	if err := operation.Destination.Rename(ctx, partial, operation.TargetPath, false); err != nil {
		_ = operation.Destination.Remove(context.Background(), partial, before.Items[0].Mode.IsDir())
		return err
	}
	return finishAcceleratedMove(ctx, operation, before, "rsync")
}

func finishAcceleratedMove(ctx context.Context, operation transfer.Operation, before transfer.Manifest, method string) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = transfer.PreserveSource(retErr)
		}
	}()
	if !operation.Move {
		return nil
	}
	afterSource, err := transfer.Snapshot(ctx, operation.Source, operation.SourcePath, true)
	if err != nil {
		return err
	}
	if err := transfer.CompareManifests(before, afterSource, false); err != nil {
		return errors.Join(transfer.ErrSourceChanged, err)
	}
	afterTarget, err := transfer.Snapshot(ctx, operation.Destination, operation.TargetPath, true)
	if err != nil {
		return err
	}
	if err := transfer.CompareManifests(before, afterTarget, true); err != nil {
		return fmt.Errorf("%s 后 SHA-256 校验失败，源已保留: %w", method, err)
	}
	if err := transfer.VerifySourceUnchanged(ctx, operation.Source, operation.SourcePath, before); err != nil {
		return err
	}
	return operation.Source.Remove(ctx, operation.SourcePath, before.Items[0].Mode.IsDir())
}

func snapshotForAgentAttempt(ctx context.Context, operation transfer.Operation, elevatedSource bool, sourceAgent *remoteagent.Session) (transfer.Manifest, error) {
	if elevatedSource {
		return sourceAgent.Manifest(operation.SourcePath)
	}
	return transfer.Snapshot(ctx, operation.Source, operation.SourcePath, operation.Move)
}

func finishAcceleratedMoveWithAgentTarget(ctx context.Context, operation transfer.Operation, before transfer.Manifest, method string, targetAgent *remoteagent.Session) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = transfer.PreserveSource(retErr)
		}
	}()
	if !operation.Move {
		return nil
	}
	afterSource, err := transfer.Snapshot(ctx, operation.Source, operation.SourcePath, true)
	if err != nil {
		return err
	}
	if err := transfer.CompareManifests(before, afterSource, false); err != nil {
		return errors.Join(transfer.ErrSourceChanged, err)
	}
	afterTarget, err := targetAgent.Manifest(operation.TargetPath)
	if err != nil {
		return fmt.Errorf("%s 后提权读取目标清单失败，源已保留: %w", method, err)
	}
	if err := transfer.CompareManifests(before, afterTarget, true); err != nil {
		return fmt.Errorf("%s 后 SHA-256 校验失败，源已保留: %w", method, err)
	}
	if err := transfer.VerifySourceUnchanged(ctx, operation.Source, operation.SourcePath, before); err != nil {
		return err
	}
	return operation.Source.Remove(ctx, operation.SourcePath, before.Items[0].Mode.IsDir())
}

func finishAcceleratedMoveWithAgentSource(ctx context.Context, operation transfer.Operation, before transfer.Manifest, method string, sourceAgent *remoteagent.Session) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = transfer.PreserveSource(retErr)
		}
	}()
	if !operation.Move {
		return nil
	}
	afterSource, err := sourceAgent.Manifest(operation.SourcePath)
	if err != nil {
		return fmt.Errorf("%s 后提权复查源清单失败，源已保留: %w", method, err)
	}
	if err := transfer.CompareManifests(before, afterSource, false); err != nil {
		return errors.Join(transfer.ErrSourceChanged, err)
	}
	afterTarget, err := transfer.Snapshot(ctx, operation.Destination, operation.TargetPath, true)
	if err != nil {
		return fmt.Errorf("%s 后读取目标清单失败，源已保留: %w", method, err)
	}
	if err := transfer.CompareManifests(before, afterTarget, true); err != nil {
		return fmt.Errorf("%s 后 SHA-256 校验失败，源已保留: %w", method, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	finalSource, err := sourceAgent.Manifest(operation.SourcePath)
	if err != nil {
		return err
	}
	if err := transfer.CompareManifests(before, finalSource, false); err != nil {
		return errors.Join(transfer.ErrSourceChanged, err)
	}
	return sourceAgent.RemovePath(operation.SourcePath, before.Items[0].Mode.IsDir())
}

func localDestinationRequiresSudo(ctx context.Context, operation transfer.Operation) (bool, string) {
	return localDestinationRequiresSudoWithProbe(ctx, operation, probeDirectoryWritable)
}

func localDestinationRequiresSudoWithProbe(ctx context.Context, operation transfer.Operation, probe func(string) error) (bool, string) {
	if _, ok := operation.Destination.(*endpoint.Local); !ok {
		return false, ""
	}
	directory := operation.Destination.Dir(operation.TargetPath)
	if source, sourceErr := operation.Source.Stat(ctx, operation.SourcePath); sourceErr == nil && source.IsDir() {
		if target, targetErr := operation.Destination.Stat(ctx, operation.TargetPath); targetErr == nil && target.IsDir() {
			directory = operation.TargetPath
		}
	}
	err := probe(directory)
	if err == nil {
		return false, directory
	}
	if errors.Is(err, fs.ErrPermission) || os.IsPermission(err) {
		return true, directory
	}
	return false, directory
}

func probeDirectoryWritable(directory string) error {
	probe, err := os.CreateTemp(directory, ".dragfm-write-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	return errors.Join(probe.Close(), os.Remove(name))
}

func localRemotePair(operation transfer.Operation) (*endpoint.Remote, strategy.Direction, bool) {
	if _, ok := operation.Source.(*endpoint.Local); ok {
		if remote, ok := operation.Destination.(*endpoint.Remote); ok {
			return remote, strategy.SourcePush, true
		}
	}
	if remote, ok := operation.Source.(*endpoint.Remote); ok {
		if _, ok := operation.Destination.(*endpoint.Local); ok {
			return remote, strategy.TargetPull, true
		}
	}
	return nil, "", false
}

func runFlySSHSCP(ctx context.Context, remote *endpoint.Remote, operation transfer.Operation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Overwrite and directory merge semantics are handled by the atomic stream
	// fallback; plain SCP is only used for a new root target.
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return errors.New("目标已存在，SCP 跳过并改用原子流")
	}
	before, err := transfer.Snapshot(ctx, operation.Source, operation.SourcePath, operation.Move)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if item.Mode&fs.ModeSymlink != 0 {
			return errors.New("SCP 不保证符号链接语义，改用原子流")
		}
	}
	partial := operation.TargetPath + fmt.Sprintf(".dragfm-partial-%d", time.Now().UnixNano())
	direction := flytransfer.DirectionUpload
	if _, ok := operation.Source.(*endpoint.Remote); ok {
		direction = flytransfer.DirectionDownload
	}
	flags := []string{"-p"}
	if before.Items[0].Mode.IsDir() {
		flags = append(flags, "-r")
	}
	spec := &flytransfer.Spec{Mode: flytransfer.ModeSCP, Direction: direction, Flags: flags, Sources: []string{operation.SourcePath}, Target: partial}
	code, err := flytransfer.RunContext(ctx, remote.SSHClient(), spec)
	if err != nil || code != 0 {
		_ = operation.Destination.Remove(context.Background(), partial, before.Items[0].Mode.IsDir())
		if err != nil {
			return err
		}
		return fmt.Errorf("scp exit code %d", code)
	}
	if err := operation.Destination.Rename(ctx, partial, operation.TargetPath, false); err != nil {
		_ = operation.Destination.Remove(context.Background(), partial, before.Items[0].Mode.IsDir())
		return err
	}
	return finishAcceleratedMove(ctx, operation, before, "SCP")
}
