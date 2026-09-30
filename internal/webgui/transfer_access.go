package webgui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

type transferAccessKey struct{}

// One queue job owns its two permission decisions, two file views and one
// source baseline. Direction changes never swap those endpoint identities.
type transferAccess struct {
	app       *App
	original  transfer.Operation
	files     map[strategy.Direction]endpoint.Endpoint
	passwords map[strategy.Direction]string
	approved  map[strategy.Direction]bool
	cleanup   []func() error
	baseline  *transfer.Manifest
}

func newTransferAccess(ctx context.Context, app *App, operation transfer.Operation) (context.Context, *transferAccess) {
	access := &transferAccess{app: app, original: operation, files: make(map[strategy.Direction]endpoint.Endpoint), passwords: make(map[strategy.Direction]string), approved: make(map[strategy.Direction]bool)}
	ctx = context.WithValue(ctx, transferAccessKey{}, access)
	return strategy.WithApproval(ctx, access.approve), access
}

func accessFor(ctx context.Context) *transferAccess {
	access, _ := ctx.Value(transferAccessKey{}).(*transferAccess)
	return access
}

func (a *transferAccess) close() error {
	var err error
	for i := len(a.cleanup) - 1; i >= 0; i-- {
		err = errors.Join(err, a.cleanup[i]())
	}
	return err
}

func (a *transferAccess) endpoint(side strategy.Direction) endpoint.Endpoint {
	if a.files[side] != nil {
		return a.files[side]
	}
	if side == strategy.SourcePush {
		return a.original.Source
	}
	return a.original.Destination
}

func fileOperation(ctx context.Context, operation transfer.Operation) transfer.Operation {
	if a := accessFor(ctx); a != nil {
		operation.Source, operation.Destination = a.endpoint(strategy.SourcePush), a.endpoint(strategy.TargetPull)
		operation.Baseline = a.baseline
		operation.PreserveOwner = a.files[strategy.TargetPull] != nil
	}
	return operation
}

func sideRisk(side strategy.Direction) strategy.Risk {
	if side == strategy.SourcePush {
		return strategy.SourceSudoRisk
	}
	return strategy.TargetSudoRisk
}

func needsElevation(ctx context.Context, side strategy.Direction, requested bool) bool {
	access := accessFor(ctx)
	return requested || (access != nil && access.files[side] != nil)
}

func (a *transferAccess) approve(ctx context.Context, risk strategy.Risk, attempt strategy.Attempt) error {
	side := strategy.SourcePush
	if risk == strategy.TargetSudoRisk || risk == strategy.SudoRisk {
		side = strategy.TargetPull
	}
	if risk != strategy.SourceSudoRisk && risk != strategy.TargetSudoRisk && risk != strategy.SudoRisk {
		return a.app.transferApproval("", nil, a.original, a.passwords)(ctx, risk, attempt)
	}
	if a.approved[side] {
		return nil
	}
	if _, local := a.endpoint(side).(*endpoint.Local); local {
		label, path := "源端提权", a.original.SourcePath
		if side == strategy.TargetPull {
			label, path = "目标端提权", a.original.TargetPath
		}
		message := "本次传输需要对以下路径使用 sudo：\n" + path + "\n只执行本任务的文件操作，不打开 root Shell。留空尝试 sudo -n。"
		if side == strategy.TargetPull {
			message += "目标将按源端数字 UID/GID 保留可获取的所有权，完成后普通用户可能无法读取。"
		}
		answer, accepted := a.app.ask(ctx, ChallengeModel{Kind: "password", Title: label + " · 本机", Message: message, Secret: true, AllowSkip: true})
		if !accepted {
			return declinedRisk(answer)
		}
		files := endpoint.NewSudoLocal(answer.Value)
		if err := files.Check(ctx); err != nil {
			return fmt.Errorf("本机 sudo 验证: %w", err)
		}
		a.files[side] = files
	} else {
		if err := a.app.transferApproval("", nil, a.original, a.passwords)(ctx, sideRisk(side), attempt); err != nil {
			return err
		}
	}
	a.approved[side] = true
	return nil
}

func (a *transferAccess) elevate(ctx context.Context, side strategy.Direction) error {
	if a.files[side] != nil {
		return errors.New("已获准的高权端点仍拒绝访问；未进一步扩大权限")
	}
	if err := strategy.Authorize(ctx, strategy.Attempt{Direction: side, Elevated: true}, sideRisk(side)); err != nil {
		return err
	}
	if a.files[side] != nil {
		return nil
	} // local approval creates its scoped view
	remote, ok := a.endpoint(side).(*endpoint.Remote)
	if !ok {
		return errors.New("端点不支持受控提权")
	}
	var architecture bytes.Buffer
	err := remote.Exec(ctx, "uname -m", endpoint.ExecOptions{Stdout: &architecture})
	if err == nil {
		agent, cleanup, startErr := a.app.startTransferAgent(ctx, remote, strings.TrimSpace(architecture.String()), true, a.passwords[side])
		err = startErr
		if err == nil {
			files, fileErr := agent.OpenFiles(ctx)
			if fileErr == nil {
				a.files[side] = files
				a.cleanup = append(a.cleanup, cleanup)
				return nil
			}
			err = fileErr
			finishAgentCleanup(&err, cleanup)
		}
	} else {
		err = fmt.Errorf("探测提权端架构: %w", err)
	}
	// Startup can itself leave an unconfirmed remote process or installation.
	// The same stop condition applies whether startup or OpenFiles failed;
	// a working system fallback must not erase the earlier unsafe state.
	if !transfer.Retryable(err) {
		return err
	}
	if ctx.Err() != nil {
		return errors.Join(ctx.Err(), err)
	}
	system, closeSystem, systemErr := a.app.openSystemFiles(ctx, remote, a.passwords[side])
	if systemErr != nil {
		return errors.Join(fmt.Errorf("uploaded helper filesystem: %w", err), fmt.Errorf("system filesystem fallback: %w", systemErr))
	}
	a.files[side] = system.Files
	a.cleanup = append(a.cleanup, closeSystem)
	return nil
}

func (a *transferAccess) inspect(ctx context.Context, side strategy.Direction, inspect func(endpoint.Endpoint) error) error {
	err := inspect(a.endpoint(side))
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	if elevateErr := a.elevate(ctx, side); elevateErr != nil {
		return errors.Join(err, elevateErr)
	}
	return inspect(a.endpoint(side))
}

func (a *transferAccess) preflight(ctx context.Context) (transfer.PreflightReport, error) {
	op := a.original
	if op.Source.Dir(op.SourcePath) == op.SourcePath || op.Destination.Dir(op.TargetPath) == op.TargetPath {
		return transfer.PreflightReport{}, errors.New("拒绝传输文件系统根目录")
	}
	// Readability is checked before taking the move baseline. A protected file
	// must not be represented by an incomplete placeholder manifest.
	var source transfer.Manifest
	if err := a.inspect(ctx, strategy.SourcePush, func(ep endpoint.Endpoint) error {
		var err error
		source, err = transfer.Snapshot(ctx, ep, op.SourcePath, op.Move)
		if err != nil {
			return err
		}
		if !op.Move {
			for _, item := range source.Items {
				if !item.Mode.IsRegular() {
					continue
				}
				reader, err := ep.Open(ctx, item.SourcePath)
				if err != nil {
					return err
				}
				if err := reader.Close(); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return transfer.PreflightReport{}, fmt.Errorf("源端预检: %w", err)
	}
	if err := a.inspect(ctx, strategy.TargetPull, func(ep endpoint.Endpoint) error {
		_, err := ep.Stat(ctx, op.TargetPath)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}); err != nil {
		return transfer.PreflightReport{}, fmt.Errorf("目标端预检: %w", err)
	}
	a.baseline = &source
	report, err := transfer.Preflight(ctx, fileOperation(ctx, op))
	if err == nil {
		report.Source = source
		report.SourceInspectionDenied = a.files[strategy.SourcePush] != nil
		report.TargetInspectionDenied = a.files[strategy.TargetPull] != nil
	}
	return report, err
}

func (a *transferAccess) relay(ctx context.Context) error {
	for attempt := 0; attempt < 3; attempt++ {
		result, err := transfer.Run(ctx, fileOperation(ctx, a.original))
		if err == nil {
			return nil
		}
		var denied *transfer.AccessError
		if !errors.As(err, &denied) {
			return err
		}
		side := strategy.TargetPull
		if denied.Source {
			side = strategy.SourcePush
		}
		if a.files[side] != nil {
			return err
		}
		if !result.SourceKept && !transfer.Retryable(err) {
			return err
		}
		if upgradeErr := a.elevate(ctx, side); upgradeErr != nil {
			return errors.Join(err, upgradeErr)
		}
		if result.SourceKept {
			return a.finishMove(ctx, *a.baseline)
		}
	}
	return errors.New("高权传输仍然失败")
}

func (a *transferAccess) finishMove(ctx context.Context, before transfer.Manifest) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := transfer.FinishMove(ctx, fileOperation(ctx, a.original), before)
		if err == nil {
			return nil
		}
		var denied *transfer.AccessError
		if !errors.As(err, &denied) {
			return err
		}
		side := strategy.TargetPull
		if denied.Source {
			side = strategy.SourcePush
		}
		if a.files[side] != nil {
			return err
		}
		if upgradeErr := a.elevate(ctx, side); upgradeErr != nil {
			return transfer.PreserveSource(errors.Join(err, upgradeErr))
		}
	}
	return transfer.PreserveSource(errors.New("已复制但未移动；高权校验或删除仍失败"))
}

func (a *transferAccess) finishTransfer(ctx context.Context, before transfer.Manifest, elevatedTarget bool) error {
	if elevatedTarget && a.files[strategy.TargetPull] == nil {
		if err := a.elevate(ctx, strategy.TargetPull); err != nil {
			return transfer.PreserveSource(err)
		}
	}
	op := fileOperation(ctx, a.original)
	if err := transfer.RestoreOwnership(ctx, op, before); err != nil {
		return transfer.PreserveSource(fmt.Errorf("数据已复制，但目标所有权恢复失败，源文件已保留: %w", err))
	}
	if op.Move {
		return a.finishMove(ctx, before)
	}
	return nil
}

func declinedRisk(answer challengeAnswer) error {
	if answer.Skipped {
		return strategy.ErrRiskSkipped
	}
	return fmt.Errorf("已取消任务，未授权新的高权操作: %w", context.Canceled)
}
