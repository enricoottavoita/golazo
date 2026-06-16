package reddit

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestCache returns a GoalLinkCache backed by a temp file so tests don't
// touch ~/.golazo. Mirrors the construction path used by NewGoalLinkCache.
func newTestCache(t *testing.T) *GoalLinkCache {
	t.Helper()
	return &GoalLinkCache{
		links:    make(map[string]GoalLink),
		filePath: filepath.Join(t.TempDir(), "goal_links.json"),
	}
}

// recordingFetchHook is the queue's fetch hook for tests: records timestamps
// of every call, supports per-call result programming, and is safe under the
// queue's single-worker contract (no concurrency expected, mutex just guards
// against accidents).
type recordingFetchHook struct {
	mu        sync.Mutex
	calls     []time.Time
	results   []*GoalLink
	errors    []error
	callCount int32
}

func (h *recordingFetchHook) fetch(_ GoalInfo) (*GoalLink, error) {
	idx := atomic.AddInt32(&h.callCount, 1) - 1
	h.mu.Lock()
	h.calls = append(h.calls, time.Now())
	h.mu.Unlock()

	if int(idx) < len(h.errors) && h.errors[idx] != nil {
		return nil, h.errors[idx]
	}
	if int(idx) < len(h.results) {
		return h.results[idx], nil
	}
	return nil, nil
}

func (h *recordingFetchHook) callTimes() []time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]time.Time, len(h.calls))
	copy(out, h.calls)
	return out
}

// TestQueueIntervalPacing verifies the worker's pacing gate inside run():
// successive fetches must be separated by at least the configured interval.
// This is where the QueueInterval guarantee actually lives.
func TestQueueIntervalPacing(t *testing.T) {
	const interval = 50 * time.Millisecond

	hook := &recordingFetchHook{
		results: []*GoalLink{
			{MatchID: 1, Minute: 1, URL: "https://example.com/1"},
			{MatchID: 1, Minute: 2, URL: "https://example.com/2"},
			{MatchID: 1, Minute: 3, URL: "https://example.com/3"},
		},
	}
	q := newGoalQueue(hook.fetch, newTestCache(t), nil, interval, time.Minute)

	replies := make(chan GoalResult, 3)
	for i := 1; i <= 3; i++ {
		q.Enqueue(GoalInfo{MatchID: 1, Minute: i}, replies)
	}

	for i := 0; i < 3; i++ {
		select {
		case <-replies:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for reply %d", i)
		}
	}

	times := hook.callTimes()
	if len(times) != 3 {
		t.Fatalf("expected 3 fetch calls, got %d", len(times))
	}
	for i := 1; i < len(times); i++ {
		gap := times[i].Sub(times[i-1])
		if gap+5*time.Millisecond < interval {
			t.Errorf("call %d -> %d gap %v < interval %v", i-1, i, gap, interval)
		}
	}
}

// TestQueueCooldownOnBlocked verifies that an ErrBlocked response sets the
// cooldown window such that subsequent goals are dropped (no fetch attempt)
// while inside the window.
func TestQueueCooldownOnBlocked(t *testing.T) {
	hook := &recordingFetchHook{
		errors: []error{ErrBlocked}, // first (and only) attempt blocked
	}
	q := newGoalQueue(hook.fetch, newTestCache(t), nil, time.Millisecond, time.Hour)

	replies := make(chan GoalResult, 2)
	q.Enqueue(GoalInfo{MatchID: 1, Minute: 1}, replies)
	q.Enqueue(GoalInfo{MatchID: 1, Minute: 2}, replies)

	got := make([]GoalResult, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case r := <-replies:
			got = append(got, r)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for reply %d", i)
		}
	}

	if calls := atomic.LoadInt32(&hook.callCount); calls != 1 {
		t.Errorf("expected exactly 1 fetch attempt (blocked, then cooldown), got %d", calls)
	}
	for _, r := range got {
		if r.Link != nil {
			t.Errorf("expected nil Link during cooldown, got %+v for %+v", r.Link, r.Key)
		}
	}
}

// TestQueueDropsOnBlocked verifies that a blocked goal is not cached
// (neither as a found link nor as a NotFoundMarker) — leaving the next app
// session free to retry.
func TestQueueDropsOnBlocked(t *testing.T) {
	hook := &recordingFetchHook{errors: []error{ErrBlocked}}
	cache := newTestCache(t)
	q := newGoalQueue(hook.fetch, cache, nil, time.Millisecond, time.Hour)

	replies := make(chan GoalResult, 1)
	key := GoalLinkKey{MatchID: 99, Minute: 33}
	q.Enqueue(GoalInfo{MatchID: 99, Minute: 33}, replies)

	select {
	case <-replies:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocked reply")
	}

	if link := cache.Get(key); link != nil {
		t.Errorf("expected no cache entry for blocked goal, got %+v", link)
	}
}

// TestQueueDedupesInFlight verifies that enqueuing the same key twice while
// the first is in flight results in a single fetch, and both reply channels
// receive the result.
func TestQueueDedupesInFlight(t *testing.T) {
	// Block the fetch hook on a signal channel so the second Enqueue lands
	// while the first is still in flight.
	gate := make(chan struct{})
	hook := &slowFetchHook{
		gate: gate,
		link: &GoalLink{MatchID: 5, Minute: 10, URL: "https://example.com/dedup"},
	}
	q := newGoalQueue(hook.fetch, newTestCache(t), nil, time.Millisecond, time.Hour)

	r1 := make(chan GoalResult, 1)
	r2 := make(chan GoalResult, 1)
	g := GoalInfo{MatchID: 5, Minute: 10}
	q.Enqueue(g, r1)
	// Tiny pause: ensure the worker has picked up the first item and is
	// blocked inside the fetch hook before the second Enqueue arrives.
	time.Sleep(20 * time.Millisecond)
	q.Enqueue(g, r2)

	close(gate) // release the in-flight fetch

	for _, ch := range []chan GoalResult{r1, r2} {
		select {
		case r := <-ch:
			if r.Link == nil || r.Link.URL != "https://example.com/dedup" {
				t.Errorf("dedup reply missing expected link, got %+v", r.Link)
			}
		case <-time.After(time.Second):
			t.Fatal("dedup reply channel never received result")
		}
	}

	if got := atomic.LoadInt32(&hook.calls); got != 1 {
		t.Errorf("expected 1 fetch for deduped key, got %d", got)
	}
}

type slowFetchHook struct {
	gate  chan struct{}
	link  *GoalLink
	calls int32
}

func (h *slowFetchHook) fetch(_ GoalInfo) (*GoalLink, error) {
	atomic.AddInt32(&h.calls, 1)
	<-h.gate
	return h.link, nil
}

// TestQueueLazyStart verifies the worker goroutine is not started until the
// first Enqueue. Without lazy start, every Client construction would leak an
// idle goroutine. We assert this behaviorally: the fetch hook must not be
// invoked by mere construction, only by an actual Enqueue.
func TestQueueLazyStart(t *testing.T) {
	hook := &recordingFetchHook{
		results: []*GoalLink{{MatchID: 1, Minute: 1, URL: "https://example.com/lazy"}},
	}
	q := newGoalQueue(hook.fetch, newTestCache(t), nil, time.Millisecond, time.Hour)

	// Give a hypothetical eager worker plenty of scheduler ticks to run.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&hook.callCount); got != 0 {
		t.Fatalf("fetch hook called %d times before any Enqueue — worker started eagerly", got)
	}

	replies := make(chan GoalResult, 1)
	q.Enqueue(GoalInfo{MatchID: 1, Minute: 1}, replies)
	select {
	case <-replies:
	case <-time.After(time.Second):
		t.Fatal("worker did not start after first Enqueue")
	}
	if got := atomic.LoadInt32(&hook.callCount); got != 1 {
		t.Errorf("expected 1 fetch after Enqueue, got %d", got)
	}
}
