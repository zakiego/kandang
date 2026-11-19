// Package queue limits how many runs happen at once. A fixed number run, a
// fixed number wait in line (first come, first served), and anyone past that
// is told the service is busy.
package queue

import (
	"context"
	"errors"
	"sync"
)

// ErrBusy means every run slot and every waiting place is taken.
var ErrBusy = errors.New("queue: busy")

// Queue hands out run slots.
type Queue struct {
	mu      sync.Mutex
	running int
	maxRun  int
	maxWait int
	waiters []chan struct{} // FIFO; a closed channel means "your turn"
}

// New returns a queue that runs maxRun at once and lets maxWait wait.
func New(maxRun, maxWait int) *Queue {
	if maxRun < 1 {
		maxRun = 1
	}
	if maxWait < 0 {
		maxWait = 0
	}
	return &Queue{maxRun: maxRun, maxWait: maxWait}
}

// Stats is a snapshot for /health.
type Stats struct {
	Running int `json:"running"`
	Waiting int `json:"waiting"`
}

// Stats returns how many runs hold a slot and how many wait.
func (q *Queue) Stats() Stats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Stats{Running: q.running, Waiting: len(q.waiters)}
}

// Acquire takes a run slot, waiting in line when all are taken. It returns
// ErrBusy at once when the line is full, or ctx's error if ctx ends while
// waiting. On success, call the returned release exactly once.
func (q *Queue) Acquire(ctx context.Context) (release func(), err error) {
	q.mu.Lock()
	if q.running < q.maxRun && len(q.waiters) == 0 {
		q.running++
		q.mu.Unlock()
		return q.releaser(), nil
	}
	if len(q.waiters) >= q.maxWait {
		q.mu.Unlock()
		return nil, ErrBusy
	}
	ch := make(chan struct{})
	q.waiters = append(q.waiters, ch)
	q.mu.Unlock()

	select {
	case <-ch:
		return q.releaser(), nil // the slot was handed over by release
	case <-ctx.Done():
		q.mu.Lock()
		defer q.mu.Unlock()
		select {
		case <-ch:
			// Handed a slot just as ctx ended: give it to the next in line.
			q.handOff()
		default:
			q.remove(ch)
		}
		return nil, ctx.Err()
	}
}

func (q *Queue) releaser() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			q.mu.Lock()
			defer q.mu.Unlock()
			q.handOff()
		})
	}
}

// handOff passes a finished slot to the first waiter, or frees it. The caller
// holds q.mu.
func (q *Queue) handOff() {
	if len(q.waiters) > 0 {
		ch := q.waiters[0]
		q.waiters = q.waiters[1:]
		close(ch) // running stays the same: the slot changes hands
		return
	}
	q.running--
}

func (q *Queue) remove(ch chan struct{}) {
	for i, w := range q.waiters {
		if w == ch {
			q.waiters = append(q.waiters[:i], q.waiters[i+1:]...)
			return
		}
	}
}
