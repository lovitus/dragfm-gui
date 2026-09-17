package webgui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

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
	abs, err := pane.endpoint.Abs(context.Background(), strings.TrimSpace(directory))
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
	return DirectoryListing{Pane: paneID, Endpoint: endpointName, Path: abs, Entries: model}, nil
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
	if a.store == nil || a.locking || expected.generation != a.generation || current == nil || current.endpoint != expected.endpoint {
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
	if err != nil {
		return DropPreview{}, err
	}
	destinationDirectory, err = destination.endpoint.Abs(context.Background(), destinationDirectory)
	if err != nil {
		return DropPreview{}, err
	}
	target := destination.endpoint.Join(destinationDirectory, entry.Name)
	_, statErr := destination.endpoint.Stat(context.Background(), target)
	conflict := statErr == nil
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
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
	return a.submitFor(source.generation, jobs.Job{Description: description, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		var knownBytes int64
		var knownFiles int
		operation := transfer.Operation{Source: source.endpoint, Destination: destination.endpoint, SourcePath: request.SourcePath, TargetPath: request.TargetPath, Move: request.Move, Overwrite: request.Overwrite, Progress: func(progress transfer.Progress) {
			bytesTotal, filesTotal := progress.BytesTotal, progress.FilesTotal
			if bytesTotal == 0 {
				bytesTotal = knownBytes
			}
			if filesTotal == 0 {
				filesTotal = knownFiles
			}
			fraction := 0.0
			progressKnown := progress.BytesDone > 0 && bytesTotal > 0
			if progressKnown {
				fraction = float64(progress.BytesDone) / float64(bytesTotal)
			}
			emit(jobs.Update{Progress: fraction, ProgressKnown: progressKnown, Indeterminate: !progressKnown, Stage: progress.Stage, Method: progress.Method, BytesDone: progress.BytesDone, BytesTotal: bytesTotal, FilesDone: progress.FilesDone, FilesTotal: filesTotal, Message: fmt.Sprintf("%s · %s · %d/%d bytes", progress.Stage, progress.Path, progress.BytesDone, bytesTotal)})
		}}
		preflight, err := transfer.Preflight(ctx, operation)
		if err != nil {
			return fmt.Errorf("传输预检失败: %w", err)
		}
		knownBytes, knownFiles = preflight.Source.Bytes, len(preflight.Source.Items)
		emit(jobs.Update{ProgressKnown: true, Progress: 0, Stage: "preflight", BytesTotal: knownBytes, FilesTotal: knownFiles, Message: fmt.Sprintf("预检完成 · %d 项 · %d bytes · 同机=%t · 源=%s · 目标=%s", knownFiles, knownBytes, preflight.SameMachine, preflight.SourceCapabilities.Architecture, preflight.TargetCapabilities.Architecture)})
		attempts, approve := a.transferAttempts(operation, preflight)
		err = strategy.Execute(ctx, attempts, approve, func(event strategy.Event) {
			message := fmt.Sprintf("策略 %s · %s", event.Attempt.Tier, event.Attempt.Method)
			if event.Error != nil {
				message += " · " + event.Error.Error()
			}
			emit(jobs.Update{ProgressKnown: false, Progress: 0, Indeterminate: true, Stage: event.Stage, Method: fmt.Sprintf("%s · %s · %s", event.Attempt.Tier, event.Attempt.Direction, event.Attempt.Method), BytesTotal: knownBytes, FilesTotal: knownFiles, Message: message})
		})
		if err == nil {
			emit(jobs.Update{ProgressKnown: true, Progress: 1, Stage: "done", BytesDone: knownBytes, BytesTotal: knownBytes, FilesDone: knownFiles, FilesTotal: knownFiles, Message: fmt.Sprintf("传输完成 · %d 项 · %d bytes", knownFiles, knownBytes)})
		}
		return err
	}})
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
	}})
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
	}})
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
	directory, name := "", target
	switch target {
	case "左栏":
		pane, err := a.pane(LeftPane)
		if err != nil {
			return "", err
		}
		selected, directory, name = pane.endpoint, pane.path, pane.name
	case "右栏":
		pane, err := a.pane(RightPane)
		if err != nil {
			return "", err
		}
		selected, directory, name = pane.endpoint, pane.path, pane.name
	case "控制机":
		selected, name = endpoint.NewLocal(), "控制机"
	default:
		return "", fmt.Errorf("未知命令目标 %q", target)
	}
	return a.submitFor(generation, jobs.Job{Description: "命令 · " + name, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		var output commandOutput
		err := selected.Exec(ctx, command, endpoint.ExecOptions{Directory: directory, Stdout: &output, Stderr: &output})
		if output.String() != "" {
			emit(jobs.Update{Message: output.String()})
		}
		return err
	}})
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
	return ConfigTexts{Markdown: configtext.Markdown(a.document)}, nil
}

func (a *App) SaveConfigTexts(markdown string) (BootstrapModel, error) {
	a.mu.RLock()
	if a.store == nil || a.locking {
		a.mu.RUnlock()
		return BootstrapModel{}, errors.New("保险库尚未解锁")
	}
	generation := a.generation
	old := a.document.Clone()
	a.mu.RUnlock()
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
	a.mu.Lock()
	if a.store == nil || a.locking || generation != a.generation {
		a.mu.Unlock()
		return BootstrapModel{}, errors.New("会话已改变；未保存配置")
	}
	a.document.Hosts, a.document.SOCKS, a.document.Keys = hosts, proxies, privateKeys
	a.mu.Unlock()
	if err := a.save(); err != nil {
		return BootstrapModel{}, err
	}
	return a.Bootstrap()
}
