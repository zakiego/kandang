package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func waitFor(t *testing.T, q *Queue, want Stats) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for q.Stats() != want {
		if time.Now().After(deadline) {
			t.Fatalf("stats = %+v, want %+v", q.Stats(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTwoRunTenWaitThenBusy(t *testing.T) {
	q := New(2, 10)
	ctx := context.Background()
	var releases []func()
	for range 2 {
		r, err := q.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, r)
	}
	waitFor(t, q, Stats{Running: 2, Waiting: 0})

	got := make(chan int, 10)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := q.Acquire(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			got <- i
			r()
		}()
		waitFor(t, q, Stats{Running: 2, Waiting: i + 1}) // keeps arrival order fixed
	}

	if _, err := q.Acquire(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("11th waiter: err = %v, want ErrBusy", err)
	}

	releases[0]()
	releases[0]() // a second call is a no-op
	releases[1]()
	wg.Wait()
	close(got)
	if n := len(got); n != 10 {
		t.Errorf("%d waiters ran, want 10", n)
	}
	waitFor(t, q, Stats{Running: 0, Waiting: 0})
}

func TestFirstComeFirstServed(t *testing.T) {
	q := New(1, 5)
	hold, _ := q.Acquire(context.Background())
	order := make(chan int, 5)
	var wg sync.WaitGroup
	for i := range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := q.Acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			order <- i // sent while holding the only slot
			r()
		}()
		waitFor(t, q, Stats{Running: 1, Waiting: i + 1})
	}
	hold()
	wg.Wait()
	close(order)
	want := 0
	for i := range order {
		if i != want {
			t.Errorf("waiter %d ran in place %d", i, want)
		}
		want++
	}
}

func TestCanceledWaiterLeavesTheLine(t *testing.T) {
	q := New(1, 2)
	release, _ := q.Acquire(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error)
	go func() {
		_, err := q.Acquire(ctx)
		errc <- err
	}()
	waitFor(t, q, Stats{Running: 1, Waiting: 1})
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	waitFor(t, q, Stats{Running: 1, Waiting: 0})
	release()
	waitFor(t, q, Stats{Running: 0, Waiting: 0})
}

func TestNoWaiting(t *testing.T) {
	q := New(1, 0)
	r, _ := q.Acquire(context.Background())
	if _, err := q.Acquire(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	r()
	if r2, err := q.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	} else {
		r2()
	}
}

func TestStress(t *testing.T) {
	q := New(2, 1000)
	var mu sync.Mutex
	running, peak := 0, 0
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := q.Acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			running++
			peak = max(peak, running)
			mu.Unlock()
			time.Sleep(100 * time.Microsecond)
			mu.Lock()
			running--
			mu.Unlock()
			r()
		}()
	}
	wg.Wait()
	if peak > 2 {
		t.Errorf("peak concurrency %d, want at most 2", peak)
	}
	if s := q.Stats(); s != (Stats{}) {
		t.Errorf("stats after = %+v", s)
	}
}
