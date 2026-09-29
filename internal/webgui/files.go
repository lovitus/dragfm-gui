package webgui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/configtext"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/routespec"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"golang.org/x/crypto/ssh"
)

func (a *App) List(paneID PaneID, endpointName, directory string) (DirectoryListing, error) {
	pane, err := a.ensureEndpoint(context.Background(), paneID, endpointName, "")
	if err != nil {
		return DirectoryListing{}, err
	}
	abs, err := pane.endpoint.Abs(context.Background(), directory)
	if err != nil {
		return DirectoryListing{}, err
	}
	entries, err := pane.endpoint.List(context.Background(), abs)
	if err != nil {
		return DirectoryListing{}, err
	}
	model := make([]FileEntryModel, 0, len(entries))
	for _, item := range entries {
		model = append(model, fileEntryModel(item))
	}
	a.updatePaneLocation(paneID, pane, abs)
	return DirectoryListing{Pane: paneID, Endpoint: endpointName, Path: abs, Entries: model, ConnectionID: pane.connection, Warning: pane.warning}, nil
}

func (a *App) ChangeEndpoint(paneID PaneID, endpointName, peerName string) (DirectoryListing, error) {
	pane, err := a.ensureEndpoint(context.Background(), paneID, endpointName, peerName)
	if err != nil {
		return DirectoryListing{}, err
	}
	directory := pane.path
	if directory == "" {
		directory, err = pane.endpoint.Home(context.Background())
		if err != nil {
			return DirectoryListing{}, err
		}
	}
	return a.List(paneID, endpointName, directory)
}

func fileEntryModel(item endpoint.Entry) FileEntryModel {
	return FileEntryModel{Name: item.Name, Path: item.Path, Mode: item.Mode.String(), Size: item.Size, Modified: item.Modified.UTC().Format(time.RFC3339Nano), Directory: item.IsDir(), Symlink: item.Mode&fs.ModeSymlink != 0}
}

func (a *App) updatePaneLocation(paneID PaneID, expected *paneState, directory string) {
	a.mu.Lock()
	current := a.panes[paneID]
	if a.store == nil || a.locking || expected.generation != a.generation || current == nil || current.stale || current.endpoint != expected.endpoint {
		a.mu.Unlock()
		return
	}
	endpointName := expected.name
	if pane := a.panes[paneID]; pane != nil {
		pane.name, pane.path = endpointName, directory
	}
	if paneID == LeftPane {
		a.document.UI.LeftEndpoint, a.document.UI.LeftPath = endpointName, directory
	} else {
		a.document.UI.RightEndpoint, a.document.UI.RightPath = endpointName, directory
	}
	for index := range a.document.Hosts {
		if a.document.Hosts[index].Name == endpointName {
			a.document.Hosts[index].LastDirectory = directory
		}
	}
	a.mu.Unlock()
	a.requestSave()
}

func (a *App) PrepareDrop(sourcePane PaneID, sourcePath string, destinationPane PaneID, destinationDirectory string) (DropPreview, error) {
	if sourcePane == destinationPane || !validPane(sourcePane) || !validPane(destinationPane) {
		return DropPreview{}, errors.New("拖放必须发生在左右文件栏之间")
	}
	source, err := a.pane(sourcePane)
	if err != nil {
		return DropPreview{}, err
	}
	destination, err := a.pane(destinationPane)
	if err != nil {
		return DropPreview{}, err
	}
	sourcePath, err = source.endpoint.Abs(context.Background(), sourcePath)
	if err != nil {
		return DropPreview{}, err
	}
	entry, err := source.endpoint.Stat(context.Background(), sourcePath)
	if err != nil && !errors.Is(err, fs.ErrPermission) {
		return DropPreview{}, err
	}
	if sourcePath == source.endpoint.Dir(sourcePath) {
		return DropPreview{}, errors.New("拒绝传输文件系统根目录")
	}
	if errors.Is(err, fs.ErrPermission) {
		// Preview must not elevate or mislabel an inaccessible item as absent.
		// The queued job will ask about this exact source before reading it.
		entry.Name = filepath.Base(sourcePath)
		if _, remote := source.endpoint.(*endpoint.Remote); remote {
			entry.Name = path.Base(sourcePath)
		}
	}
	destinationDirectory, err = destination.endpoint.Abs(context.Background(), destinationDirectory)
	if err != nil {
		return DropPreview{}, err
	}
	target := destination.endpoint.Join(destinationDirectory, entry.Name)
	_, statErr := destination.endpoint.Stat(context.Background(), target)
	conflict := statErr == nil || errors.Is(statErr, fs.ErrPermission)
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) && !errors.Is(statErr, fs.ErrPermission) {
		return DropPreview{}, statErr
	}
	return DropPreview{SourcePane: sourcePane, DestinationPane: destinationPane, SourcePath: sourcePath, DestinationDirectory: destinationDirectory, TargetPath: target, Name: entry.Name, Conflict: conflict}, nil
}

func (a *App) QueueTransfer(request TransferRequest) (string, error) {
	if request.SourcePane == request.DestinationPane || !validPane(request.SourcePane) || !validPane(request.DestinationPane) {
		return "", errors.New("无效的传输方向")
	}
	source, err := a.pane(request.SourcePane)
	if err != nil {
		return "", err
	}
	destination, err := a.pane(request.DestinationPane)
	if err != nil {
		return "", err
	}
	request.SourcePath, err = source.endpoint.Abs(context.Background(), request.SourcePath)
	if err != nil {
		return "", err
	}
	request.TargetPath, err = destination.endpoint.Abs(context.Background(), request.TargetPath)
	if err != nil {
		return "", err
	}
	verb := "复制"
	if request.Move {
		verb = "移动"
	}
	description := fmt.Sprintf("%s · %s:%s → %s:%s", verb, source.name, request.SourcePath, destination.name, request.TargetPath)
	return a.submitFor(source.generation, jobs.Job{Description: description, Run: func(ctx context.Context, emit func(jobs.Update)) (retErr error) {
		var knownBytes int64
		var knownFiles int
		var statusMu sync.Mutex
		var currentMethod string
		ctx, _ = activity.WithObserver(ctx, func(stage string, wireBytes int64) {
			if strings.HasPrefix(stage, "system-files/") {
				emit(jobs.Update{Message: "已切换到系统 OpenSSH 文件通道（不上传 agent），保持本任务已批准的权限"})
				return
			}
			if strings.HasPrefix(stage, "stream-port/") {
				emit(jobs.Update{Message: "加密数据流 · 正在尝试端口 " + strings.TrimPrefix(stage, "stream-port/")})
				return
			}
			if strings.HasPrefix(stage, "ncat-port/") {
				emit(jobs.Update{Message: "tar+ncat · 正在尝试端口 " + strings.TrimPrefix(stage, "ncat-port/")})
				return
			}
			if stage == "ncat-connected" {
				emit(jobs.Update{Stage: "ncat-connected", Message: "tar+ncat · 数据连接已建立，正在传输；完成后经 SSH 校验归档"})
				return
			}
			// Channel I/O is liveness, not file progress. Do not erase a real
			// measured percentage or the active copy/verification phase.
			emit(jobs.Update{Message: fmt.Sprintf("远端活性 · %s · 已测量 %d 通道 I/O bytes（可能含协议开销，非文件完成百分比）", stage, wireBytes)})
		})
		operation := transfer.Operation{Source: source.endpoint, Destination: destination.endpoint, SourcePath: request.SourcePath, TargetPath: request.TargetPath, Move: request.Move, Overwrite: request.Overwrite, Progress: func(progress transfer.Progress) {
			statusMu.Lock()
			defer statusMu.Unlock()
			bytesTotal, filesTotal := progress.BytesTotal, progress.FilesTotal
			if bytesTotal == 0 {
				bytesTotal = knownBytes
			}
			if filesTotal == 0 {
				filesTotal = knownFiles
			}
			fraction := 0.0
			progressKnown := bytesTotal > 0 && (progress.BytesDone > 0 || progress.Stage == "copy")
			if progress.Stage == "verify" || progress.Stage == "snapshot" {
				progressKnown = false
			}
			if progressKnown {
				fraction = float64(progress.BytesDone) / float64(bytesTotal)
			}
			// Concrete native operations refine the generic same-host plan.
			// Other method labels come from the actual strategy observer, not
			// a low-level copy callback that would discard direction/route.
			method := ""
			if progress.Method == "cp" || progress.Method == "mv" {
				method = "same-host · " + progress.Method
				currentMethod = method
			}
			emit(jobs.Update{Progress: fraction, ProgressKnown: progressKnown, Indeterminate: !progressKnown, Stage: progress.Stage, Method: method, BytesDone: progress.BytesDone, BytesTotal: bytesTotal, FilesDone: progress.FilesDone, FilesTotal: filesTotal, Message: fmt.Sprintf("%s · %s · %d/%d bytes", progress.Stage, progress.Path, progress.BytesDone, bytesTotal)})
		}}
		ctx, access := newTransferAccess(ctx, a, operation)
		defer func() {
			if err := access.close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("任务文件通道清理未完成: %w", err))
			}
		}()
		emit(jobs.Update{Stage: "preflight", Indeterminate: true, Message: "正在预检路径、权限、源清单与可用能力；移动还需计算源文件哈希"})
		preflight, err := access.preflight(ctx)
		if err != nil {
			return fmt.Errorf("传输预检失败: %w", err)
		}
		statusMu.Lock()
		knownBytes = preflight.Source.Bytes
		for _, item := range preflight.Source.Items {
			if item.Mode.IsRegular() {
				knownFiles++
			}
		}
		statusMu.Unlock()
		emit(jobs.Update{Indeterminate: true, Stage: "preflight", BytesTotal: knownBytes, FilesTotal: knownFiles, Message: fmt.Sprintf("预检完成 · %d 项（%d 个普通文件）· %d bytes · 同机=%t · 源=%s · 目标=%s", len(preflight.Source.Items), knownFiles, knownBytes, preflight.SameMachine, preflight.SourceCapabilities.Architecture, preflight.TargetCapabilities.Architecture)})
		attempts, approve := a.planTransfer(operation, preflight, probeDirectoryWritable, access.passwords)
		err = strategy.Execute(ctx, attempts, approve, func(event strategy.Event) {
			statusMu.Lock()
			defer statusMu.Unlock()
			label := string(event.Attempt.Tier)
			if event.Attempt.RouteName != "" {
				label += " · " + event.Attempt.RouteName
			}
			if event.Attempt.Group {
				label += " · 候选探测/隧道准备"
			} else {
				label += fmt.Sprintf(" · %s · %s", event.Attempt.Direction, event.Attempt.Method)
				rootPair := event.Attempt.Elevated && (event.Attempt.Method == strategy.Rsync || event.Attempt.Method == strategy.SCP)
				if needsElevation(ctx, strategy.SourcePush, rootPair || event.Attempt.Elevated && event.Attempt.Direction == strategy.SourcePush) {
					label += " · 源端高权"
				}
				if needsElevation(ctx, strategy.TargetPull, rootPair || event.Attempt.Elevated && event.Attempt.Direction == strategy.TargetPull) {
					label += " · 目标端高权"
				}
			}
			message := fmt.Sprintf("策略 %s · %s", label, event.Stage)
			if event.Error != nil {
				message += " · " + event.Error.Error()
			}
			update := jobs.Update{Message: message}
			if event.Stage == "running" {
				currentMethod = label
				update.Stage, update.Method, update.Indeterminate, update.ResetCounters = "attempt", label, true, true
				update.BytesTotal, update.FilesTotal = knownBytes, knownFiles
			} else if !event.Attempt.Group && event.Stage != "skipped" {
				// A memory transfer may acquire a file view after its first
				// permission failure; reflect that final, actual privilege.
				if currentMethod != "same-host · cp" && currentMethod != "same-host · mv" {
					currentMethod, update.Method = label, label
				}
			}
			// Group completion must not replace the actual winning nested
			// method, nor reset measured native/memory-copy counters to zero.
			emit(update)
		})
		if err == nil {
			emit(jobs.Update{ProgressKnown: true, Progress: 1, Stage: "done", BytesDone: knownBytes, BytesTotal: knownBytes, FilesDone: knownFiles, FilesTotal: knownFiles, Message: fmt.Sprintf("传输完成 · %d 项（%d 个普通文件）· %d bytes", len(preflight.Source.Items), knownFiles, knownBytes)})
		}
		return err
	}}, source, destination)
}

func (a *App) QueueDelete(paneID PaneID, target string, recursive bool) (string, error) {
	pane, err := a.pane(paneID)
	if err != nil {
		return "", err
	}
	target, err = pane.endpoint.Abs(context.Background(), target)
	if err != nil {
		return "", err
	}
	if target == pane.endpoint.Dir(target) {
		return "", errors.New("拒绝删除文件系统根目录")
	}
	return a.submitFor(pane.generation, jobs.Job{Description: "删除 · " + pane.name + ":" + target, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		return pane.endpoint.Remove(ctx, target, recursive)
	}}, pane)
}

func (a *App) QueueHash(paneID PaneID, target string) (string, error) {
	pane, err := a.pane(paneID)
	if err != nil {
		return "", err
	}
	target, err = pane.endpoint.Abs(context.Background(), target)
	if err != nil {
		return "", err
	}
	return a.submitFor(pane.generation, jobs.Job{Description: "SHA-256 · " + pane.name + ":" + target, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		manifest, err := transfer.Snapshot(ctx, pane.endpoint, target, true)
		if err != nil {
			return err
		}
		var lines []string
		for _, item := range manifest.Items {
			if item.SHA256 != "" {
				name := item.Relative
				if name == "" {
					name = filepath.Base(target)
				}
				lines = append(lines, item.SHA256+"  "+name)
			}
		}
		emit(jobs.Update{Progress: 1, Message: strings.Join(lines, "\n")})
		return nil
	}}, pane)
}

func (a *App) QueueCommand(target, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("命令不能为空")
	}
	ctx, cancel, generation, err := a.activeContext(context.Background())
	if err != nil {
		return "", err
	}
	defer cancel()
	_ = ctx
	var selected endpoint.Endpoint
	var bound []*paneState
	directory, name := "", target
	switch target {
	case "左栏":
		pane, err := a.pane(LeftPane)
		if err != nil {
			return "", err
		}
		selected, directory, name = pane.endpoint, pane.path, pane.name
		bound = append(bound, pane)
	case "右栏":
		pane, err := a.pane(RightPane)
		if err != nil {
			return "", err
		}
		selected, directory, name = pane.endpoint, pane.path, pane.name
		bound = append(bound, pane)
	case "控制机":
		selected, name = endpoint.NewLocal(), "控制机"
	default:
		return "", fmt.Errorf("未知命令目标 %q", target)
	}
	return a.submitFor(generation, jobs.Job{Description: "命令 · " + name + " · $ " + command, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		settings := a.settingsFor(ctx)
		settings.secretMu.RLock()
		secrets := knownSecrets(settings.document, settings.passwords, settings.master, settings.extraSecret...)
		settings.secretMu.RUnlock()
		output := &liveCommandOutput{emit: emit}
		stdout := &commandStream{secrets: secrets, dst: output}
		stderr := &commandStream{secrets: secrets, dst: output}
		// Deferred finalization also covers an endpoint panic. Close decoders
		// before the aggregate buffer so their last partial records are kept.
		defer output.Close()
		defer stderr.Close()
		defer stdout.Close()
		emit(jobs.Update{Stage: "command", Indeterminate: true, Message: "命令执行中"})
		err := selected.Exec(ctx, command, endpoint.ExecOptions{Directory: directory, Stdout: stdout, Stderr: stderr})
		stdout.Close()
		stderr.Close()
		output.Close()
		if err == nil {
			emit(jobs.Update{Stage: "done", Message: "命令已完成"})
		}
		return err
	}}, bound...)
}

func (a *App) CancelJob(id string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.locking {
		a.queue.Cancel(id)
	}
}

func (a *App) GetConfigTexts() (ConfigTexts, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil || a.locking {
		return ConfigTexts{}, errors.New("保险库尚未解锁")
	}
	return ConfigTexts{Markdown: configtext.Markdown(a.document), Revision: a.configRevisionLocked()}, nil
}

func (a *App) SaveConfigTexts(markdown string) (BootstrapModel, error) {
	return a.saveConfigTexts(markdown, "")
}

func (a *App) SaveConfigTextsAtRevision(markdown, revision string) (BootstrapModel, error) {
	if revision == "" {
		return BootstrapModel{}, errors.New("缺少配置版本，请重新载入")
	}
	return a.saveConfigTexts(markdown, revision)
}

func (a *App) saveConfigTexts(markdown, revision string) (BootstrapModel, error) {
	// Save and publication form one transaction. Never expose candidate values
	// to dialers or background persistence until the atomic vault write succeeds.
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	locked := true
	defer func() {
		if locked {
			a.mu.Unlock()
		}
	}()
	if a.store == nil || a.locking {
		return BootstrapModel{}, errors.New("保险库尚未解锁")
	}
	if revision != "" && revision != a.configRevisionLocked() {
		return BootstrapModel{}, errors.New("配置已在其他操作中改变；保留当前草稿，请重新载入后合并")
	}
	old := a.document.Clone()
	hosts, privateKeys, proxies, err := configtext.ParseMarkdown(markdown, old)
	if err != nil {
		return BootstrapModel{}, err
	}
	usedHostNames := make(map[string]bool)
	for index := range hosts {
		hops, parseErr := routespec.ParseSSH(hosts[index].RouteSpec, func(name string) (*config.PrivateKey, bool) {
			for index := range privateKeys {
				if privateKeys[index].Name == name || privateKeys[index].ID == name {
					return &privateKeys[index], true
				}
			}
			return nil, false
		})
		if parseErr != nil {
			return BootstrapModel{}, fmt.Errorf("主机 %q: %w", hosts[index].Name, parseErr)
		}
		if len(hops) == 0 {
			return BootstrapModel{}, fmt.Errorf("主机 %q: SSH 路由为空", hosts[index].Name)
		}
		if usedHostNames[hosts[index].Name] {
			return BootstrapModel{}, fmt.Errorf("主机名 %q 重复", hosts[index].Name)
		}
		usedHostNames[hosts[index].Name] = true
	}
	usedProxyNames := make(map[string]bool)
	proxyIDsByName := make(map[string]string)
	proxyDisabledByName := make(map[string]bool)
	for index := range proxies {
		parsed, parseErr := routespec.ParseSOCKS(proxies[index].Spec)
		if parseErr != nil {
			return BootstrapModel{}, fmt.Errorf("SOCKS %q: %w", proxies[index].Name, parseErr)
		}
		if parsed.Address == "" {
			return BootstrapModel{}, fmt.Errorf("SOCKS %q: 地址为空", proxies[index].Name)
		}
		if usedProxyNames[proxies[index].Name] {
			return BootstrapModel{}, fmt.Errorf("SOCKS 名称 %q 重复", proxies[index].Name)
		}
		usedProxyNames[proxies[index].Name] = true
		proxyIDsByName[proxies[index].Name] = proxies[index].ID
		proxyDisabledByName[proxies[index].Name] = proxies[index].Disabled
	}
	for index := range hosts {
		if hosts[index].DefaultSOCKSID == "" {
			continue
		}
		if id, ok := proxyIDsByName[hosts[index].DefaultSOCKSID]; ok {
			if proxyDisabledByName[hosts[index].DefaultSOCKSID] {
				return BootstrapModel{}, fmt.Errorf("主机 %q 的默认 SOCKS %q 已禁用", hosts[index].Name, hosts[index].DefaultSOCKSID)
			}
			hosts[index].DefaultSOCKSID = id
			continue
		}
		if proxy := old.SOCKSByID(hosts[index].DefaultSOCKSID); proxy != nil {
			if id, ok := proxyIDsByName[proxy.Name]; ok {
				hosts[index].DefaultSOCKSID = id
				continue
			}
		}
		return BootstrapModel{}, fmt.Errorf("主机 %q 引用的默认 SOCKS %q 不存在", hosts[index].Name, hosts[index].DefaultSOCKSID)
	}
	for index := range privateKeys {
		pem := []byte(privateKeys[index].PEM)
		var parseErr error
		if privateKeys[index].Passphrase == "" {
			_, parseErr = ssh.ParsePrivateKey(pem)
		} else {
			_, parseErr = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(privateKeys[index].Passphrase))
		}
		if parseErr != nil {
			return BootstrapModel{}, fmt.Errorf("私钥 %q 无法解析: %w", privateKeys[index].Name, parseErr)
		}
	}
	next := old.Clone()
	next.Hosts, next.SOCKS, next.Keys = hosts, proxies, privateKeys
	unchanged := make(map[string]bool)
	unchanged["local"] = true
	for _, host := range next.Hosts {
		previous := old.HostByID(host.ID)
		if previous == nil || host.Disabled {
			continue
		}
		before, after := *previous, host
		before.LastDirectory, after.LastDirectory = "", ""
		before.NoRelay, after.NoRelay = false, false
		unchanged[host.ID] = reflect.DeepEqual(before, after) && reflect.DeepEqual(old.Keys, next.Keys)
		if host.DefaultSOCKSID != "" {
			unchanged[host.ID] = unchanged[host.ID] && reflect.DeepEqual(old.SOCKSByID(host.DefaultSOCKSID), next.SOCKSByID(host.DefaultSOCKSID))
		}
	}
	next.Relays = nil
	for _, relay := range old.Relays {
		host := next.HostByID(relay.RelayHostID)
		if host != nil && !host.NoRelay && unchanged[relay.EndpointAID] && unchanged[relay.EndpointBID] && unchanged[relay.RelayHostID] {
			next.Relays = append(next.Relays, relay)
		}
	}
	if err := a.store.Save(a.password, next); err != nil {
		return BootstrapModel{}, err
	}
	a.document = next
	a.configVersion++
	for id := range a.runtimePasswords {
		if !unchanged[id] {
			delete(a.runtimePasswords, id)
		}
	}
	for id := range a.sessionSSH {
		if !unchanged[id] {
			delete(a.sessionSSH, id)
		}
	}
	for _, pane := range a.panes {
		if pane.name == "本机" {
			continue
		}
		host, exists := old.HostByName(pane.name)
		if !exists || !unchanged[host.ID] {
			// Keep the concrete endpoint alive for jobs already bound to it, but
			// don't reuse it for new user actions under the edited host's name.
			pane.stale = true
		}
	}
	a.mu.Unlock()
	locked = false
	return a.Bootstrap()
}
