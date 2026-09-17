package jobs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type State string

const (
	Pending   State = "pending"
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

type Job struct {
	ID          string
	Description string
	Run         func(context.Context, func(Update)) error
}

type Update struct {
	ID, Description, Message string
	State                    State
	Revision                 uint64
	Progress                 float64
	ProgressKnown            bool
	Indeterminate            bool
	Stage, Method            string
	BytesDone, BytesTotal    int64
	FilesDone, FilesTotal    int
	StartedAt, FinishedAt    time.Time
	Error                    string
}

const historyLimit = 500
const eventLimit = 1024

// Queue has one worker and one event dispatcher. State changes never wait for
// an event consumer, so an overloaded UI cannot prevent cancellation/shutdown.
// Updates is a bounded, best-effort event stream; Snapshot is authoritative and
// retains every active job and the latest 500 completed jobs. Revision permits
// consumers to merge a snapshot with events without resurrecting stale jobs.
type Queue struct {
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	capacity   int
	pending    []Job
	running    string
	runCancel  context.CancelFunc
	latest     map[string]Update
	completed  []string
	events     []Update
	sequence   uint64
	revision   uint64
	closed     bool
	wake       chan struct{}
	eventWake  chan struct{}
	workerDone chan struct{}
	updates    chan Update
}

func New(buffer int) *Queue {
	if buffer < 1 {
		buffer = 64
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		ctx: ctx, cancel: cancel, capacity: buffer, latest: make(map[string]Update),
		wake: make(chan struct{}, 1), eventWake: make(chan struct{}, 1),
		workerDone: make(chan struct{}), updates: make(chan Update, buffer*4),
	}
	go q.loop()
	go q.dispatch()
	return q
}

func signal(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

func (q *Queue) Submit(job Job) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return "", errors.New("job queue is closed")
	}
	if job.Run == nil {
		return "", errors.New("job has no runner")
	}
	if len(q.pending) >= q.capacity {
		return "", errors.New("pending queue is full")
	}
	if job.ID == "" {
		q.sequence++
		job.ID = fmt.Sprintf("job-%d-%d", time.Now().UnixNano(), q.sequence)
	}
	if _, exists := q.latest[job.ID]; exists {
		return "", fmt.Errorf("duplicate job ID %q", job.ID)
	}
	q.pending = append(q.pending, job)
	q.recordLocked(Update{ID: job.ID, Description: job.Description, State: Pending})
	signal(q.wake)
	return job.ID, nil
}

func (q *Queue) Cancel(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running == id && q.runCancel != nil {
		q.runCancel()
		return true
	}
	for index, job := range q.pending {
		if job.ID == id {
			q.pending = append(q.pending[:index], q.pending[index+1:]...)
			q.recordLocked(cancelledUpdate(job))
			return true
		}
	}
	return false
}

func cancelledUpdate(job Job) Update {
	return Update{ID: job.ID, Description: job.Description, State: Cancelled, Message: "已取消", FinishedAt: time.Now()}
}

func (q *Queue) cancelAllLocked() {
	if q.runCancel != nil {
		q.runCancel()
	}
	for _, job := range q.pending {
		q.recordLocked(cancelledUpdate(job))
	}
	q.pending = nil
}

func (q *Queue) CancelAll() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cancelAllLocked()
}

func (q *Queue) Updates() <-chan Update { return q.updates }

// Done closes after the running job has returned and its final state is in
// Snapshot. Close is nonblocking; callers needing durable history must wait on
// Done and persist Snapshot as well as consuming Updates.
func (q *Queue) Done() <-chan struct{} { return q.workerDone }

func (q *Queue) Closed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	q.cancelAllLocked()
	q.cancel()
	signal(q.wake)
}

func (q *Queue) Snapshot() []Update {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]Update, 0, len(q.latest))
	for _, update := range q.latest {
		result = append(result, update)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Revision < result[j].Revision })
	return result
}

func (q *Queue) recordLocked(update Update) {
	q.revision++
	update.Revision = q.revision
	q.latest[update.ID] = update
	if update.State == Succeeded || update.State == Failed || update.State == Cancelled {
		q.completed = append(q.completed, update.ID)
		if len(q.completed) > historyLimit {
			delete(q.latest, q.completed[0])
			q.completed = q.completed[1:]
		}
	}
	if len(q.events) == eventLimit {
		// Keep memory bounded if the window is suspended. Snapshot repairs the
		// view on resume; the worker and cancellation must remain responsive.
		copy(q.events, q.events[1:])
		q.events = q.events[:eventLimit-1]
	}
	q.events = append(q.events, update)
	signal(q.eventWake)
}

func (q *Queue) loop() {
	defer close(q.workerDone)
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return
		}
		if len(q.pending) == 0 {
			q.mu.Unlock()
			<-q.wake
			continue
		}
		job := q.pending[0]
		q.pending[0] = Job{}
		q.pending = q.pending[1:]
		ctx, cancel := context.WithCancel(q.ctx)
		// Removal from Pending and registration as Running are atomic: Cancel
		// can never fall into the gap between these states.
		q.running, q.runCancel = job.ID, cancel
		q.recordLocked(Update{ID: job.ID, Description: job.Description, State: Running,
			Indeterminate: true, Stage: "starting", StartedAt: time.Now()})
		q.mu.Unlock()
		q.execute(ctx, job)
		cancel()
	}
}

func (q *Queue) execute(ctx context.Context, job Job) {
	emit := func(update Update) {
		q.mu.Lock()
		defer q.mu.Unlock()
		if q.running != job.ID {
			return // Ignore a late callback after the runner has finished.
		}
		last := q.latest[job.ID]
		update.ID, update.Description, update.State = job.ID, job.Description, Running
		update.StartedAt = last.StartedAt
		if update.Message == "" {
			update.Message = last.Message
		}
		if !update.ProgressKnown && !update.Indeterminate {
			update.Progress, update.ProgressKnown, update.Indeterminate = last.Progress, last.ProgressKnown, last.Indeterminate
		}
		if update.Stage == "" {
			update.Stage = last.Stage
		}
		if update.Method == "" {
			update.Method = last.Method
		}
		if update.BytesDone == 0 && update.BytesTotal == 0 {
			update.BytesDone, update.BytesTotal = last.BytesDone, last.BytesTotal
		}
		if update.FilesDone == 0 && update.FilesTotal == 0 {
			update.FilesDone, update.FilesTotal = last.FilesDone, last.FilesTotal
		}
		q.recordLocked(update)
	}
	err := runSafely(ctx, job.Run, emit)
	q.mu.Lock()
	defer q.mu.Unlock()
	final := q.latest[job.ID]
	q.running, q.runCancel = "", nil
	final.State, final.FinishedAt, final.Indeterminate = Succeeded, time.Now(), false
	if final.Message == "" {
		final.Message = "完成"
	}
	if errors.Is(err, context.Canceled) || (err != nil && ctx.Err() != nil) {
		final.State, final.Message = Cancelled, "已取消"
	} else if err != nil {
		final.State, final.Message = Failed, err.Error()
	}
	if final.State == Succeeded {
		final.Progress, final.ProgressKnown = 1, true
	}
	if err != nil {
		final.Error = err.Error()
	}
	q.recordLocked(final)
}

func runSafely(ctx context.Context, run func(context.Context, func(Update)) error, emit func(Update)) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("job panicked: %v", value)
		}
	}()
	return run(ctx, emit)
}

func (q *Queue) nextEvent() (Update, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.events) == 0 {
		return Update{}, false
	}
	event := q.events[0]
	q.events[0] = Update{}
	q.events = q.events[1:]
	return event, true
}

func (q *Queue) dispatch() {
	defer close(q.updates) // Only this goroutine ever sends or closes Updates.
	for {
		if event, ok := q.nextEvent(); ok {
			select {
			case q.updates <- event:
			case <-q.workerDone:
				select {
				case q.updates <- event:
				default:
					return // Complete final states remain available in Snapshot.
				}
			}
			continue
		}
		select {
		case <-q.eventWake:
		case <-q.workerDone:
			// The worker can have queued its last event after nextEvent.
			for {
				event, ok := q.nextEvent()
				if !ok {
					return
				}
				select {
				case q.updates <- event:
				default:
					return
				}
			}
		}
	}
}
