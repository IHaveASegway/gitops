// Package runner executes an operation against many targets in parallel,
// preserving order and reporting progress as it goes.
package runner

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultJobs is the default number of targets processed concurrently.
const DefaultJobs = 8

// Result holds the outcome of an operation on a single target.
type Result struct {
	Repo    string // display name of the target
	Success bool
	Output  string // human-readable output on success (may span lines)
	Error   string // failure reason
}

// Text returns the human-readable body: output on success, error otherwise.
func (r Result) Text() string {
	if r.Success {
		return r.Output
	}
	return r.Error
}

// FirstLine returns the first line of Text.
func (r Result) FirstLine() string {
	line, _, _ := strings.Cut(r.Text(), "\n")
	return line
}

// Func runs one operation against one target (usually a repository path).
type Func func(ctx context.Context, target string) Result

// Event reports progress: Started when a target begins, otherwise the
// finished Result.
type Event struct {
	Index   int
	Started bool
	Result  Result
}

// Run applies fn to every target using at most jobs workers. Targets start
// in order and results keep the input order. onEvent, if non-nil, is called
// serially from worker goroutines. When ctx is canceled, remaining targets
// finish with the error "canceled".
func Run(ctx context.Context, targets []string, fn Func, jobs int, onEvent func(Event)) []Result {
	if jobs <= 0 {
		jobs = DefaultJobs
	}
	jobs = min(jobs, len(targets))
	results := make([]Result, len(targets))

	var mu sync.Mutex
	emit := func(ev Event) {
		if onEvent == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		onEvent(ev)
	}

	idxCh := make(chan int)
	var wg sync.WaitGroup
	for range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range idxCh {
				name := filepath.Base(targets[idx])
				if ctx.Err() != nil {
					results[idx] = Result{Repo: name, Error: "canceled"}
					emit(Event{Index: idx, Result: results[idx]})
					continue
				}
				emit(Event{Index: idx, Started: true})
				r := fn(ctx, targets[idx])
				if r.Repo == "" {
					r.Repo = name
				}
				results[idx] = r
				emit(Event{Index: idx, Result: r})
			}
		}()
	}
	for i := range targets {
		idxCh <- i
	}
	close(idxCh)
	wg.Wait()
	return results
}

// Summarize counts successes and failures.
func Summarize(results []Result) (ok, fail int) {
	for _, r := range results {
		if r.Success {
			ok++
		} else {
			fail++
		}
	}
	return ok, fail
}
