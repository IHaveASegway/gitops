package runner

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunOrderLimitAndCancel(t *testing.T) {
	targets := []string{"/x/a", "/x/b", "/x/c", "/x/d", "/x/e", "/x/f"}
	var running, peak int32
	fn := func(ctx context.Context, target string) Result {
		n := atomic.AddInt32(&running, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return Result{Success: true, Output: "ok " + target}
	}

	var mu sync.Mutex
	var order []int
	results := Run(context.Background(), targets, fn, 2, func(ev Event) {
		if ev.Started {
			mu.Lock()
			order = append(order, ev.Index)
			mu.Unlock()
		}
	})
	if peak > 2 {
		t.Errorf("peak concurrency %d exceeds jobs=2", peak)
	}
	for i, r := range results {
		if !r.Success || r.Repo != filepath.Base(targets[i]) || r.Output != "ok "+targets[i] {
			t.Errorf("result %d = %+v", i, r)
		}
	}
	for i := 1; i < len(order); i++ {
		if order[i] < order[i-1] {
			t.Errorf("targets started out of order: %v", order)
			break
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, r := range Run(ctx, targets, fn, 3, nil) {
		if r.Success || r.Error != "canceled" {
			t.Errorf("canceled result = %+v", r)
		}
	}
	if got := Run(context.Background(), nil, fn, 0, nil); len(got) != 0 {
		t.Errorf("no targets should yield no results, got %v", got)
	}
}

func TestResultHelpers(t *testing.T) {
	r := Result{Success: true, Output: "first\nsecond"}
	if r.Text() != "first\nsecond" || r.FirstLine() != "first" {
		t.Errorf("unexpected %q / %q", r.Text(), r.FirstLine())
	}
	f := Result{Error: "boom"}
	if f.Text() != "boom" || f.FirstLine() != "boom" {
		t.Errorf("unexpected %q", f.Text())
	}
	if ok, fail := Summarize([]Result{r, f, f}); ok != 1 || fail != 2 {
		t.Errorf("summarize = %d/%d", ok, fail)
	}
}
