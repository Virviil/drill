package judge

import (
	"context"
	"errors"
	"sync"
)

// ErrBusy is returned when the user already has a job in flight.
var ErrBusy = errors.New("у тебя уже есть проверка в очереди")

// Queue runs jobs on a fixed pool of workers, one active job per user.
type Queue struct {
	sb     Sandbox
	jobs   chan queued
	mu     sync.Mutex
	active map[int64]bool
}

type queued struct {
	job  Job
	done chan Result
}

// NewQueue starts workers that stop when ctx is cancelled.
func NewQueue(ctx context.Context, sb Sandbox, workers int) *Queue {
	q := &Queue{sb: sb, jobs: make(chan queued, 64), active: map[int64]bool{}}
	for i := 0; i < workers; i++ {
		go q.worker(ctx)
	}
	return q
}

// Submit enqueues a job; the result arrives on the returned channel.
func (q *Queue) Submit(job Job) (<-chan Result, error) {
	q.mu.Lock()
	if q.active[job.UserID] {
		q.mu.Unlock()
		return nil, ErrBusy
	}
	q.active[job.UserID] = true
	q.mu.Unlock()

	done := make(chan Result, 1)
	select {
	case q.jobs <- queued{job: job, done: done}:
		return done, nil
	default:
		q.release(job.UserID)
		return nil, errors.New("очередь судьи переполнена, попробуй через минуту")
	}
}

func (q *Queue) release(uid int64) {
	q.mu.Lock()
	delete(q.active, uid)
	q.mu.Unlock()
}

func (q *Queue) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case it := <-q.jobs:
			res := Check(ctx, q.sb, it.job)
			q.release(it.job.UserID)
			it.done <- res
		}
	}
}
