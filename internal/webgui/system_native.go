package webgui

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/agentroute"
	"github.com/lovitus/dragfm-gui/internal/boundedbuf"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// This adapter only connects native tools' standard streams. SCP/rsync wire
// protocols remain implemented by the installed tools, SSH by Paramiko.
//
//go:embed system_native.py
var systemNativeProgram string

type systemNativeRequest struct {
	Method     strategy.Method `json:"method"`
	Pull       bool            `json:"pull"`
	Source     string          `json:"source"`
	Target     string          `json:"target"`
	Directory  bool            `json:"directory"`
	PeerPrefix []string        `json:"peer_prefix"`
}

func (a *App) runNativeMethod(ctx context.Context, operation transfer.Operation, preflight transfer.PreflightReport, direction strategy.Direction, elevated bool, password string, socks *connector.SOCKS5, prefix []connector.Hop, method strategy.Method) error {
	if elevated {
		if err := strategy.Authorize(ctx, strategy.Attempt{Direction: direction, Elevated: true, Method: method}, strategy.SourceSudoRisk, strategy.TargetSudoRisk); err != nil {
			return err
		}
	}
	err := a.runAgentMethod(ctx, operation, preflight, direction, elevated, password, socks, prefix, method)
	if err == nil || ctx.Err() != nil || !transfer.Retryable(err) {
		return err
	}
	if fallback := a.runSystemNative(ctx, operation, direction, elevated, socks, prefix, method); fallback != nil {
		return errors.Join(err, fmt.Errorf("system %s: %w", method, fallback))
	}
	return nil
}

func (a *App) runSystemNative(ctx context.Context, operation transfer.Operation, direction strategy.Direction, elevated bool, socks *connector.SOCKS5, prefix []connector.Hop, method strategy.Method) (retErr error) {
	source, target, ok := remoteRemotePair(operation)
	if !ok || (method != strategy.Rsync && method != strategy.SCP) || (direction != strategy.SourcePush && direction != strategy.TargetPull) {
		return errors.New("系统 rsync/SCP 需要两个 Linux SSH 端点和明确的传输方向")
	}
	operation = fileOperation(ctx, operation)
	elevatedSource := needsElevation(ctx, strategy.SourcePush, elevated)
	elevatedTarget := needsElevation(ctx, strategy.TargetPull, elevated)
	committed := false
	defer func() {
		if retErr != nil && (committed || errors.Is(retErr, endpoint.ErrCommandExitUnconfirmed)) {
			retErr = transfer.PreserveSource(retErr)
		}
	}()
	send, closeSend, err := a.openNcatEndpoint(ctx, source, strategy.SourcePush, elevatedSource)
	if err != nil {
		return err
	}
	defer func() { retErr = ncatCleanupResult(retErr, closeSend(), committed) }()
	receive, closeReceive, err := a.openNcatEndpoint(ctx, target, strategy.TargetPull, elevatedTarget)
	if err != nil {
		return err
	}
	defer func() { retErr = ncatCleanupResult(retErr, closeReceive(), committed) }()
	operation.Source, operation.Destination, operation.PreserveOwner = send, receive, elevatedTarget
	before, err := transfer.SnapshotForOperation(ctx, operation)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if !item.Mode.IsRegular() && !item.Mode.IsDir() && item.Mode&fs.ModeSymlink == 0 {
			return fmt.Errorf("系统 %s 不接收特殊文件类型 %s", method, item.Mode.Type())
		}
		if method == strategy.SCP && (item.Mode&fs.ModeSymlink != 0 || strings.ContainsAny(item.Relative+operation.SourcePath+operation.TargetPath, "\r\n")) {
			return errors.New("SCP 无法安全保留链接或换行文件名，继续其他方法")
		}
	}
	if _, err := receive.Stat(ctx, operation.TargetPath); err == nil {
		return errors.New("目标已存在，系统 rsync/SCP 跳过整根提交并继续安全合并")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	initiator, other, peerElevated := send, target, elevatedTarget
	if direction == strategy.TargetPull {
		initiator, other, peerElevated = receive, source, elevatedSource
	}
	host, found := a.settingsFor(ctx).document.HostByName(other.Name())
	if !found {
		return errors.New("系统传输对端未包含在任务路由快照中")
	}
	route, err := a.peerTransferRoute(ctx, host, socks, prefix)
	if err != nil {
		return err
	}
	payload, err := a.encodeTransferRoute(ctx, route, host, peerElevated)
	if err != nil {
		return err
	}
	var wire agentroute.Route
	if err := json.Unmarshal([]byte(payload), &wire); err != nil {
		return err
	}
	if err := checkSystemPoolRuntime(ctx, initiator, wire); err != nil {
		return err
	}
	for _, files := range []endpoint.Endpoint{send, receive} {
		command := "test -x /usr/bin/" + string(method) + " && command -v bash >/dev/null && command -v flock >/dev/null && command -v sync >/dev/null"
		if err := files.Exec(ctx, command, endpoint.ExecOptions{}); err != nil {
			return fmt.Errorf("系统 %s 工具预检: %w", method, err)
		}
	}
	free, err := ncatAvailableBytes(ctx, receive, path.Dir(operation.TargetPath))
	if err != nil {
		return err
	}
	if free < before.Bytes {
		return errors.New("系统传输目标暂存空间不足")
	}
	sourceLease, closeSourceLease, err := a.newTaskWorkspace(ctx, source, send, "/tmp", elevatedSource)
	if err != nil {
		return err
	}
	defer func() {
		retErr = ncatCleanupResult(retErr, closeSourceLease(!errors.Is(retErr, endpoint.ErrCommandExitUnconfirmed)), committed)
	}()
	targetLease, closeTargetLease, err := a.newTaskWorkspace(ctx, target, receive, path.Dir(operation.TargetPath), elevatedTarget)
	if err != nil {
		return err
	}
	defer func() {
		retErr = ncatCleanupResult(retErr, closeTargetLease(!errors.Is(retErr, endpoint.ErrCommandExitUnconfirmed)), committed)
	}()
	staged := path.Join(targetLease.Directory, "payload")
	localLease, peerLease := sourceLease, targetLease
	if direction == strategy.TargetPull {
		localLease, peerLease = targetLease, sourceLease
	}
	// Dynamic server arguments stay an argv vector all the way to Bash's
	// "$@". Both the peer and the initiating child inherit their own fd9 lease.
	peerPrefix := append(peerLease.CommandPrefix(), "bash", "--noprofile", "--norc", "-c", ncatOwnedCommand(`exec "$@"`), "dragfm")
	request := systemPoolRequest{Version: 1, Mode: "native", Route: wire, Native: &systemNativeRequest{
		Method: method, Pull: direction == strategy.TargetPull, Source: operation.SourcePath, Target: staged,
		Directory: before.Items[0].Mode.IsDir(), PeerPrefix: peerPrefix,
	}}
	input, err := json.Marshal(request)
	if err != nil {
		return err
	}
	var diagnostic boundedbuf.Buffer
	progress := &nativeProgress{ctx: ctx}
	activity.Report(ctx, "system-"+string(method), 0)
	program := "exec 6<&0; exec python3 -I -B -c " + shellQuote(systemNativeProgram+"\n"+systemPoolStream)
	if err := initiator.Exec(ctx, localLease.Command(ncatOwnedCommand(program)), endpoint.ExecOptions{Stdin: bytes.NewReader(input), Stdout: progress, Stderr: &diagnostic}); err != nil {
		return fmt.Errorf("系统 %s 执行: %w; %s", method, systemTransferExitError(err, true), diagnostic.String())
	}
	if operation.Progress != nil {
		operation.Progress(transfer.Progress{Stage: "verify", Path: operation.TargetPath, BytesTotal: before.Bytes})
	}
	after, err := transfer.Snapshot(ctx, send, operation.SourcePath, operation.Move)
	if err != nil {
		return err
	}
	if err := transfer.CompareManifests(before, after, false); err != nil {
		return errors.Join(transfer.ErrSourceChanged, err)
	}
	received, err := transfer.Snapshot(ctx, receive, staged, operation.Move)
	if err != nil {
		return err
	}
	if len(before.Items) != len(received.Items) {
		return errors.New("系统传输暂存清单包含缺失或多余项")
	}
	if err := transfer.CompareManifests(before, received, true); err != nil {
		return fmt.Errorf("系统传输暂存校验: %w", err)
	}
	stagedOperation := operation
	stagedOperation.TargetPath = staged
	if err := transfer.RestoreOwnership(ctx, stagedOperation, before); err != nil {
		return err
	}
	// SCP time fields are whole seconds; restore the endpoint's actual mtime
	// precision, and apply directory permissions last, using the original list.
	for i := len(before.Items) - 1; i >= 0; i-- {
		item := before.Items[i]
		if item.Mode&fs.ModeSymlink != 0 {
			continue
		}
		name := path.Join(staged, item.Relative)
		if err := receive.Chmod(ctx, name, item.Mode); err != nil {
			return err
		}
		if err := receive.Chtimes(ctx, name, item.ModifiedTime(), item.ModifiedTime()); err != nil {
			return err
		}
	}
	// Sync the containing filesystem, not a copied symlink's referent.
	if err := receive.Exec(ctx, targetLease.Command("command sync -f -- "+shellQuote(targetLease.Directory)), endpoint.ExecOptions{}); err != nil {
		return err
	}
	if err := receive.Rename(ctx, staged, operation.TargetPath, false); err != nil {
		return err
	}
	committed = true
	if err := receive.Exec(ctx, targetLease.Command("command sync -f -- "+shellQuote(path.Dir(operation.TargetPath))), endpoint.ExecOptions{}); err != nil {
		return fmt.Errorf("目标已提交但持久化确认失败，源保留: %w", err)
	}
	if access := accessFor(ctx); access != nil {
		return access.finishTransfer(ctx, before, elevatedTarget)
	}
	return finishAcceleratedMove(ctx, operation, before, "系统 "+string(method))
}

// Python emits measured duplex wire bytes, not completed file percentages.
// This stream is separate from tool output, which remains bounded/redacted.
type nativeProgress struct {
	ctx  context.Context
	tail string
	last int64
}

func (p *nativeProgress) Write(data []byte) (int, error) {
	p.tail += string(data)
	for {
		line, rest, ok := strings.Cut(p.tail, "\n")
		if !ok {
			break
		}
		p.tail = rest
		if value, found := strings.CutPrefix(line, "wire "); found {
			if n, err := strconv.ParseInt(value, 10, 64); err == nil && n >= p.last {
				p.last = n
				activity.Report(p.ctx, "transport", n)
			}
		}
	}
	if len(p.tail) > 128 {
		p.tail = ""
	}
	return len(data), nil
}
