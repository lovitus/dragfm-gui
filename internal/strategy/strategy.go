package strategy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
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
	Group     bool // Pool/tunnel orchestration; not a concrete copy method.
	Tier      Tier
	RouteName string
	Direction Direction
	Elevated  bool
	Method    Method
	Risk      Risk
	Risks     []Risk
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

type observerKey struct{}

// Observe exposes the concrete method inside a pool/tunnel through the same
// job observer as Execute. It does not change retry or approval decisions.
func Observe(ctx context.Context, attempt Attempt, run func(context.Context) error) error {
	observer, _ := ctx.Value(observerKey{}).(func(Event))
	started := time.Now()
	if observer != nil {
		observer(Event{Attempt: attempt, Stage: "running", Started: started})
	}
	err := run(ctx)
	if observer != nil {
		stage := "succeeded"
		if err != nil {
			stage = "failed"
		}
		observer(Event{Attempt: attempt, Stage: stage, Error: err, Started: started, Elapsed: time.Since(started)})
	}
	return err
}

var ErrRiskSkipped = errors.New("已跳过本任务中需要这类权限的方法")

type approvalKey struct{}
type approvals struct {
	mu        sync.Mutex
	decisions map[Risk]error
	ask       Approval
}

// Share decisions with nested SOCKS/jump/Hans attempts. Approving a listener
// never implies sudo, and declining one class must not cancel unrelated routes.
func WithApproval(ctx context.Context, approve Approval) context.Context {
	return context.WithValue(ctx, approvalKey{}, &approvals{decisions: make(map[Risk]error), ask: approve})
}

func Authorize(ctx context.Context, attempt Attempt, risks ...Risk) error {
	state, ok := ctx.Value(approvalKey{}).(*approvals)
	for _, risk := range risks {
		if risk == "" || risk == Ordinary {
			continue
		}
		if !ok {
			return fmt.Errorf("%s requires task approval: %w", risk, ErrRiskSkipped)
		}
		state.mu.Lock()
		decision, known := state.decisions[risk]
		if !known {
			if state.ask == nil {
				decision = fmt.Errorf("%s requires approval: %w", risk, ErrRiskSkipped)
			} else {
				decision = state.ask(ctx, risk, attempt)
			}
			if decision == nil || errors.Is(decision, ErrRiskSkipped) {
				state.decisions[risk] = decision
			}
		}
		state.mu.Unlock()
		if decision != nil {
			return decision
		}
	}
	return nil
}

func Execute(ctx context.Context, attempts []Attempt, approve Approval, emit func(Event)) error {
	if emit != nil {
		ctx = context.WithValue(ctx, observerKey{}, emit)
	}
	if _, ok := ctx.Value(approvalKey{}).(*approvals); !ok {
		ctx = WithApproval(ctx, approve)
	}
	var failures []error
	for _, attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt.Run == nil {
			continue
		}
		if err := Authorize(ctx, attempt, append([]Risk{attempt.Risk}, attempt.Risks...)...); err != nil {
			if emit != nil {
				emit(Event{Attempt: attempt, Stage: "skipped", Error: err})
			}
			if errors.Is(err, ErrRiskSkipped) {
				failures = append(failures, err)
				continue
			}
			return err
		}
		err := Observe(ctx, attempt, attempt.Run)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		var retry interface{ Retryable() bool }
		if errors.As(err, &retry) && !retry.Retryable() {
			return err
		}
		// A route-local dial/handshake deadline is a failed route, not a
		// cancellation of the user's job. When the parent is still live, let
		// the next direction or proxy recover after the attempt has cleaned up.
		failures = append(failures, fmt.Errorf("%s/%s/%s: %w", attempt.Tier, attempt.Direction, attempt.Method, err))
	}
	if len(failures) == 0 {
		return errors.New("没有可用的传输路径")
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
