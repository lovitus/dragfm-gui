package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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
	Progress                 float64
	ProgressKnown            bool
	Indeterminate            bool
	Stage, Method            string
	BytesDone, BytesTotal    int64
	FilesDone, FilesTotal    int
	StartedAt, FinishedAt    time.Time
	Error                    string
}

type Queue struct {
	ctx       context.Context
	cancel    context.CancelFunc
	input     chan Job
	updates   chan Update
	mu        sync.Mutex
	running   map[string]context.CancelFunc
	pending   map[string]string
	cancelled map[string]bool
	sequence  atomic.Uint64
	closed    bool
}

func New(buffer int) *Queue {
	if buffer < 1 {
		buffer = 64
	}
	ctx, cancel := context.WithCancel(context.Background())
	queue := &Queue{ctx: ctx, cancel: cancel, input: make(chan Job, buffer), updates: make(chan Update, buffer*4), running: make(map[string]context.CancelFunc), pending: make(map[string]string), cancelled: make(map[string]bool)}
	go queue.loop()
	return queue
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
	if job.ID == "" {
		job.ID = fmt.Sprintf("job-%d-%d", time.Now().Unix(), q.sequence.Add(1))
	}
	select {
	case q.input <- job:
		q.pending[job.ID] = job.Description
		q.publish(Update{ID: job.ID, Description: job.Description, State: Pending})
		return job.ID, nil
	default:
		return "", errors.New("pending queue is full")
	}
}

func (q *Queue) Cancel(id string) bool {
	q.mu.Lock()
	cancel := q.running[id]
	description, pending := q.pending[id]
	if pending {
		q.cancelled[id] = true
		delete(q.pending, id)
	}
	q.mu.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	if pending {
		q.publish(Update{ID: id, Description: description, State: Cancelled, Message: "已取消", FinishedAt: time.Now()})
	}
	return pending
}

func (q *Queue) CancelAll() {
	q.mu.Lock()
	ids := make([]string, 0, len(q.pending)+len(q.running))
	for id := range q.pending {
		ids = append(ids, id)
	}
	for id := range q.running {
		ids = append(ids, id)
	}
	q.mu.Unlock()
	for _, id := range ids {
		q.Cancel(id)
	}
}

func (q *Queue) Updates() <-chan Update { return q.updates }

func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()
	q.cancel()
}

func (q *Queue) loop() {
	defer close(q.updates)
	for {
		select {
		case <-q.ctx.Done():
			return
		case job := <-q.input:
			q.mu.Lock()
			cancelled := q.cancelled[job.ID]
			delete(q.cancelled, job.ID)
			delete(q.pending, job.ID)
			q.mu.Unlock()
			if cancelled {
				continue
			}
			q.execute(job)
		}
	}
}

func (q *Queue) execute(job Job) {
	ctx, cancel := context.WithCancel(q.ctx)
	q.mu.Lock()
	q.running[job.ID] = cancel
	q.mu.Unlock()
	started := time.Now()
	q.publish(Update{ID: job.ID, Description: job.Description, State: Running, Indeterminate: true, Stage: "starting", StartedAt: started})
	lastMessage := ""
	lastProgress := 0.0
	progressKnown := false
	emit := func(update Update) {
		update.ID, update.Description = job.ID, job.Description
		if update.State == "" {
			update.State = Running
		}
		update.StartedAt = started
		if update.Message != "" {
			lastMessage = update.Message
		}
		if update.ProgressKnown {
			lastProgress, progressKnown = update.Progress, true
		} else if progressKnown {
			update.Progress, update.ProgressKnown = lastProgress, true
		}
		q.publish(update)
	}
	err := job.Run(ctx, emit)
	cancel()
	q.mu.Lock()
	delete(q.running, job.ID)
	q.mu.Unlock()
	state := Succeeded
	message := "完成"
	if lastMessage != "" {
		message = lastMessage
	}
	if errors.Is(err, context.Canceled) {
		state, message = Cancelled, "已取消"
	} else if err != nil {
		state, message = Failed, err.Error()
	}
	update := Update{ID: job.ID, Description: job.Description, State: state, Message: message, StartedAt: started, FinishedAt: time.Now(), Progress: lastProgress, ProgressKnown: progressKnown}
	if state == Succeeded {
		update.Progress, update.ProgressKnown = 1, true
	}
	if err != nil {
		update.Error = err.Error()
	}
	q.publish(update)
}

func (q *Queue) publish(update Update) {
	select {
	case q.updates <- update:
	case <-q.ctx.Done():
	}
}
