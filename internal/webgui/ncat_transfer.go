package webgui

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/agentroute"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"golang.org/x/crypto/ssh"
)

// This is an independent system-tool fallback, not the agent's ncat carrier.
// Only archive bytes use ncat. Both process results and the SHA-256 of the
// exact transmitted archive arrive over the already authenticated SSH routes.
// The destination stages the bounded archive before extraction; the controller
// sees only diagnostics/hashes during archive transfer. The existing move
// verifier may stream files to hash them; it still never writes controller disk.
func (a *App) runSystemNcatTar(ctx context.Context, operation transfer.Operation, direction strategy.Direction, elevated bool) (retErr error) {
	return a.runSystemNcatTarRoute(ctx, operation, direction, elevated, nil)
}

func (a *App) runSystemNcatTarRoute(ctx context.Context, operation transfer.Operation, direction strategy.Direction, elevated bool, route *agentroute.Route) (retErr error) {
	source, target, ok := remoteRemotePair(operation)
	if !ok {
		return errors.New("系统 tar+ncat 需要两个 Linux SSH 端点")
	}
	if direction != strategy.SourcePush && direction != strategy.TargetPull {
		return errors.New("未知 tar+ncat 方向")
	}
	elevatedSource := needsElevation(ctx, strategy.SourcePush, elevated && direction == strategy.SourcePush)
	elevatedTarget := needsElevation(ctx, strategy.TargetPull, elevated && direction == strategy.TargetPull)
	if accessFor(ctx) != nil {
		if err := strategy.Authorize(ctx, strategy.Attempt{Method: strategy.NcatTar, Direction: direction}, strategy.ListenRisk); err != nil {
			return err
		}
	}
	operation = fileOperation(ctx, operation)
	// Separate transfer channels allow cancellation/cleanup without closing the
	// shared permission views used for final verification or a later relay.
	committed := false
	defer func() {
		if errors.Is(retErr, endpoint.ErrCommandExitUnconfirmed) {
			retErr = transfer.PreserveSource(fmt.Errorf("远端进程退出未确认，停止重试；暂存资源保留待检查: %w", retErr))
		} else if retErr != nil && committed {
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
	operation.Source, operation.Destination = send, receive
	operation.PreserveOwner = elevatedTarget
	before, err := transfer.SnapshotForOperation(ctx, operation)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if !item.Mode.IsRegular() && !item.Mode.IsDir() && item.Mode&fs.ModeSymlink == 0 {
			return fmt.Errorf("tar+ncat 不接收特殊文件类型: %s", item.Mode.Type())
		}
	}
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); err == nil {
		return errors.New("目标已存在，系统 tar+ncat 跳过整根提交并继续尝试安全合并")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("tar+ncat 目标检查: %w", err)
	}
	for _, host := range []endpoint.Endpoint{send, receive} {
		if err := host.Exec(ctx, "command -v bash >/dev/null && command -v flock >/dev/null && command -v ncat >/dev/null && command -v tar >/dev/null && command -v sha256sum >/dev/null && command -v tee >/dev/null && command -v mkfifo >/dev/null && command -v head >/dev/null && command -v sync >/dev/null", endpoint.ExecOptions{}); err != nil {
			return fmt.Errorf("系统 tar+ncat 工具预检: %w", err)
		}
	}
	// Addresses describe the physical endpoints, not an elevated login route.
	listening := target
	connecting := source
	if direction == strategy.TargetPull {
		listening, connecting = source, target
	}
	addresses, err := remoteIPv4s(ctx, listening)
	if err != nil {
		return err
	}
	origins, err := remoteIPv4s(ctx, connecting)
	if err != nil {
		return err
	}
	physicalOrigins := append([]string(nil), origins...)
	if route != nil {
		connector := send
		if direction == strategy.TargetPull {
			connector = receive
		}
		if err := checkSystemPoolRuntime(ctx, connector, *route); err != nil {
			return err
		}
		if len(route.Hops) > 0 {
			// The last authenticated hop is the listening endpoint itself.
			// Its direct-tcpip channel, not the original source, opens ncat.
			origins = append(append([]string(nil), addresses...), "127.0.0.1")
		} else if route.SOCKS != nil {
			origins, err = systemProxyOrigins(ctx, connector, route.SOCKS.Address)
			if err != nil {
				return err // Never replace the scoped allow list with 0.0.0.0/0.
			}
		}
	}
	private := true
	for _, address := range append(append(append([]string(nil), addresses...), physicalOrigins...), origins...) {
		if ip := net.ParseIP(address); ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
			private = false
		}
	}
	if !private {
		if err := strategy.Authorize(ctx, strategy.Attempt{Method: strategy.NcatTar, Direction: direction}, strategy.PlaintextRisk); err != nil {
			return err
		}
	}
	// gzip can expand incompressible data slightly; tar also adds headers and
	// long-name records. Bound unauthenticated input so a wrong peer cannot send
	// an unlimited archive while the SSH-authenticated checksum is pending.
	if before.Bytes < 0 || before.Bytes > (math.MaxInt64-(1<<20))/2 {
		return errors.New("tar+ncat 归档大小超出可表示范围")
	}
	limit := before.Bytes*2 + 1<<20
	for _, item := range before.Items {
		overhead := int64(len(item.Relative)+len(item.LinkTarget)+len(path.Base(operation.SourcePath)))*4 + 8192
		if overhead > math.MaxInt64-limit {
			return errors.New("tar+ncat 归档大小超出可表示范围")
		}
		limit += overhead
	}
	free, err := ncatAvailableBytes(ctx, receive, path.Dir(operation.TargetPath))
	if err != nil {
		return err
	}
	// Reserve extraction space as well as a small metadata margin; do not
	// reject compressible files merely because the archive's loose upper bound
	// exceeds free space. Recheck before extraction for concurrent disk usage.
	const margin = int64(1 << 20)
	if free < margin || free-margin <= before.Bytes {
		return errors.New("tar+ncat 目标空间不足以同时保存归档和解包内容")
	}
	if budget := free - before.Bytes - margin; limit > budget {
		limit = budget
	}
	sourceLease, closeSourceLease, err := a.newTaskWorkspace(ctx, source, send, "/tmp", elevatedSource)
	if err != nil {
		return err
	}
	processesStopped := true
	defer func() {
		remove := processesStopped && !errors.Is(retErr, endpoint.ErrCommandExitUnconfirmed)
		retErr = ncatCleanupResult(retErr, closeSourceLease(remove), committed)
	}()
	targetLease, closeTargetLease, err := a.newTaskWorkspace(ctx, target, receive, path.Dir(operation.TargetPath), elevatedTarget)
	if err != nil {
		return err
	}
	defer func() {
		remove := processesStopped && !errors.Is(retErr, endpoint.ErrCommandExitUnconfirmed)
		retErr = ncatCleanupResult(retErr, closeTargetLease(remove), committed)
	}()
	sourceWork, targetWork := sourceLease.Directory, targetLease.Directory
	archive, extract := path.Join(targetWork, "archive.tar.gz"), path.Join(targetWork, "unpack")
	fifo := path.Join(sourceWork, "digest-pipe")
	ports := randomPorts(5)
	var failures []error
	transferred := false
	for _, port := range ports {
		for _, address := range addresses {
			if err := ctx.Err(); err != nil {
				return err
			}
			activity.Report(ctx, "ncat-port/"+strconv.Itoa(port), 0)
			carrier := ""
			var routeInput []byte
			if route != nil {
				mode := "send"
				if direction == strategy.TargetPull {
					mode = "receive"
				}
				routeInput, err = json.Marshal(systemPoolRequest{Version: 1, Mode: mode, Host: address, Port: port, Route: *route})
				if err != nil {
					return err
				}
				if len(routeInput) > 1<<20 {
					return errors.New("system route configuration exceeds 1 MiB limit")
				}
				carrier = "command python3 -I -B -c " + shellQuote(systemPoolStream)
			}
			sender, receiver := ncatArchiveCommandsWithCarrier(operation.SourcePath, fifo, archive, address, origins, port, direction, limit, carrier)
			sender, receiver = sourceLease.Command(sender), targetLease.Command(receiver)
			digest, err := runNcatPairWithInput(ctx, send, receive, sender, receiver, direction, routeInput)
			if errors.Is(err, endpoint.ErrCommandExitUnconfirmed) {
				processesStopped = false
				return transfer.PreserveSource(fmt.Errorf("远端进程退出未确认，停止重试并保留本任务暂存区 %q / %q，不删除源或暂存数据: %w", sourceWork, targetWork, err))
			}
			if err != nil && !transfer.Retryable(err) {
				return err
			}
			if err == nil {
				var received bytes.Buffer
				err = receive.Exec(ctx, "command sha256sum < "+shellQuote(archive), endpoint.ExecOptions{Stdout: &received})
				if err == nil {
					actual, parseErr := ncatDigest(received.String())
					if parseErr != nil || actual != digest {
						err = errors.New("tar+ncat 归档 SHA-256 不一致，未解包、未更改目标")
					}
				}
			}
			if err == nil {
				transferred = true
				break
			}
			failures = append(failures, fmt.Errorf("tar+ncat 端口 %d: %w", port, err))
			if staged, statErr := receive.Stat(ctx, archive); statErr == nil && staged.Size >= limit {
				return errors.Join(err, errors.New("tar+ncat 归档达到暂存大小/空间上限，未解包；继续其他传输方式"))
			}
			// Both process groups are stopped/joined before removing their exact
			// partial paths and trying the next port. No stale listener is reused.
			if err := removeNcatPartial(ctx, receive, archive); err != nil {
				return errors.Join(append(failures, err)...)
			}
			if err := removeNcatPartial(ctx, send, fifo); err != nil {
				return errors.Join(append(failures, err)...)
			}
		}
		if transferred {
			break
		}
	}
	if !transferred {
		return errors.Join(failures...)
	}
	after, err := transfer.Snapshot(ctx, operation.Source, operation.SourcePath, operation.Move)
	if err != nil {
		return err
	}
	if err := transfer.CompareManifests(before, after, false); err != nil {
		return errors.Join(transfer.ErrSourceChanged, err)
	}
	free, err = ncatAvailableBytes(ctx, receive, targetWork)
	if err != nil {
		return err
	}
	if free < margin || free-margin < before.Bytes {
		return errors.New("tar+ncat 收到归档后目标空间不足，尚未解包或提交")
	}
	// Never extract directly into a live destination, follow archive symlinks,
	// accept absolute names, or restore root ownership from archive headers.
	command := "unset TAR_OPTIONS GZIP; umask 077; command mkdir -m 0700 -- " + shellQuote(extract) + " && command tar -xzf " + shellQuote(archive) + " --no-same-owner --no-same-permissions -C " + shellQuote(extract)
	if err := receive.Exec(ctx, targetLease.Command(ncatOwnedCommand(command)), endpoint.ExecOptions{}); err != nil {
		return fmt.Errorf("解包经 SSH 哈希验证的归档: %w", systemTransferExitError(err, false))
	}
	staged := path.Join(extract, path.Base(operation.SourcePath))
	stagedManifest, err := transfer.Snapshot(ctx, receive, staged, operation.Move)
	if err != nil {
		return err
	}
	if len(before.Items) != len(stagedManifest.Items) {
		return errors.New("tar+ncat 解包后清单项数不一致")
	}
	if err := transfer.CompareManifests(before, stagedManifest, true); err != nil {
		return fmt.Errorf("tar+ncat 解包校验: %w", err)
	}
	stagedOperation := operation
	stagedOperation.TargetPath = staged
	if err := transfer.RestoreOwnership(ctx, stagedOperation, before); err != nil {
		return fmt.Errorf("tar+ncat 暂存所有权恢复: %w", err)
	}
	for i := len(before.Items) - 1; i >= 0; i-- {
		item := before.Items[i]
		if item.Mode&fs.ModeSymlink != 0 {
			continue
		}
		itemPath := staged
		if item.Relative != "" {
			itemPath = path.Join(staged, item.Relative)
		}
		if err := receive.Chmod(ctx, itemPath, item.Mode); err != nil {
			return err
		}
		if err := receive.Chtimes(ctx, itemPath, item.ModifiedTime(), item.ModifiedTime()); err != nil {
			return err
		}
	}
	// Flush the receiving filesystem before publishing, then persist the
	// directory entry before the move verifier is allowed to delete source.
	// Never sync a copied symlink, which would follow an unrelated referent.
	if err := receive.Exec(ctx, targetLease.Command("command sync -f -- "+shellQuote(targetLease.Directory)), endpoint.ExecOptions{}); err != nil {
		return fmt.Errorf("tar+ncat 提交前落盘失败: %w", err)
	}
	if err := operation.Destination.Rename(ctx, staged, operation.TargetPath, false); err != nil {
		return err
	}
	committed = true
	if err := receive.Exec(ctx, targetLease.Command("command sync -f -- "+shellQuote(path.Dir(operation.TargetPath))), endpoint.ExecOptions{}); err != nil {
		return fmt.Errorf("tar+ncat 目标已提交但目录落盘失败，源已保留: %w", err)
	}
	if access := accessFor(ctx); access != nil {
		return access.finishTransfer(ctx, before, elevatedTarget)
	}
	return finishAcceleratedMove(ctx, operation, before, "系统 tar+ncat")
}

func ncatAvailableBytes(ctx context.Context, remote endpoint.Endpoint, directory string) (int64, error) {
	if provider, ok := remote.(interface {
		AvailableBytes(context.Context, string) (int64, error)
	}); ok {
		if free, err := provider.AvailableBytes(ctx, directory); err == nil && free >= 0 {
			return free, nil
		}
	}
	var output bytes.Buffer
	if err := remote.Exec(ctx, "LC_ALL=C command df -Pk -- "+shellQuote(directory), endpoint.ExecOptions{Stdout: &output}); err != nil {
		return 0, fmt.Errorf("tar+ncat 暂存空间探测: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, errors.New("tar+ncat 暂存空间探测未返回容量")
	}
	blocks, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("tar+ncat 暂存容量无效: %w", err)
	}
	if blocks > math.MaxInt64/1024 {
		return math.MaxInt64, nil
	}
	return int64(blocks) * 1024, nil
}

func ncatArchiveCommands(source, fifo, archive, address string, origins []string, port int, direction strategy.Direction, limit int64) (string, string) {
	return ncatArchiveCommandsWithCarrier(source, fifo, archive, address, origins, port, direction, limit, "")
}

func ncatArchiveCommandsWithCarrier(source, fifo, archive, address string, origins []string, port int, direction strategy.Direction, limit int64, carrier string) (string, string) {
	network := "command ncat -4 -v --idle-timeout 60s "
	listen := "--listen --allow " + shellQuote(strings.Join(origins, ",")) + " "
	connect := "--wait 5s "
	send, receive := connect, listen
	if direction == strategy.TargetPull {
		send, receive = listen, connect
	}
	destination := shellQuote(address) + " " + strconv.Itoa(port)
	send = network + "--send-only " + send + destination
	receive = network + "--recv-only " + receive + destination
	if carrier != "" {
		if direction == strategy.SourcePush {
			send = carrier
		} else {
			receive = carrier
		}
	}
	// Hash the very same bytes that tee feeds to ncat, not a separately-created
	// tar stream. A FIFO carries only bytes, and sha256sum returns over SSH.
	sender := "unset TAR_OPTIONS GZIP; umask 077; command mkfifo -m 0600 -- " + shellQuote(fifo) + " || exit; command sha256sum < " + shellQuote(fifo) + " & digest_pid=$!; command tar -czf - -C " + shellQuote(path.Dir(source)) + " -- " + shellQuote(path.Base(source)) + " | command tee -- " + shellQuote(fifo) + " | " + send + "; copied=$?; wait \"$digest_pid\"; hashed=$?; if [ \"$copied\" -ne 0 ]; then exit \"$copied\"; fi; exit \"$hashed\""
	receiver := "umask 077; set -C; " + receive + " </dev/null | command head -c " + strconv.FormatInt(limit, 10) + " > " + shellQuote(archive)
	if carrier != "" {
		// Duplicate the original SSH stdin BEFORE the archive pipeline
		// replaces fd0. fd6 contains only the versioned credential frame.
		if direction == strategy.SourcePush {
			sender = "exec 6<&0; " + sender
		} else {
			receiver = "exec 6<&0; " + receiver
		}
	}
	return ncatOwnedCommand(sender), ncatOwnedCommand(receiver)
}

// sudo without a PTY forwards TERM to its direct child, not necessarily every
// process in a tar/tee/ncat pipeline. This monitor remains that direct child;
// Bash job control puts its one background job in a separate process group.
// Credentials are not part of this script, argv, or environment; stdin and
// the workspace's inherited directory lease are passed unchanged.
func ncatOwnedCommand(command string) string {
	script := `set +e; set +u; set +x; set -m
# A stopped child is still using its inherited lease. Older Bash must fail
# before starting any writer rather than treating a state change as exit.
if ! builtin wait -f 2>/dev/null; then
    printf '%s\n' 'Bash wait -f is required for transfer supervision' >&2
    exit 125
fi
child=; stopping=0
stop_owned() {
    stopping=1
    if [ -n "$child" ]; then
        trap '' TERM HUP INT
        kill -TERM -- "-$child" 2>/dev/null || :
        kill -KILL -- "-$child" 2>/dev/null || :
        builtin wait -f %1 2>/dev/null
        stopped_status=$?
        if [ "$stopped_status" -eq 77 ]; then
            exit 77
        fi
        if [ "$stopped_status" -eq 127 ]; then
            printf '%s\n' 'Transfer monitor cannot confirm child exit (wait status 127)' >&2
            exit 77
        fi
        exit 143
    fi
}
trap stop_owned TERM HUP INT
# This fresh shell owns exactly one job. A jobspec avoids older Bash's
# wait -f PID loop retaining a PROCESS pointer after internal job cleanup.
# Parse spawn through exit as one compound command: reading another command
# line can notify/reap a fast child before the jobspec is waited for.
{
    bash --noprofile --norc +m -o pipefail -c ` + shellQuote(command) + ` dragfm "$@" <&0 & child=$!
    [ "$stopping" -eq 0 ] || stop_owned
    builtin wait -f %1
    status=$?
    trap - TERM HUP INT
    # A missing job and a tool's own exit 127 are indistinguishable here.
    # Conservatively retain ownership rather than permit cleanup/retry.
    if [ "$status" -eq 127 ]; then
        printf '%s\n' 'Transfer monitor cannot confirm child exit (wait status 127)' >&2
        exit 77
    fi
    exit "$status"
}`
	return "unset BASH_ENV ENV; exec bash --noprofile --norc -m -c " + shellQuote(script) + ` dragfm "$@"`
}

func removeNcatPartial(ctx context.Context, remote endpoint.Endpoint, target string) error {
	if _, err := remote.Stat(ctx, target); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return remote.Remove(ctx, target, false)
}

func ncatCleanupResult(result, cleanup error, committed bool) error {
	if cleanup == nil {
		return result
	}
	err := errors.Join(result, cleanup)
	if committed {
		return transfer.PreserveSource(fmt.Errorf("数据已提交，临时资源清理失败；不重新复制: %w", err))
	}
	return transfer.PreserveSource(fmt.Errorf("临时资源清理未确认，停止重试: %w", err))
}

func ncatDigest(output string) (string, error) {
	fields := strings.Fields(output)
	if len(fields) != 2 || len(fields[0]) != 64 || fields[1] != "-" {
		return "", errors.New("没有收到有效的 SSH 归档校验和")
	}
	if data, err := hex.DecodeString(fields[0]); err != nil || len(data) != 32 {
		return "", errors.New("无效的 SSH 归档校验和")
	}
	return fields[0], nil
}

type ncatDiagnostic struct {
	mu        sync.Mutex
	data      []byte
	ready     chan struct{}
	once      sync.Once
	connected sync.Once
	ctx       context.Context
}

func (d *ncatDiagnostic) Write(p []byte) (int, error) {
	d.mu.Lock()
	d.data = append(d.data, p...)
	if len(d.data) > 8192 {
		d.data = append([]byte(nil), d.data[len(d.data)-8192:]...)
	}
	if bytes.Contains(d.data, []byte("Listening on ")) {
		d.once.Do(func() { close(d.ready) })
	}
	connected := bytes.Contains(d.data, []byte("Connected to "))
	d.mu.Unlock()
	if connected {
		d.connected.Do(func() { activity.Report(d.ctx, "ncat-connected", 0) })
	}
	return len(p), nil
}

func (d *ncatDiagnostic) String() string { d.mu.Lock(); defer d.mu.Unlock(); return string(d.data) }

func runNcatPair(ctx context.Context, source, target endpoint.Endpoint, sender, receiver string, direction strategy.Direction) (string, error) {
	return runNcatPairWithInput(ctx, source, target, sender, receiver, direction, nil)
}

func runNcatPairWithInput(ctx context.Context, source, target endpoint.Endpoint, sender, receiver string, direction strategy.Direction, input []byte) (string, error) {
	started := time.Now()
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	diagnostic := &ncatDiagnostic{ready: make(chan struct{}), ctx: ctx}
	var digest bytes.Buffer
	sentDone, receivedDone := make(chan error, 1), make(chan error, 1)
	startSender := func() {
		go func() {
			options := endpoint.ExecOptions{Stdout: &digest, Stderr: diagnostic}
			if direction == strategy.SourcePush && len(input) != 0 {
				options.Stdin = bytes.NewReader(input)
			}
			// Normalize monitor uncertainty at the Exec boundary, including
			// direct listeners and results consumed only during cancellation.
			sentDone <- systemTransferExitError(source.Exec(life, sender, options), len(input) != 0 && direction == strategy.SourcePush)
		}()
	}
	startReceiver := func() {
		go func() {
			options := endpoint.ExecOptions{Stderr: diagnostic}
			if direction == strategy.TargetPull && len(input) != 0 {
				options.Stdin = bytes.NewReader(input)
			}
			receivedDone <- systemTransferExitError(target.Exec(life, receiver, options), len(input) != 0 && direction == strategy.TargetPull)
		}()
	}
	listener := receivedDone
	if direction == strategy.SourcePush {
		startReceiver()
	} else {
		startSender()
		listener = sentDone
	}
	ready := time.NewTimer(10 * time.Second)
	defer ready.Stop()
	select {
	case <-diagnostic.ready:
	case err := <-listener:
		if err == nil {
			err = errors.New("监听进程在传输开始前退出")
		}
		return "", fmt.Errorf("启动 ncat 监听: %w; %s", err, diagnostic.String())
	case <-ready.C:
		cancel()
		exitErr := <-listener
		err := errors.New("ncat 未在期限内确认绑定具体接口")
		if errors.Is(exitErr, endpoint.ErrCommandExitUnconfirmed) {
			err = errors.Join(err, exitErr)
		}
		return "", err
	case <-ctx.Done():
		cancel()
		return "", fmt.Errorf("ncat 监听就绪前取消（%s）: %w; %s", time.Since(started), errors.Join(ctx.Err(), <-listener), diagnostic.String())
	}
	if direction == strategy.SourcePush {
		startSender()
	} else {
		startReceiver()
	}
	// The goroutines retain immutable channels. Only these select-local copies
	// become nil after completion; cancellation cannot strand a channel send.
	sent, received := sentDone, receivedDone
	var failures []error
	for sent != nil || received != nil {
		select {
		case err := <-sent:
			sent = nil
			if err != nil {
				// Peer cancellation is expected after the other side fails, but
				// must not erase an already-classified safety/host-key failure.
				var classified interface{ Retryable() bool }
				if !errors.Is(err, context.Canceled) || (errors.As(err, &classified) && !classified.Retryable()) {
					failures = append(failures, fmt.Errorf("源端 tar+ncat: %w", err))
				}
				cancel()
			}
		case err := <-received:
			received = nil
			if err != nil {
				var classified interface{ Retryable() bool }
				if !errors.Is(err, context.Canceled) || (errors.As(err, &classified) && !classified.Retryable()) {
					failures = append(failures, fmt.Errorf("目标端 tar+ncat: %w", err))
				}
				cancel()
			}
		case <-ctx.Done():
			// These flags describe results consumed by this select, not whether
			// the remote process was alive at the deadline. Do not infer that
			// from stderr or change cancellation/retry decisions for diagnostics.
			elapsed, sourceResult, targetResult := time.Since(started), sent == nil, received == nil
			cancel()
			if sent != nil {
				if err := <-sent; err != nil {
					failures = append(failures, fmt.Errorf("源端 tar+ncat 取消收尾: %w", err))
				}
				sent = nil
			}
			if received != nil {
				if err := <-received; err != nil {
					failures = append(failures, fmt.Errorf("目标端 tar+ncat 取消收尾: %w", err))
				}
				received = nil
			}
			return "", fmt.Errorf("ncat 传输取消（%s；已接收源端结果=%t，目标端结果=%t）: %w; %s", elapsed, sourceResult, targetResult, errors.Join(append(failures, ctx.Err())...), diagnostic.String())
		}
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("ncat 两端结果已接收，但任务已取消（%s）: %w; %s", time.Since(started), errors.Join(append(failures, ctx.Err())...), diagnostic.String())
	}
	if len(failures) > 0 {
		return "", fmt.Errorf("%w; %s", errors.Join(failures...), diagnostic.String())
	}
	return ncatDigest(digest.String())
}

func systemTransferExitError(err error, routed bool) error {
	var status *ssh.ExitError
	if errors.As(err, &status) && status.ExitStatus() == 77 {
		return errors.Join(endpoint.ErrCommandExitUnconfirmed, err)
	}
	if routed && errors.As(err, &status) && status.ExitStatus() == 78 {
		return transfer.PreserveSource(fmt.Errorf("SSH 跳板指纹变化，认证已阻断，不继续换端口或路线: %w", err))
	}
	return err
}
