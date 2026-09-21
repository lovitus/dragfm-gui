package strategy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type Tier string

const (
	SameHost        Tier = "same-host"
	Direct          Tier = "direct"
	SOCKSPool       Tier = "socks-pool"
	JumpPool        Tier = "jump-pool"
	Hans            Tier = "hans"
	ControllerRelay Tier = "controller-memory-relay"
)

type Method string

const (
	Rsync           Method = "rsync"
	SCP             Method = "scp"
	EncryptedStream Method = "encrypted-stream"
	NcatTar         Method = "ncat-tar"
	MemoryStream    Method = "memory-stream"
)

type Direction string

const (
	SourcePush Direction = "source-push"
	TargetPull Direction = "target-pull"
)

type Attempt struct {
	Tier      Tier
	RouteName string
	Direction Direction
	Elevated  bool
	Method    Method
	Risk      Risk
	Run       func(context.Context) error
}

type Risk string

const (
	Ordinary       Risk = "ordinary"
	SudoRisk       Risk = "sudo"
	SourceSudoRisk Risk = "source-sudo"
	TargetSudoRisk Risk = "target-sudo"
	ListenRisk     Risk = "listener"
	HansRisk       Risk = "hans"
	PlaintextRisk  Risk = "plaintext-listener"
)

type Event struct {
	Attempt Attempt
	Stage   string
	Error   error
	Started time.Time
	Elapsed time.Duration
}

type Approval func(context.Context, Risk, Attempt) error

func Execute(ctx context.Context, attempts []Attempt, approve Approval, emit func(Event)) error {
	approved := make(map[Risk]bool)
	var failures []error
	for _, attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt.Run == nil {
			continue
		}
		if attempt.Risk != "" && attempt.Risk != Ordinary && !approved[attempt.Risk] {
			if approve == nil {
				failures = append(failures, fmt.Errorf("%s requires approval", attempt.Risk))
				continue
			}
			if err := approve(ctx, attempt.Risk, attempt); err != nil {
				return err
			}
			approved[attempt.Risk] = true
		}
		started := time.Now()
		if emit != nil {
			emit(Event{Attempt: attempt, Stage: "running", Started: started})
		}
		err := attempt.Run(ctx)
		if emit != nil {
			emit(Event{Attempt: attempt, Stage: map[bool]string{true: "failed", false: "succeeded"}[err != nil], Error: err, Started: started, Elapsed: time.Since(started)})
		}
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		var retry interface{ Retryable() bool }
		if errors.As(err, &retry) && !retry.Retryable() {
			return err
		}
		failures = append(failures, fmt.Errorf("%s/%s/%s: %w", attempt.Tier, attempt.Direction, attempt.Method, err))
	}
	return fmt.Errorf("所有传输路径均失败: %w", errors.Join(failures...))
}

type ProbeResult struct {
	Name       string
	Samples    []time.Duration
	LastOK     time.Time
	Successful bool
}

func SortProbes(results []ProbeResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Successful != results[j].Successful {
			return results[i].Successful
		}
		left, right := median(results[i].Samples), median(results[j].Samples)
		if left != right {
			return left < right
		}
		return results[i].LastOK.After(results[j].LastOK)
	})
}

func median(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return time.Duration(1<<63 - 1)
	}
	copy := append([]time.Duration(nil), values...)
	sort.Slice(copy, func(i, j int) bool { return copy[i] < copy[j] })
	return copy[len(copy)/2]
}
