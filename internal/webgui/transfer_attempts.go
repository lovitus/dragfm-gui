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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	flytransfer "github.com/flyssh/flyssh/pkg/transfer"
	"github.com/lovitus/dragfm-gui/internal/activity"
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
	return a.planTransfer(operation, preflight, probe, make(map[strategy.Direction]string))
}

func (a *App) planTransfer(operation transfer.Operation, preflight transfer.PreflightReport, probe func(string) error, remoteSudo map[strategy.Direction]string) ([]strategy.Attempt, strategy.Approval) {
	var attempts []strategy.Attempt
	requiresSudo, protectedDirectory := localDestinationRequiresSudoWithProbe(context.Background(), operation, probe)
	if !requiresSudo {
		if preflight.SameMachine {
			attempts = append(attempts, strategy.Attempt{Tier: strategy.SameHost, Direction: strategy.SourcePush, Method: strategy.MemoryStream, Run: func(ctx context.Context) error {
				_, err := transfer.Run(ctx, fileOperation(ctx, operation))
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
						return a.runNativeMethod(ctx, operation, preflight, descriptor.Direction, descriptor.Elevated, remoteSudo[descriptor.Direction], nil, nil, descriptor.Method)
					}
				case strategy.EncryptedStream:
					descriptor.Run = func(ctx context.Context) error {
						return a.runAgentStream(ctx, operation, preflight, descriptor.Direction, descriptor.Elevated, remoteSudo[descriptor.Direction], nil, nil, "")
					}
				case strategy.NcatTar:
					descriptor.Run = func(ctx context.Context) error {
						err := a.runSystemNcatTar(ctx, operation, descriptor.Direction, descriptor.Elevated)
						if err == nil || ctx.Err() != nil || !transfer.Retryable(err) {
							return err
						}
						// An OS may omit sftp-server but permit the uploaded helper.
						// Keep that capability without making it a prerequisite for
						// the independent fallback or retrying a committed transfer.
						if needsElevation(ctx, strategy.SourcePush, descriptor.Elevated && descriptor.Direction == strategy.SourcePush) || needsElevation(ctx, strategy.TargetPull, descriptor.Elevated && descriptor.Direction == strategy.TargetPull) {
							if fallbackErr := a.runAgentStreamWithCarrier(ctx, operation, preflight, descriptor.Direction, descriptor.Elevated, remoteSudo[descriptor.Direction], nil, nil, "", "ncat"); fallbackErr != nil {
								return errors.Join(err, fallbackErr)
							}
							return nil
						}
						return err
					}
				}
				attempts = append(attempts, descriptor)
			}
			attempts = append(attempts, strategy.Attempt{Tier: strategy.SOCKSPool, Group: true, Run: func(ctx context.Context) error {
				return a.runSOCKSPool(ctx, operation, preflight, remoteSudo)
			}})
			attempts = append(attempts, strategy.Attempt{Tier: strategy.JumpPool, Group: true, Run: func(ctx context.Context) error {
				return a.runJumpPool(ctx, operation, preflight, remoteSudo)
			}})
			attempts = append(attempts, strategy.Attempt{Tier: strategy.Hans, Group: true, Risk: strategy.HansRisk, Run: func(ctx context.Context) error {
				return a.runHans(ctx, operation, preflight, remoteSudo)
			}})
		}
		attempts = append(attempts, strategy.Attempt{Tier: strategy.ControllerRelay, Direction: strategy.SourcePush, Method: strategy.MemoryStream, Run: func(ctx context.Context) error {
			if access := accessFor(ctx); access != nil {
				return access.relay(ctx)
			}
			_, err := transfer.Run(ctx, operation)
			return err
		}})
	}
	if !requiresSudo {
		return attempts, a.transferApproval("", nil, operation, remoteSudo)
	}
	var elevated *endpoint.SudoLocal
	attempts = append(attempts, strategy.Attempt{Tier: strategy.ControllerRelay, Direction: strategy.TargetPull, Elevated: true, Method: strategy.MemoryStream, Risk: strategy.SudoRisk, Run: func(ctx context.Context) error {
		if access := accessFor(ctx); access != nil {
			return access.relay(ctx)
		}
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
				var risks []strategy.Risk
				if elevated && (method == strategy.Rsync || method == strategy.SCP) {
					peerRisk := strategy.TargetSudoRisk
					if direction == strategy.TargetPull {
						peerRisk = strategy.SourceSudoRisk
					}
					risks = append(risks, peerRisk)
				}
				if method == strategy.EncryptedStream {
					if elevated {
						risks = append(risks, strategy.ListenRisk)
					} else {
						methodRisk = strategy.ListenRisk
					}
				}
				attempts = append(attempts, strategy.Attempt{Tier: strategy.Direct, Direction: direction, Elevated: elevated, Method: method, Risk: methodRisk, Risks: risks})
			}
			if preflight.SourceCapabilities.Tools["ncat"] && preflight.TargetCapabilities.Tools["ncat"] {
				ncatRisk := risk
				var risks []strategy.Risk
				if elevated {
					risks = append(risks, strategy.ListenRisk)
				} else {
					ncatRisk = strategy.ListenRisk
				}
				if !privateHosts {
					risks = append(risks, strategy.PlaintextRisk)
				}
				attempts = append(attempts, strategy.Attempt{Tier: strategy.Direct, Direction: direction, Elevated: elevated, Method: strategy.NcatTar, Risk: ncatRisk, Risks: risks})
			}
		}
	}
	return attempts
}

func (a *App) runAgentMethod(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, direction strategy.Direction, elevated bool, sudoPassword string, socks *connector.SOCKS5, prefix []connector.Hop, method strategy.Method) (resultErr error) {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("远端发起 SCP 仅用于两个 Linux SSH 端点")
	}
	operation = fileOperation(ctx, operation)
	elevatedSource := needsElevation(ctx, strategy.SourcePush, elevated)
	elevatedTarget := needsElevation(ctx, strategy.TargetPull, elevated)
	initiator, other, architecture := source, target, preflight.SourceCapabilities.Architecture
	action, remoteSource, remoteTarget := string(method)+"-upload", operation.SourcePath, operation.TargetPath+".dragfm-partial-"+randomTransferToken(8)
	if direction == strategy.TargetPull {
		initiator, other, architecture = target, source, preflight.TargetCapabilities.Architecture
		action = string(method) + "-download"
	}
	host, exists := a.settingsFor(ctx).document.HostByName(other.Name())
	if !exists {
		return fmt.Errorf("未找到端点 %q 的路由", other.Name())
	}
	route, err := a.peerTransferRoute(ctx, host, socks, prefix)
	if err != nil {
		return err
	}
	peerElevated := (direction == strategy.SourcePush && elevatedTarget) || (direction == strategy.TargetPull && elevatedSource)
	routePayload, err := a.encodeTransferRoute(ctx, route, host, peerElevated)
	if err != nil {
		return err
	}
	initiatorElevated := elevatedSource
	if direction == strategy.TargetPull {
		initiatorElevated = elevatedTarget
	}
	agent, cleanupAgent, err := a.startTransferAgent(ctx, initiator, architecture, initiatorElevated, sudoPassword)
	if err != nil {
		return err
	}
	var receiver *remoteagent.Session
	var cleanupReceiver func() error
	defer func() {
		finishAgentCleanup(&resultErr, cleanupAgent)
		if cleanupReceiver != nil {
			finishAgentCleanup(&resultErr, cleanupReceiver)
		}
	}()
	if direction == strategy.SourcePush {
		receiver, cleanupReceiver, err = a.startTransferAgent(ctx, target, preflight.TargetCapabilities.Architecture, elevatedTarget, "")
		if err != nil {
			return err
		}
		if _, err = receiver.CallContext(ctx, "track-partial", map[string]string{"path": remoteTarget}, nil); err != nil {
			return err
		}
	}
	targetHelper := agent
	if direction == strategy.SourcePush {
		targetHelper = receiver
	}
	before, err := snapshotForAgentAttempt(ctx, &operation, elevated && direction == strategy.SourcePush, agent)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if method == strategy.SCP && (item.Mode&fs.ModeSymlink != 0 || strings.ContainsAny(item.Relative+operation.SourcePath+operation.TargetPath, "\r\n")) {
			return errors.New("SCP 无法安全保留链接或换行文件名，继续其他方法")
		}
	}
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); !elevatedTarget && (err == nil || !errors.Is(err, fs.ErrNotExist)) {
		return errors.New("目标已存在，远端 SCP 原子根路径跳过并改用流式合并")
	}
	peerHelper := ""
	if receiver != nil {
		peerHelper = target.Join(receiver.Directory, "dragfm-agent")
	}
	if method == strategy.Rsync {
		if before.Items[0].Mode.IsDir() {
			// The random destination is this directory's replacement root,
			// not a container for another source-basename level.
			remoteSource = strings.TrimRight(remoteSource, "/") + "/"
		}
		err = agent.RsyncContext(ctx, action, remoteSource, remoteTarget, routePayload, peerHelper)
	} else {
		err = agent.SCPContext(ctx, action, remoteSource, remoteTarget, routePayload, before.Items[0].Mode.IsDir(), peerHelper)
	}
	if err != nil {
		// Only the destination helper's exclusive lease can authorize
		// cleanup. A failed SSH stream may leave a peer writer alive.
		return err
	}
	if elevatedTarget {
		err = targetHelper.Commit(remoteTarget, operation.TargetPath, operation.Overwrite)
	} else {
		err = operation.Destination.Rename(ctx, remoteTarget, operation.TargetPath, false)
	}
	if err != nil {
		return err
	}
	if !elevatedTarget {
		if _, err := targetHelper.CallContext(ctx, "forget-partial", map[string]string{"path": remoteTarget}, nil); err != nil {
			return transfer.PreserveSource(fmt.Errorf("目标已提交，但暂存记录销账失败: %w", err))
		}
	}
	if elevatedTarget {
		return finishAcceleratedMoveWithAgentTarget(ctx, operation, before, "远端 "+string(method), targetHelper)
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

func (a *App) runSOCKSPool(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, sudoPasswords map[strategy.Direction]string) (resultErr error) {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("SOCKS 池仅用于两个 SSH 端点")
	}
	configured := a.settingsFor(ctx).document.SOCKS
	if len(configured) == 0 {
		return errors.New("SOCKS 池为空")
	}
	var failures []error
	sourceAgent, cleanupSource, err := a.openPoolProber(ctx, source, preflight.SourceCapabilities.Architecture)
	if err != nil {
		if !transfer.Retryable(err) {
			return err
		}
		failures = append(failures, fmt.Errorf("源端 SOCKS 探测通道: %w", err))
	} else {
		defer finishAgentCleanup(&resultErr, cleanupSource)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	targetAgent, cleanupTarget, err := a.openPoolProber(ctx, target, preflight.TargetCapabilities.Architecture)
	if err != nil {
		if !transfer.Retryable(err) {
			return err
		}
		failures = append(failures, fmt.Errorf("目标端 SOCKS 探测通道: %w", err))
	} else {
		defer finishAgentCleanup(&resultErr, cleanupTarget)
	}
	if sourceAgent == nil && targetAgent == nil {
		return errors.Join(failures...)
	}
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
		if sourceAgent != nil {
			probe.sourceRTT, err = probeTCPMedian(sourceAgent, parsed.Address)
			probe.sourceOK = err == nil
			if err != nil {
				failures = append(failures, fmt.Errorf("%s 源端 SOCKS 探测: %w", candidate.Name, err))
			}
		}
		if targetAgent != nil {
			probe.targetRTT, err = probeTCPMedian(targetAgent, parsed.Address)
			probe.targetOK = err == nil
			if err != nil {
				failures = append(failures, fmt.Errorf("%s 目标端 SOCKS 探测: %w", candidate.Name, err))
			}
		}
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
				for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream, strategy.NcatTar} {
					descriptor := strategy.Attempt{Tier: strategy.SOCKSPool, RouteName: candidate.config.Name, Direction: direction, Elevated: elevated, Method: method}
					runErr := strategy.Observe(ctx, descriptor, func(ctx context.Context) error {
						return a.runPooledMethod(ctx, operation, preflight, direction, elevated, sudoPasswords[direction], &proxy, nil, method)
					})
					if runErr != nil {
						if !transfer.Retryable(runErr) {
							return runErr
						}
						failures = append(failures, fmt.Errorf("%s/%s/%s/elevated=%t: %w", candidate.config.Name, direction, method, elevated, runErr))
						continue
					}
					a.rememberSOCKS(ctx, candidate.config.ID, rtt)
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

func (a *App) runJumpPool(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, sudoPasswords map[strategy.Direction]string) (resultErr error) {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("SSH 会话池仅用于两个 SSH 端点")
	}
	settings := a.settingsFor(ctx)
	document := settings.document
	sourceHost, sourceFound := document.HostByName(source.Name())
	targetHost, targetFound := document.HostByName(target.Name())
	if !sourceFound || !targetFound {
		return errors.New("端点主机配置不存在")
	}
	cached := cachedRelay(document, sourceHost.ID, target.Name())
	ids := make([]string, 0, len(settings.sessions))
	for id := range settings.sessions {
		if id != sourceHost.ID && id != targetHost.ID && id != cached {
			ids = append(ids, id)
		}
	}
	makeCandidate := func(id string) (probedRelay, bool) {
		host := document.HostByID(id)
		if host == nil || host.Disabled || host.NoRelay {
			return probedRelay{}, false
		}
		route, err := a.routeForHostContext(ctx, *host)
		if err != nil || len(route.Hops) == 0 {
			return probedRelay{}, false
		}
		return probedRelay{host: *host, hops: route.Hops}, true
	}
	var failures []error
	if cached != "" {
		if candidate, ok := makeCandidate(cached); ok {
			candidate.sourceOK, candidate.targetOK = true, true
			activity.Report(ctx, "jump-cache-reuse", 0)
			err := a.runRelayCandidate(ctx, operation, preflight, sudoPasswords, candidate)
			if err == nil {
				a.rememberRelayContext(ctx, sourceHost.ID, target.Name(), cached)
				return nil
			}
			if !transfer.Retryable(err) {
				return err
			}
			failures = append(failures, err)
		}
		a.forgetRelayContext(ctx, sourceHost.ID, target.Name())
	}
	if len(ids) == 0 {
		return errors.Join(errors.New("没有其他已成功登录的 SSH 跳板会话"), errors.Join(failures...))
	}
	sourceAgent, cleanupSource, err := a.openPoolProber(ctx, source, preflight.SourceCapabilities.Architecture)
	if err != nil {
		if !transfer.Retryable(err) {
			return err
		}
		failures = append(failures, fmt.Errorf("源端 SSH 跳板探测通道: %w", err))
	} else {
		defer finishAgentCleanup(&resultErr, cleanupSource)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	targetAgent, cleanupTarget, err := a.openPoolProber(ctx, target, preflight.TargetCapabilities.Architecture)
	if err != nil {
		if !transfer.Retryable(err) {
			return err
		}
		failures = append(failures, fmt.Errorf("目标端 SSH 跳板探测通道: %w", err))
	} else {
		defer finishAgentCleanup(&resultErr, cleanupTarget)
	}
	if sourceAgent == nil && targetAgent == nil {
		return errors.Join(failures...)
	}
	sort.Strings(ids)
	var candidates []probedRelay
	for _, id := range ids {
		probe, ok := makeCandidate(id)
		if !ok {
			continue
		}
		first := net.JoinHostPort(probe.hops[0].Host, fmt.Sprint(probe.hops[0].Port))
		activity.Report(ctx, "jump-probe", 0)
		if sourceAgent != nil {
			probe.sourceRTT, err = probeTCPMedian(sourceAgent, first)
			probe.sourceOK = err == nil
			if err != nil {
				failures = append(failures, fmt.Errorf("%s 源端跳板探测: %w", probe.host.Name, err))
			}
		}
		if targetAgent != nil {
			probe.targetRTT, err = probeTCPMedian(targetAgent, first)
			probe.targetOK = err == nil
			if err != nil {
				failures = append(failures, fmt.Errorf("%s 目标端跳板探测: %w", probe.host.Name, err))
			}
		}
		if probe.sourceOK || probe.targetOK {
			candidates = append(candidates, probe)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return bestRelayRTT(candidates[i]) < bestRelayRTT(candidates[j]) })
	for _, candidate := range candidates {
		err := a.runRelayCandidate(ctx, operation, preflight, sudoPasswords, candidate)
		if err == nil {
			a.rememberRelayContext(ctx, sourceHost.ID, target.Name(), candidate.host.ID)
			return nil
		}
		if !transfer.Retryable(err) {
			return err
		}
		failures = append(failures, err)
	}
	return errors.Join(errors.New("没有可从实际发起端访问的 SSH 跳板"), errors.Join(failures...))
}

func (a *App) runRelayCandidate(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, sudoPasswords map[strategy.Direction]string, candidate probedRelay) error {
	var failures []error
	for _, elevated := range []bool{false, true} {
		for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
			if direction == strategy.SourcePush && !candidate.sourceOK || direction == strategy.TargetPull && !candidate.targetOK {
				continue
			}
			for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream, strategy.NcatTar} {
				descriptor := strategy.Attempt{Tier: strategy.JumpPool, RouteName: candidate.host.Name, Direction: direction, Elevated: elevated, Method: method}
				runErr := strategy.Observe(ctx, descriptor, func(ctx context.Context) error {
					return a.runPooledMethod(ctx, operation, preflight, direction, elevated, sudoPasswords[direction], nil, candidate.hops, method)
				})
				if runErr != nil {
					if !transfer.Retryable(runErr) {
						return runErr
					}
					failures = append(failures, fmt.Errorf("%s/%s/%s/elevated=%t: %w", candidate.host.Name, direction, method, elevated, runErr))
					continue
				}
				return nil
			}
		}
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

func probeTCPMedian(prober tcpProber, address string) (time.Duration, error) {
	samples := make([]time.Duration, 0, 3)
	var failures []error
	for attempt := 0; attempt < 3; attempt++ {
		if elapsed, err := prober.ProbeTCP(address); err == nil {
			samples = append(samples, elapsed)
		} else {
			failures = append(failures, err)
		}
	}
	if len(samples) < 2 {
		return 0, errors.Join(failures...)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2], nil
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
	return a.runHansRoleMethods(ctx, operation, preflight, server, client, serverArchitecture, clientArchitecture, direction, serverSudoPassword, clientSudoPassword, []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream, strategy.NcatTar})
}

func (a *App) runHansRoleMethods(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, server, client *endpoint.Remote, serverArchitecture, clientArchitecture string, direction strategy.Direction, serverSudoPassword, clientSudoPassword string, methods []strategy.Method) (resultErr error) {
	serverRoot, err := remoteIsRoot(ctx, server)
	if err != nil {
		return err
	}
	serverAgent, cleanupServerAgent, err := a.startTransferAgent(ctx, server, serverArchitecture, !serverRoot, serverSudoPassword)
	if err != nil {
		return fmt.Errorf("启动 Hans server agent: %w", err)
	}
	defer finishAgentCleanup(&resultErr, cleanupServerAgent)
	clientAgent, cleanupClientAgent, err := a.startTransferAgent(ctx, client, clientArchitecture, false, "")
	if err != nil {
		return fmt.Errorf("启动 Hans client agent: %w", err)
	}
	defer finishAgentCleanup(&resultErr, cleanupClientAgent)
	serverBinary, err := remoteagent.InstallHans(ctx, serverAgent, serverArchitecture)
	if err != nil {
		return err
	}
	clientBinary, err := remoteagent.InstallHans(ctx, clientAgent, clientArchitecture)
	if err != nil {
		return err
	}
	serverHost, hostExists := a.settingsFor(ctx).document.HostByName(server.Name())
	if !hostExists {
		return errors.New("Hans server configuration is missing")
	}
	serverRoute, err := a.routeForHostContext(ctx, serverHost)
	if err != nil {
		return err
	}
	if len(serverRoute.Hops) == 0 {
		return errors.New("Hans server SSH route is empty")
	}
	serverPort := serverRoute.Hops[len(serverRoute.Hops)-1].Port
	if serverPort == 0 {
		serverPort = 22
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
		if err := waitForHansSOCKS(ctx, clientAgent, socksAddress, net.JoinHostPort(serverTunnelIP, fmt.Sprint(serverPort)), serverAgent, serverJob, clientJob); err != nil {
			if stopErr := clientAgent.StopProcess(clientJob); stopErr != nil {
				return errors.Join(err, transfer.PreserveSource(fmt.Errorf("Hans 客户端退出未确认: %w", stopErr)))
			}
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
		var cleanupTransferAgent func() error
		if elevated {
			transferAgent, cleanupTransferAgent, err = a.startTransferAgent(ctx, client, clientArchitecture, true, clientSudoPassword)
			if err != nil {
				transferFailures = append(transferFailures, fmt.Errorf("elevated client: %w", err))
				continue
			}
		}
		for _, method := range methods {
			descriptor := strategy.Attempt{Tier: strategy.Hans, RouteName: "server=" + server.Name(), Direction: direction, Elevated: elevated, Method: method}
			err = strategy.Observe(ctx, descriptor, func(ctx context.Context) error {
				if method == strategy.EncryptedStream || method == strategy.NcatTar {
					return a.runAgentStreamWithCarrier(ctx, operation, preflight, direction, elevated, clientSudoPassword, &proxy, nil, serverTunnelIP, carrierForMethod(method))
				}
				return a.runHansMethod(ctx, operation, transferAgent, serverAgent, server, direction, elevated, serverTunnelIP, socksAddress, method)
			})
			if err != nil {
				if !transfer.Retryable(err) {
					if cleanupTransferAgent != nil {
						finishAgentCleanup(&err, cleanupTransferAgent)
					}
					return err
				}
				transferFailures = append(transferFailures, fmt.Errorf("%s/elevated=%t: %w", method, elevated, err))
				continue
			}
			if cleanupTransferAgent != nil {
				finishAgentCleanup(&err, cleanupTransferAgent)
			}
			return err
		}
		if cleanupTransferAgent != nil {
			var cleanupErr error
			finishAgentCleanup(&cleanupErr, cleanupTransferAgent)
			if cleanupErr != nil {
				return errors.Join(errors.Join(transferFailures...), cleanupErr)
			}
		}
	}
	return errors.Join(transferFailures...)
}

func (a *App) runHansMethod(ctx context.Context, operation transfer.Operation, agent, serverAgent *remoteagent.Session, server *endpoint.Remote, direction strategy.Direction, elevated bool, tunnelHost, socksAddress string, method strategy.Method) error {
	if elevated {
		if err := strategy.Authorize(ctx, strategy.Attempt{Direction: direction, Elevated: true, Method: method}, strategy.SourceSudoRisk, strategy.TargetSudoRisk); err != nil {
			return err
		}
	}
	operation = fileOperation(ctx, operation)
	before, err := snapshotForAgentAttempt(ctx, &operation, elevated && direction == strategy.SourcePush, agent)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if method == strategy.SCP && (item.Mode&fs.ModeSymlink != 0 || strings.ContainsAny(item.Relative+operation.SourcePath+operation.TargetPath, "\r\n")) {
			return errors.New("Hans SCP 无法安全保留链接或换行文件名，继续其他方法")
		}
	}
	elevatedTarget := needsElevation(ctx, strategy.TargetPull, elevated)
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); !elevatedTarget && (err == nil || !errors.Is(err, fs.ErrNotExist)) {
		return errors.New("Hans SCP 要求目标根路径不存在")
	}
	host, ok := a.settingsFor(ctx).document.HostByName(server.Name())
	if !ok {
		return errors.New("Hans server 主机配置不存在")
	}
	route, err := a.routeForHostContext(ctx, host)
	if err != nil {
		return err
	}
	if len(route.Hops) == 0 {
		return errors.New("Hans server SSH 路由为空")
	}
	peerSide := strategy.TargetPull
	if direction == strategy.TargetPull {
		peerSide = strategy.SourcePush
	}
	peerElevated := needsElevation(ctx, peerSide, elevated)
	final := route.Hops[len(route.Hops)-1]
	final.Host = tunnelHost
	proxy, err := routespec.ParseSOCKS(socksAddress)
	if err != nil {
		return err
	}
	route = connector.Route{Hops: []connector.Hop{final}, Timeout: 15 * time.Second, SOCKS: &proxy}
	payload, err := a.encodeTransferRoute(ctx, route, host, peerElevated)
	if err != nil {
		return err
	}
	partial := operation.TargetPath + ".dragfm-partial-" + randomTransferToken(8)
	action := string(method) + "-upload"
	targetHelper := serverAgent
	peerHelper := server.Join(serverAgent.Directory, "dragfm-agent")
	if direction == strategy.TargetPull {
		action = string(method) + "-download"
		targetHelper = agent
		peerHelper = "" // Local writer is covered by this agent's lease.
	} else {
		if _, err := targetHelper.CallContext(ctx, "track-partial", map[string]string{"path": partial}, nil); err != nil {
			return err
		}
	}
	if method == strategy.Rsync {
		sourcePath := operation.SourcePath
		if before.Items[0].Mode.IsDir() {
			sourcePath = strings.TrimRight(sourcePath, "/") + "/"
		}
		err = agent.RsyncContext(ctx, action, sourcePath, partial, payload, peerHelper)
	} else {
		err = agent.SCPContext(ctx, action, operation.SourcePath, partial, payload, before.Items[0].Mode.IsDir(), peerHelper)
	}
	if err != nil {
		return err
	}
	if elevatedTarget {
		err = targetHelper.Commit(partial, operation.TargetPath, operation.Overwrite)
	} else {
		err = operation.Destination.Rename(ctx, partial, operation.TargetPath, false)
	}
	if err != nil {
		return err
	}
	if !elevatedTarget {
		if _, err := targetHelper.CallContext(ctx, "forget-partial", map[string]string{"path": partial}, nil); err != nil {
			return transfer.PreserveSource(fmt.Errorf("目标已提交，但暂存记录销账失败: %w", err))
		}
	}
	if elevatedTarget {
		return finishAcceleratedMoveWithAgentTarget(ctx, operation, before, "Hans v5 "+string(method), targetHelper)
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

// Require an actual TCP CONNECT through the authenticated v5 tunnel,
// not merely a listening SOCKS socket. Retain bounded redacted diagnostics.
func waitForHansSOCKS(ctx context.Context, client *remoteagent.Session, proxyAddress, targetAddress string, server *remoteagent.Session, serverJob, clientJob string) error {
	var last error
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := server.ProcessStatus(ctx, serverJob); err != nil {
			return fmt.Errorf("Hans server: %w", err)
		}
		if err := client.ProcessStatus(ctx, clientJob); err != nil {
			return fmt.Errorf("Hans client: %w", err)
		}
		if err := client.ProbeSOCKSTCP(ctx, proxyAddress, targetAddress); err == nil {
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
	serverLog, _ := server.ProcessDiagnostics(ctx, serverJob)
	clientLog, _ := client.ProcessDiagnostics(ctx, clientJob)
	return fmt.Errorf("Hans tunnel TCP readiness failed: %w; server: %s; client: %s", last, serverLog, clientLog)
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

func (a *App) rememberSOCKS(ctx context.Context, id string, rtt time.Duration) {
	a.mu.Lock()
	if !a.settingsCurrentLocked(ctx) {
		a.mu.Unlock()
		return
	}
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
			answer, accepted := a.ask(ctx, ChallengeModel{Kind: "confirm", Title: "允许临时传输监听", AllowSkip: true, Message: "本任务可在指定接口的随机高端口监听。加密流使用 TLS 1.3、证书固定和随机令牌；独立 tar+ncat 是明文，仅私网自动尝试，其他地址另行确认，归档在 SSH 校验后才解包。监听允许至多五个随机端口重试，任务结束或取消后关闭。"})
			if !accepted {
				return declinedRisk(answer)
			}
			return nil
		}
		if risk == strategy.PlaintextRisk {
			answer, accepted := a.ask(ctx, ChallengeModel{Kind: "confirm", Title: "允许明文 tar+ncat", AllowSkip: true, Message: "无法确认两端监听地址均为私网。继续会在随机端口用未加密的 tar+ncat 传输；文件内容可能被链路观察或篡改。建议仅在可信隔离网络中使用。"})
			if !accepted {
				return declinedRisk(answer)
			}
			return nil
		}
		if risk == strategy.HansRisk {
			answer, accepted := a.ask(ctx, ChallengeModel{Kind: "confirm", Title: "允许 Hans v5 ICMP 隧道", AllowSkip: true, Message: "将选择一端以 root/sudo 启动临时 Hans server（会创建临时 TUN/veth 与 ICMP 状态），另一端启动无需 TUN 的 userspace SOCKS client。使用随机网段、随机强口令、独立临时身份、服务端指纹固定和 --require-v5；任务结束或取消后停止进程并清理所属临时目录。"})
			if !accepted {
				return declinedRisk(answer)
			}
			return nil
		}
		if risk == strategy.SourceSudoRisk || risk == strategy.TargetSudoRisk {
			direction := strategy.SourcePush
			if risk == strategy.TargetSudoRisk {
				direction = strategy.TargetPull
			}
			remote := remoteForElevatedAttempt(operation, direction)
			title := "源端提权"
			if direction == strategy.TargetPull {
				title = "目标端提权"
			}
			if remote == nil {
				return errors.New("提权端点不是 SSH 主机")
			}
			stored := a.savedSudoPassword(ctx, remote.Name())
			protectedPath := operation.SourcePath
			if direction == strategy.TargetPull {
				protectedPath = operation.TargetPath
			}
			message := "本次传输路径：\n" + protectedPath + "\n将使用已配置的高权 SSH 身份或 sudo 执行本任务的文件操作，优先临时 agent，不可用时尝试系统 OpenSSH 文件通道。留空会先用保险库中已保存的 sudo 密码，否则尝试 sudo -n；不会打开交互式 root Shell。勾选保存后，也仅在本次 sudo 确实使用并验证该密码后保存。"
			if direction == strategy.TargetPull {
				message += "目标将按源端数字 UID/GID 保留可获取的所有权，完成后普通用户可能无法读取。"
			}
			answer, accepted := a.ask(ctx, ChallengeModel{Kind: "password", Title: title + " · " + remote.Name(), Message: message, Secret: true, AllowSave: true, AllowSkip: true})
			if !accepted {
				return declinedRisk(answer)
			}
			password := answer.Value
			if password == "" {
				password = stored
			}
			remoteSudo[direction] = password
			if answer.Save && answer.Value != "" {
				if err := offerSudoPassword(ctx, remote.Name(), answer.Value); err != nil {
					return err
				}
			}
			return nil
		}
		if risk != strategy.SudoRisk {
			return nil
		}
		answer, accepted := a.ask(ctx, ChallengeModel{
			Kind:      "password",
			Title:     "目标目录需要管理员权限",
			Message:   fmt.Sprintf("当前用户不能写入：\n%s\n\n程序将仅对这次传输使用 sudo，在目标目录创建随机 .dragfm-partial 文件，完成后原子改名。密码仅通过 stdin 传给 sudo；留空会尝试已有的 sudo 授权。", protectedDirectory),
			Secret:    true,
			AllowSkip: true,
		})
		if !accepted {
			return declinedRisk(answer)
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

func (a *App) savedSudoPassword(ctx context.Context, name string) string {
	host, ok := a.settingsFor(ctx).document.HostByName(name)
	if !ok {
		return ""
	}
	return host.SudoPassword
}

func (a *App) saveSudoPassword(ctx context.Context, name, password string) error {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store == nil || !a.settingsCurrentLocked(ctx) {
		return errors.New("主机配置已经改变；未保存本次 sudo 密码")
	}
	next := a.document.Clone()
	for index := range next.Hosts {
		if next.Hosts[index].Name == name {
			next.Hosts[index].SudoPassword = password
			if err := a.store.Save(a.password, next); err != nil {
				return err
			}
			a.document = next
			return nil
		}
	}
	return errors.New("sudo 密码对应的主机配置不存在")
}

// startTransferAgent uses an explicitly configured root SSH identity only
// inside an already-approved elevated attempt. It never adds root routes to
// browsing, capability probes, SOCKS sorting, or relay discovery. If root SSH
// is unavailable, the same attempt falls back to the scoped sudo helper.
func (a *App) startTransferAgent(ctx context.Context, remote *endpoint.Remote, architecture string, elevated bool, sudoPassword string) (*remoteagent.Session, func() error, error) {
	if access := accessFor(ctx); access != nil && elevated {
		side := strategy.SourcePush
		if remote == access.original.Destination {
			side = strategy.TargetPull
		}
		if err := strategy.Authorize(ctx, strategy.Attempt{Direction: side, Elevated: true}, sideRisk(side)); err != nil {
			return nil, nil, err
		}
		sudoPassword = access.passwords[side]
	}
	start := func(owned *endpoint.Remote, sudo bool) (*remoteagent.Session, error) {
		journal, err := a.taskWorkspaceJournal(ctx, remote, owned, elevated)
		if err != nil {
			return nil, err
		}
		if sudo {
			return remoteagent.StartElevated(ctx, owned, architecture, sudoPassword, journal)
		}
		return remoteagent.Start(ctx, owned, architecture, journal)
	}
	if !elevated {
		owned, err := remote.Fork(ctx)
		if err != nil {
			return nil, nil, err
		}
		agent, err := start(owned, false)
		if err != nil {
			_ = owned.Close()
			return nil, nil, err
		}
		return agent, func() error { return errors.Join(agent.Close(), owned.Close()) }, nil
	}

	var rootErr error
	host, configured := a.settingsFor(ctx).document.HostByName(remote.Name())
	if configured && (host.RootUser != "" || len(host.RootKeyIDs) > 0) {
		route, err := a.routeForHostContext(ctx, host)
		if err == nil {
			route, err = a.rootRouteForHost(ctx, route, host)
		}
		if err == nil {
			final := &route.Hops[len(route.Hops)-1]
			rootRemote, dialErr := endpoint.DialSSH(ctx, remote.Name(), final.HostKey.PinnedSHA256, route)
			if dialErr == nil {
				agent, startErr := start(rootRemote, false)
				if startErr == nil {
					return agent, func() error {
						return errors.Join(agent.Close(), rootRemote.Close())
					}, nil
				}
				_ = rootRemote.Close()
				if !transfer.Retryable(startErr) {
					return nil, nil, startErr
				}
				rootErr = fmt.Errorf("root SSH helper: %w", startErr)
			} else {
				rootErr = fmt.Errorf("root SSH: %w", dialErr)
			}
		} else if err != nil {
			rootErr = fmt.Errorf("root SSH route: %w", err)
		}
	}
	owned, dialErr := remote.Fork(ctx)
	if dialErr != nil {
		return nil, nil, errors.Join(rootErr, dialErr)
	}
	agent, sudoErr := start(owned, true)
	if sudoErr != nil {
		_ = owned.Close()
		return nil, nil, errors.Join(rootErr, fmt.Errorf("scoped sudo helper: %w", sudoErr))
	}
	if agent.SudoAuthenticated() {
		if err := a.persistAuthenticatedSudo(ctx, remote.Name(), sudoPassword); err != nil {
			failure := fmt.Errorf("sudo 已验证，但保存密码失败: %w", err)
			finishAgentCleanup(&failure, func() error { return errors.Join(agent.Close(), owned.Close()) })
			return nil, nil, failure
		}
	}
	return agent, func() error { return errors.Join(agent.Close(), owned.Close()) }, nil
}

// Cleanup uncertainty is a terminal task error, not a reason to start another
// writer through a different strategy. Queue/history keep the original cause.
func finishAgentCleanup(result *error, cleanup func() error) {
	if err := cleanup(); err != nil {
		*result = errors.Join(*result, transfer.PreserveSource(fmt.Errorf("远端代理清理未确认，停止后续传输尝试: %w", err)))
	}
}

// Resolve from the same frozen settings as the ordinary route. Editing/removing
// a vault key while this task waits must not replace its admitted credentials.
func (a *App) rootRouteForHost(ctx context.Context, route connector.Route, host config.Host) (connector.Route, error) {
	document := a.settingsFor(ctx).document
	keys := make([]connector.PrivateKey, 0, len(host.RootKeyIDs))
	for _, id := range host.RootKeyIDs {
		key := document.KeyByID(id)
		if key == nil {
			return connector.Route{}, fmt.Errorf("主机 %q 的 root 私钥引用不存在", host.Name)
		}
		keys = append(keys, connector.PrivateKey{PEM: []byte(key.PEM), Passphrase: []byte(key.Passphrase)})
	}
	user := host.RootUser
	if user == "" && len(keys) > 0 {
		user = "root"
	}
	return configuredRootRoute(route, user, host.RootPassword, keys...)
}

func configuredRootRoute(route connector.Route, user, password string, keys ...connector.PrivateKey) (connector.Route, error) {
	if user == "" || len(route.Hops) == 0 {
		return connector.Route{}, errors.New("root SSH route is incomplete")
	}
	result := route
	result.Hops = append([]connector.Hop(nil), route.Hops...)
	final := &result.Hops[len(result.Hops)-1]
	if final.User != user {
		if password == "" && len(keys) == 0 {
			return connector.Route{}, errors.New("高权 SSH 账户不同且未配置该账户的凭据；改试获准的 sudo")
		}
		// A credential bound to the ordinary user is not authorization to
		// try that private key (or every controller agent key) as root.
		final.Credentials = connector.Credentials{Password: password}
	} else if password != "" {
		final.Credentials.Password = password
	}
	if len(keys) > 0 {
		// Explicit selection means only those keys, even when the account name
		// is unchanged. Never append unrelated ordinary/agent keys.
		final.Credentials.UseAgent = false
		final.Credentials.PrivateKeys = append([]connector.PrivateKey(nil), keys...)
	}
	final.User = user
	return result, nil
}

// Only remote transfer launchers consume this payload. Controller root Dial
// continues to require explicit account-bound credentials via rootRouteForHost.
func (a *App) encodeTransferRoute(ctx context.Context, route connector.Route, host config.Host, peerElevated bool) (string, error) {
	if !peerElevated {
		return agentroute.Encode(route)
	}
	if len(route.Hops) == 0 {
		return "", errors.New("高权传输的对端 SSH 路由为空")
	}
	if host.RootUser == "" {
		host.RootUser = "root"
	}
	if route.Hops[len(route.Hops)-1].User == host.RootUser || host.RootPassword != "" || len(host.RootKeyIDs) != 0 {
		configured, err := a.rootRouteForHost(ctx, route, host)
		if err != nil {
			return "", err // Missing explicit keys never enable implicit identities.
		}
		return agentroute.Encode(configured)
	}
	return agentroute.EncodeRootInitiator(route, host.RootUser)
}

func remoteRemotePair(operation transfer.Operation) (*endpoint.Remote, *endpoint.Remote, bool) {
	source, sourceOK := operation.Source.(*endpoint.Remote)
	target, targetOK := operation.Destination.(*endpoint.Remote)
	return source, target, sourceOK && targetOK
}

func (a *App) runAgentStream(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, direction strategy.Direction, elevated bool, sudoPassword string, socksProxy *connector.SOCKS5, prefix []connector.Hop, preferredListenerAddress string) error {
	return a.runAgentStreamWithCarrier(ctx, operation, preflight, direction, elevated, sudoPassword, socksProxy, prefix, preferredListenerAddress, "")
}

func carrierForMethod(method strategy.Method) string {
	if method == strategy.NcatTar {
		return "ncat"
	}
	return ""
}

func (a *App) runAgentStreamWithCarrier(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, direction strategy.Direction, elevated bool, sudoPassword string, socksProxy *connector.SOCKS5, prefix []connector.Hop, preferredListenerAddress, carrier string) (resultErr error) {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("加密直连流仅用于两个 Linux SSH 端点")
	}
	if accessFor(ctx) != nil {
		if err := strategy.Authorize(ctx, strategy.Attempt{Direction: direction, Elevated: elevated}, strategy.ListenRisk); err != nil {
			return err
		}
	}
	operation = fileOperation(ctx, operation)
	elevatedSource := needsElevation(ctx, strategy.SourcePush, elevated && direction == strategy.SourcePush)
	elevatedTarget := needsElevation(ctx, strategy.TargetPull, elevated && direction == strategy.TargetPull)
	sourceAgent, cleanupSourceAgent, err := a.startTransferAgent(ctx, source, preflight.SourceCapabilities.Architecture, elevatedSource, sudoPassword)
	if err != nil {
		return fmt.Errorf("启动源端 agent: %w", err)
	}
	defer finishAgentCleanup(&resultErr, cleanupSourceAgent)
	targetAgent, cleanupTargetAgent, err := a.startTransferAgent(ctx, target, preflight.TargetCapabilities.Architecture, elevatedTarget, sudoPassword)
	if err != nil {
		return fmt.Errorf("启动目标端 agent: %w", err)
	}
	defer finishAgentCleanup(&resultErr, cleanupTargetAgent)
	before, err := snapshotForAgentAttempt(ctx, &operation, elevated && direction == strategy.SourcePush, sourceAgent)
	if err != nil {
		return err
	}
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); !elevatedTarget && (err == nil || !errors.Is(err, fs.ErrNotExist)) {
		return errors.New("目标已存在，加密流原子根路径跳过并改用流式合并")
	}
	partial := operation.TargetPath + ".dragfm-partial-" + randomTransferToken(8)
	routePayload := ""
	if len(prefix) > 0 {
		other := target
		if direction == strategy.TargetPull {
			other = source
		}
		host, exists := a.settingsFor(ctx).document.HostByName(other.Name())
		if !exists {
			return fmt.Errorf("未找到端点 %q 的路由", other.Name())
		}
		route, routeErr := a.peerTransferRoute(ctx, host, nil, prefix)
		if routeErr != nil {
			return routeErr
		}
		routePayload, err = agentroute.Encode(route)
		if err != nil {
			return err
		}
	}
	waiter, connector := targetAgent, sourceAgent
	listenAction, connectAction := "listen-receive", "connect-send"
	listenPath, connectPath, advertised := partial, operation.SourcePath, target.ConnectionHost()
	if direction == strategy.TargetPull {
		waiter, connector = sourceAgent, targetAgent
		listenAction, connectAction = "listen-send", "connect-receive"
		listenPath, connectPath, advertised = operation.SourcePath, partial, source.ConnectionHost()
	}
	// Bind failures and actual connection/firewall failures consume the same
	// five distinct port candidates. Every address gets a fresh one-use listener
	// and token, since a failed TLS handshake may already have consumed Accept.
	var addresses []string
	var failures []error
	connected := false
ports:
	for _, port := range randomPorts(5) {
		activity.Report(ctx, "stream-port/"+strconv.Itoa(port), 0)
		for index := 0; ; index++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			job, token := "job-"+randomTransferToken(12), randomTransferToken(32)
			listener, err := waiter.ListenPort(listenAction, job, listenPath, token, elevatedTarget && direction == strategy.SourcePush, preferredListenerAddress, strconv.Itoa(port))
			if err != nil {
				failures = append(failures, fmt.Errorf("端口 %d 监听: %w", port, err))
				continue ports
			}
			if addresses == nil {
				addresses = prependAddress(prependAddress(listener.Addresses, advertised), preferredListenerAddress)
			}
			address := addresses[index]
			connectErr := connector.ConnectWithCarrier(carrier, connectAction, connectPath, net.JoinHostPort(address, listener.Port), token, listener.Pin, socksProxy, routePayload, elevatedTarget)
			if connectErr == nil {
				connectErr = waiter.Wait(job)
			} else if stopErr := waiter.StopListener(job); stopErr != nil {
				// Do not race an old receiver or silently leave a listener running.
				return errors.Join(connectErr, stopErr)
			}
			if connectErr == nil {
				connected = true
				break ports
			}
			failures = append(failures, fmt.Errorf("端口 %d · %s: %w", port, address, connectErr))
			if !transfer.Retryable(connectErr) {
				return errors.Join(failures...)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := targetAgent.DiscardPartial(partial); err != nil {
				return errors.Join(append(failures, fmt.Errorf("重试前清理 partial: %w", err))...)
			}
			if index+1 >= len(addresses) {
				break
			}
		}
	}
	if !connected {
		return errors.Join(failures...)
	}
	if elevatedTarget {
		err = targetAgent.Commit(partial, operation.TargetPath, operation.Overwrite)
	} else {
		err = operation.Destination.Rename(ctx, partial, operation.TargetPath, false)
	}
	if err != nil {
		return err
	}
	if !elevatedTarget {
		if _, err := targetAgent.CallContext(ctx, "forget-partial", map[string]string{"path": partial}, nil); err != nil {
			return transfer.PreserveSource(fmt.Errorf("目标已提交，但暂存记录销账失败: %w", err))
		}
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
	if _, err := transfer.Preflight(ctx, operation); err != nil {
		return err
	}
	return (&App{}).runSystemNcatTar(ctx, operation, direction, false)
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
	operation = fileOperation(ctx, operation)
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return errors.New("目标已存在，rsync 原子根路径跳过并改用流式合并")
	}
	before, err := transfer.SnapshotForOperation(ctx, operation)
	if err != nil {
		return err
	}
	partial := operation.TargetPath + ".dragfm-partial-" + randomTransferToken(16)
	direction := rsyncbridge.Upload
	if _, ok := operation.Source.(*endpoint.Remote); ok {
		direction = rsyncbridge.Download
	}
	owned, err := remote.Fork(ctx)
	if err != nil {
		return err
	}
	defer owned.Close()
	sourcePath := operation.SourcePath
	if before.Items[0].Mode.IsDir() {
		sourcePath = strings.TrimRight(sourcePath, "/") + "/"
	}
	if err := rsyncbridge.Run(ctx, owned.SSHClient(), direction, sourcePath, partial); err != nil {
		return cleanupAcceleratedPartial(operation, partial, before.Items[0].Mode.IsDir(), err)
	}
	if err := operation.Destination.Rename(ctx, partial, operation.TargetPath, false); err != nil {
		return cleanupAcceleratedPartial(operation, partial, before.Items[0].Mode.IsDir(), err)
	}
	return finishAcceleratedMove(ctx, operation, before, "rsync")
}

func finishAcceleratedMove(ctx context.Context, operation transfer.Operation, before transfer.Manifest, method string) error {
	if access := accessFor(ctx); access != nil {
		return access.finishTransfer(ctx, before, needsElevation(ctx, strategy.TargetPull, false))
	}
	if !operation.Move {
		return nil
	}
	if err := transfer.FinishMove(ctx, operation, before); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

func snapshotForAgentAttempt(ctx context.Context, operation *transfer.Operation, elevatedSource bool, sourceAgent *remoteagent.Session) (transfer.Manifest, error) {
	if accessFor(ctx) != nil {
		*operation = fileOperation(ctx, *operation)
		return transfer.SnapshotForOperation(ctx, *operation)
	}
	if elevatedSource {
		files, err := sourceAgent.OpenFiles(ctx)
		if err != nil {
			return transfer.Manifest{}, err
		}
		// Keep the actual approved source view in the caller's operation,
		// including when the target-elevation completion branch wins. Returning
		// only a manifest used to lose this view before the final source hash.
		operation.Source = files
	}
	return transfer.SnapshotForOperation(ctx, *operation)
}

func finishAcceleratedMoveWithAgentTarget(ctx context.Context, operation transfer.Operation, before transfer.Manifest, method string, targetAgent *remoteagent.Session) error {
	if access := accessFor(ctx); access != nil {
		return access.finishTransfer(ctx, before, true)
	}
	if !operation.Move {
		return nil
	}
	files, err := targetAgent.OpenFiles(ctx)
	if err != nil {
		return transfer.PreserveSource(fmt.Errorf("%s 后提权读取目标失败，源已保留: %w", method, err))
	}
	operation.Destination = files
	return finishAcceleratedMove(ctx, operation, before, method)
}

func finishAcceleratedMoveWithAgentSource(ctx context.Context, operation transfer.Operation, before transfer.Manifest, method string, sourceAgent *remoteagent.Session) error {
	if access := accessFor(ctx); access != nil {
		return access.finishTransfer(ctx, before, needsElevation(ctx, strategy.TargetPull, false))
	}
	if !operation.Move {
		return nil
	}
	files, err := sourceAgent.OpenFiles(ctx)
	if err != nil {
		return transfer.PreserveSource(fmt.Errorf("%s 后提权复查源失败，源已保留: %w", method, err))
	}
	operation.Source = files
	return finishAcceleratedMove(ctx, operation, before, method)
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
	operation = fileOperation(ctx, operation)
	if err := ctx.Err(); err != nil {
		return err
	}
	// Overwrite and directory merge semantics are handled by the atomic stream
	// fallback; plain SCP is only used for a new root target.
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return errors.New("目标已存在，SCP 跳过并改用原子流")
	}
	before, err := transfer.SnapshotForOperation(ctx, operation)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if item.Mode&fs.ModeSymlink != 0 || strings.ContainsAny(item.Relative+operation.SourcePath+operation.TargetPath, "\r\n") {
			return errors.New("SCP 无法安全保留链接或换行文件名，改用原子流")
		}
	}
	partial := operation.TargetPath + ".dragfm-partial-" + randomTransferToken(16)
	direction := flytransfer.DirectionUpload
	if _, ok := operation.Source.(*endpoint.Remote); ok {
		direction = flytransfer.DirectionDownload
	}
	flags := []string{"-p"}
	if before.Items[0].Mode.IsDir() {
		flags = append(flags, "-r")
	}
	spec := &flytransfer.Spec{Mode: flytransfer.ModeSCP, Direction: direction, Flags: flags, Sources: []string{operation.SourcePath}, Target: partial}
	owned, err := remote.Fork(ctx)
	if err != nil {
		return err
	}
	defer owned.Close()
	code, err := flytransfer.RunContext(ctx, owned.SSHClient(), spec)
	if err != nil || code != 0 {
		if err == nil {
			err = fmt.Errorf("scp exit code %d", code)
		}
		return cleanupAcceleratedPartial(operation, partial, before.Items[0].Mode.IsDir(), err)
	}
	if err := operation.Destination.Rename(ctx, partial, operation.TargetPath, false); err != nil {
		return cleanupAcceleratedPartial(operation, partial, before.Items[0].Mode.IsDir(), err)
	}
	return finishAcceleratedMove(ctx, operation, before, "SCP")
}

// Non-agent local/SSH accelerators lack a receiver lease. Only confirmed
// failures may clean their exact staging path; cancellation/transport loss
// retain it and report the path for recovery, never hide the underlying error.
func cleanupAcceleratedPartial(operation transfer.Operation, partial string, directory bool, failure error) error {
	if !transfer.Retryable(failure) {
		return transfer.PreserveSource(fmt.Errorf("传输未确认停止，暂存路径已保留 %q: %w", partial, failure))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := operation.Destination.Remove(ctx, partial, directory); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return transfer.PreserveSource(errors.Join(failure, fmt.Errorf("暂存路径清理失败 %q: %w", partial, err)))
	}
	return failure
}
