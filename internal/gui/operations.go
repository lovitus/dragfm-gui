package gui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	flytransfer "github.com/flyssh/flyssh/pkg/transfer"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func (c *controller) confirmDrop(source, destination *filePane, destinationDirectory string) {
	entry, ok := source.selectedEntry()
	if !ok {
		return
	}
	if destinationDirectory == "" {
		destinationDirectory = destination.path.Text
	}
	target := destination.currentEndpoint().Join(destinationDirectory, entry.Name)
	go func() {
		_, err := destination.currentEndpoint().Stat(context.Background(), target)
		conflict := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			c.reportError("检查目标冲突", err)
			return
		}
		fyne.Do(func() { c.showDropDialog(source, destination, entry.Path, target, entry.Name, conflict) })
	}()
}

func (c *controller) showDropDialog(source, destination *filePane, sourcePath, targetPath, name string, conflict bool) {
	title := "选择拖放操作"
	copyText, moveText := "复制", "移动"
	if conflict {
		title, copyText, moveText = "目标已存在", "复制并覆盖", "移动并覆盖"
	}
	message := widget.NewLabel(fmt.Sprintf("%s\n%s → %s", name, source.endpointName, destination.endpointName))
	message.Wrapping = fyne.TextWrapWord
	var popup dialog.Dialog
	copyButton := widget.NewButton(copyText, func() {
		popup.Hide()
		c.enqueueTransfer(source, destination, sourcePath, targetPath, false, conflict)
	})
	copyButton.Importance = widget.HighImportance
	moveButton := widget.NewButton(moveText, func() {
		popup.Hide()
		c.enqueueTransfer(source, destination, sourcePath, targetPath, true, conflict)
	})
	moveButton.Importance = widget.WarningImportance
	cancel := widget.NewButton("取消", func() { popup.Hide() })
	buttons := container.NewGridWrap(fyne.NewSize(185, 54), copyButton, moveButton, cancel)
	popup = dialog.NewCustomWithoutButtons(title, container.NewVBox(message, widget.NewSeparator(), buttons), c.window)
	popup.Resize(fyne.NewSize(620, 190))
	popup.Show()
}

func (c *controller) enqueueTransfer(source, destination *filePane, sourcePath, targetPath string, move, overwrite bool) {
	verb := "复制"
	if move {
		verb = "移动"
	}
	description := fmt.Sprintf("%s · %s:%s → %s:%s", verb, source.endpointName, sourcePath, destination.endpointName, targetPath)
	_, err := c.queue.Submit(jobs.Job{Description: description, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		operation := transfer.Operation{
			Source: source.currentEndpoint(), Destination: destination.currentEndpoint(),
			SourcePath: sourcePath, TargetPath: targetPath, Move: move, Overwrite: overwrite,
			Progress: func(progress transfer.Progress) {
				fraction := float64(0)
				if progress.BytesTotal > 0 {
					fraction = float64(progress.BytesDone) / float64(progress.BytesTotal)
				}
				emit(jobs.Update{Progress: fraction, Message: fmt.Sprintf("%s · %s · %s / %s", progress.Stage, progress.Path, formatBytes(progress.BytesDone), formatBytes(progress.BytesTotal))})
			},
		}
		attempts := c.transferAttempts(ctx, operation, emit)
		return strategy.Execute(ctx, attempts, nil, func(event strategy.Event) {
			message := fmt.Sprintf("策略 %s · %s", event.Attempt.Tier, event.Attempt.Method)
			if event.Error != nil {
				message += " · " + event.Error.Error()
			}
			emit(jobs.Update{Message: message})
		})
	}})
	if err != nil {
		c.reportError("传输入队失败", err)
	}
}

func (c *controller) transferAttempts(ctx context.Context, operation transfer.Operation, emit func(jobs.Update)) []strategy.Attempt {
	var attempts []strategy.Attempt
	if remote, direction, ok := localRemotePair(operation); ok {
		attempts = append(attempts, strategy.Attempt{Tier: strategy.Direct, Direction: direction, Method: strategy.SCP, Run: func(runContext context.Context) error {
			return runFlySSHSCP(runContext, remote, operation)
		}})
	}
	attempts = append(attempts, strategy.Attempt{Tier: strategy.ControllerRelay, Direction: strategy.SourcePush, Method: strategy.MemoryStream, Run: func(runContext context.Context) error {
		_, err := transfer.Run(runContext, operation)
		return err
	}})
	return attempts
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
	if _, err := operation.Destination.Stat(ctx, operation.TargetPath); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return errors.New("目标已存在，SCP 原子路径跳过")
	}
	before, err := transfer.Snapshot(ctx, operation.Source, operation.SourcePath, operation.Move)
	if err != nil {
		return err
	}
	for _, item := range before.Items {
		if item.Mode&fs.ModeSymlink != 0 {
			return errors.New("SCP 不保证符号链接语义，改用内存流")
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
		return fmt.Errorf("SCP 后 SHA-256 校验失败，源已保留: %w", err)
	}
	return operation.Source.Remove(ctx, operation.SourcePath, before.Items[0].Mode.IsDir())
}

func (c *controller) confirmDelete(pane *filePane) {
	entry, ok := pane.selectedEntry()
	if !ok {
		return
	}
	dialog.ShowConfirm("确认删除", fmt.Sprintf("删除 %s？\n%s", entry.Name, entry.Path), func(confirmed bool) {
		if !confirmed {
			return
		}
		_, err := c.queue.Submit(jobs.Job{Description: "删除 · " + pane.endpointName + ":" + entry.Path, Run: func(ctx context.Context, emit func(jobs.Update)) error {
			return pane.currentEndpoint().Remove(ctx, entry.Path, entry.IsDir())
		}})
		if err != nil {
			c.reportError("删除入队失败", err)
		}
	}, c.window)
}

func (c *controller) enqueueHash(pane *filePane) {
	entry, ok := pane.selectedEntry()
	if !ok {
		return
	}
	_, err := c.queue.Submit(jobs.Job{Description: "SHA-256 · " + pane.endpointName + ":" + entry.Path, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		manifest, err := transfer.Snapshot(ctx, pane.currentEndpoint(), entry.Path, true)
		if err != nil {
			return err
		}
		var lines []string
		for _, item := range manifest.Items {
			if item.SHA256 != "" {
				name := item.Relative
				if name == "" {
					name = entry.Name
				}
				lines = append(lines, item.SHA256+"  "+name)
			}
		}
		emit(jobs.Update{Message: strings.Join(lines, "\n")})
		return nil
	}})
	if err != nil {
		c.reportError("哈希入队失败", err)
	}
}
