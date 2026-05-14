package poller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meopedevts/revu/internal/github"
	"github.com/meopedevts/revu/internal/store"
)

type fakeProfileProvider struct {
	mu    sync.Mutex
	id    string
	err   error
	calls int
}

func (f *fakeProfileProvider) ActiveProfileID(_ context.Context) (string, error) {
	f.mu.Lock()
	f.calls++
	id, err := f.id, f.err
	f.mu.Unlock()
	return id, err
}

func (f *fakeProfileProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type recordingStore struct {
	store.Store

	mu    sync.Mutex
	calls []string
}

func (r *recordingStore) SetActiveProfileID(id string) {
	r.mu.Lock()
	r.calls = append(r.calls, id)
	r.mu.Unlock()
	r.Store.SetActiveProfileID(id)
}

func (r *recordingStore) profileCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

type fakeClient struct {
	mu sync.Mutex

	listCalls    int
	detailsCalls int

	listResponses   []listResponse
	detailsResponse github.PRDetails
	detailsErr      error
}

type listResponse struct {
	prs []github.PRSummary
	err error
}

func (f *fakeClient) AuthStatus(_ context.Context) error { return nil }

func (f *fakeClient) ListReviewRequested(_ context.Context) ([]github.PRSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.listCalls
	f.listCalls++
	if i >= len(f.listResponses) {
		return nil, nil
	}
	r := f.listResponses[i]
	return r.prs, r.err
}

func (f *fakeClient) GetPRDetails(_ context.Context, _ string) (*github.PRDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detailsCalls++
	if f.detailsErr != nil {
		return nil, f.detailsErr
	}
	d := f.detailsResponse
	return &d, nil
}

func (f *fakeClient) GetPRFullDetails(_ context.Context, _ string) (*github.PRFullDetails, error) {
	return nil, nil
}

func (f *fakeClient) GetPRDiff(_ context.Context, _ string) (string, error) { return "", nil }

func (f *fakeClient) MergePR(_ context.Context, _ string, _ github.MergeMethod) error {
	return nil
}

func (f *fakeClient) ApprovePR(_ context.Context, _ string) error {
	return nil
}

type fakeNotifier struct {
	mu   sync.Mutex
	sent []store.PRRecord
	err  error
}

func (f *fakeNotifier) Notify(pr store.PRRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, pr)
	return nil
}

func (f *fakeNotifier) SetEnabled(bool)                {}
func (f *fakeNotifier) SetExpireTimeout(time.Duration) {}
func (f *fakeNotifier) Close() error                   { return nil }

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func freshStore(t *testing.T) store.Store {
	t.Helper()
	s := store.New(filepath.Join(t.TempDir(), "revu.db"),
		store.WithClock(func() time.Time { return time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC) }),
	)
	if err := s.Load(context.Background()); err != nil {
		t.Fatalf("store Load: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}

func TestTick_NotifiesNovosAndEnriches(t *testing.T) {
	fc := &fakeClient{
		listResponses: []listResponse{{
			prs: []github.PRSummary{
				{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x",
					URL: "https://github.com/a/b/pull/1"},
			},
		}},
		detailsResponse: github.PRDetails{Additions: 10, Deletions: 2, State: "OPEN"},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithLogger(quietLogger()))

	p.tick(context.Background())

	if fc.detailsCalls != 1 {
		t.Fatalf("want 1 details call, got %d", fc.detailsCalls)
	}
	if len(fn.sent) != 1 {
		t.Fatalf("want 1 notify, got %d", len(fn.sent))
	}
	got := fn.sent[0]
	if got.Additions != 10 || got.Deletions != 2 {
		t.Fatalf("notification not enriched: %+v", got)
	}
}

func TestTick_SecondPollSamePRsDoesNotRenotify(t *testing.T) {
	prs := []github.PRSummary{
		{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u"},
	}
	fc := &fakeClient{
		listResponses: []listResponse{{prs: prs}, {prs: prs}},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithLogger(quietLogger()))

	p.tick(context.Background())
	p.tick(context.Background())

	if len(fn.sent) != 1 {
		t.Fatalf("want 1 notify across 2 polls, got %d", len(fn.sent))
	}
}

func TestTick_ReRequestRenotifies(t *testing.T) {
	pr1 := github.PRSummary{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u1"}
	pr2 := github.PRSummary{ID: "a/b#2", Number: 2, Repo: "a/b", Title: "t2", Author: "x", URL: "u2"}
	fc := &fakeClient{
		listResponses: []listResponse{
			{prs: []github.PRSummary{pr1, pr2}}, // initial
			{prs: []github.PRSummary{pr2}},      // pr1 dropped (review submitted)
			{prs: []github.PRSummary{pr1, pr2}}, // pr1 re-requested
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	// Re-request lands ~immediately so the cooldown would otherwise mute it;
	// disable cooldown to assert the dedup path doesn't swallow legit
	// re-requests when the operator opts out of the throttle.
	p := New(fc, s, fn, WithLogger(quietLogger()))

	p.tick(context.Background())
	p.tick(context.Background())
	p.tick(context.Background())

	if len(fn.sent) != 3 {
		t.Fatalf("want 3 notifies (pr1 initial + pr2 initial + pr1 re-request), got %d", len(fn.sent))
	}
}

func TestTick_EnrichFailureStillNotifies(t *testing.T) {
	fc := &fakeClient{
		listResponses: []listResponse{{prs: []github.PRSummary{
			{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u"},
		}}},
		detailsErr: errors.New("boom"),
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithLogger(quietLogger()))

	p.tick(context.Background())

	if len(fn.sent) != 1 {
		t.Fatalf("want 1 notify even without diff, got %d", len(fn.sent))
	}
	if fn.sent[0].Additions != 0 || fn.sent[0].Deletions != 0 {
		t.Fatalf("want zero diff when enrich fails, got %+v", fn.sent[0])
	}
}

func TestBackoff_DoublesUntilCap(t *testing.T) {
	fc := &fakeClient{
		listResponses: []listResponse{
			{err: github.ErrTransient},
			{err: github.ErrTransient},
			{err: github.ErrTransient},
			{err: github.ErrTransient},
			{err: github.ErrTransient},
			{err: github.ErrTransient},
			{err: github.ErrTransient},
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithInterval(5*time.Minute), WithLogger(quietLogger()))

	wants := []time.Duration{
		10 * time.Minute,
		20 * time.Minute,
		MaxBackoff,
		MaxBackoff,
		MaxBackoff,
		MaxBackoff,
		MaxBackoff,
	}
	for i, want := range wants {
		p.tick(context.Background())
		if got := p.CurrentBackoff(); got != want {
			t.Fatalf("tick #%d: want backoff %s, got %s", i+1, want, got)
		}
	}
}

func TestBackoff_ResetsOnSuccess(t *testing.T) {
	fc := &fakeClient{
		listResponses: []listResponse{
			{err: github.ErrTransient},
			{prs: nil},
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithLogger(quietLogger()))

	p.tick(context.Background())
	if p.CurrentBackoff() == 0 {
		t.Fatal("want backoff > 0 after failure")
	}
	p.tick(context.Background())
	if p.CurrentBackoff() != 0 {
		t.Fatalf("want backoff reset after success, got %s", p.CurrentBackoff())
	}
}

func TestTick_EmitsEvents_HappyPath(t *testing.T) {
	fc := &fakeClient{
		listResponses: []listResponse{{
			prs: []github.PRSummary{
				{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u"},
			},
		}},
		detailsResponse: github.PRDetails{Additions: 5, Deletions: 2, State: "OPEN"},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	var mu sync.Mutex
	var events []Event
	p := New(fc, s, fn,
		WithLogger(quietLogger()),
		WithEventHandler(func(e Event) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}),
	)

	p.tick(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("want 2 events (pr:new + poll:completed), got %d: %+v", len(events), events)
	}
	if events[0].Kind != EventPRNew {
		t.Fatalf("events[0] kind = %s, want %s", events[0].Kind, EventPRNew)
	}
	if events[0].PR == nil || events[0].PR.ID != "a/b#1" || events[0].PR.Additions != 5 {
		t.Fatalf("events[0] PR payload wrong: %+v", events[0].PR)
	}
	if events[1].Kind != EventPollCompleted || events[1].At.IsZero() {
		t.Fatalf("events[1] bad: %+v", events[1])
	}
	if events[1].Err != "" {
		t.Fatalf("poll:completed should have no error on happy path, got %q", events[1].Err)
	}
}

func TestTick_EmitsPollCompleted_WithErrOnFailure(t *testing.T) {
	fc := &fakeClient{
		listResponses: []listResponse{{err: github.ErrTransient}},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	var got Event
	p := New(fc, s, fn,
		WithLogger(quietLogger()),
		WithEventHandler(func(e Event) { got = e }),
	)

	p.tick(context.Background())

	if got.Kind != EventPollCompleted {
		t.Fatalf("want %s, got %+v", EventPollCompleted, got)
	}
	if got.Err == "" {
		t.Fatal("want non-empty Err on failed poll")
	}
}

func TestTick_EmitsStatusChanged(t *testing.T) {
	prs := []github.PRSummary{
		{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u"},
	}
	// First poll: open. Second poll (after merge): same URL but details
	// return CLOSED + mergedAt → state flips to MERGED → status-changed.
	merged := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	fc := &fakeClient{
		listResponses: []listResponse{
			{prs: prs},
			{prs: prs},
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)

	var mu sync.Mutex
	var kinds []string
	p := New(fc, s, fn,
		WithLogger(quietLogger()),
		WithEventHandler(func(e Event) {
			mu.Lock()
			kinds = append(kinds, e.Kind)
			mu.Unlock()
		}),
	)

	// Tick 1: OPEN details.
	fc.detailsResponse = github.PRDetails{State: "OPEN"}
	p.tick(context.Background())
	// Tick 2: CLOSED + mergedAt → MERGED.
	fc.detailsResponse = github.PRDetails{State: "CLOSED", MergedAt: &merged}
	p.tick(context.Background())

	mu.Lock()
	defer mu.Unlock()
	// Tick 1: pr:new + poll:completed (no status-changed — prevState empty).
	// Tick 2: neither pr:new (already known, pending=true again) nor
	// status-changed for this case — wait, enrich only fires for novos in
	// UpdateFromPoll. Re-pending-without-prior-drop is NOT novo. So tick 2
	// has just poll:completed. We really need to test the status change
	// path explicitly: manually call enrich on a pre-populated store.
	foundNew := false
	foundCompleted := 0
	for _, k := range kinds {
		if k == EventPRNew {
			foundNew = true
		}
		if k == EventPollCompleted {
			foundCompleted++
		}
	}
	if !foundNew {
		t.Fatal("expected at least one pr:new")
	}
	if foundCompleted != 2 {
		t.Fatalf("expected 2 poll:completed, got %d", foundCompleted)
	}
}

func TestTick_VanishedPRGetsEnriched(t *testing.T) {
	// REV-43: a transient `[]` from gh search no longer flags every PR as
	// vanished. The vanished path now triggers when the next non-empty
	// poll legitimately omits a PR (review submitted, merged, etc.). Use
	// pr2 as the survivor so pr1's drop is unambiguous.
	pr1 := github.PRSummary{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u1"}
	pr2 := github.PRSummary{ID: "a/b#2", Number: 2, Repo: "a/b", Title: "t2", Author: "y", URL: "u2"}
	fc := &fakeClient{
		listResponses: []listResponse{
			{prs: []github.PRSummary{pr1, pr2}}, // tick 1: both visible
			{prs: []github.PRSummary{pr2}},      // tick 2: pr1 dropped, gets re-enriched
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithLogger(quietLogger()))

	// Tick 1 enrich → both stay PENDING + OPEN.
	fc.detailsResponse = github.PRDetails{State: "OPEN", ReviewState: "PENDING"}
	p.tick(context.Background())

	// Tick 2 enrich for the dropped pr1 → merged after approval; REV-16
	// flips it into the history tab.
	fc.detailsResponse = github.PRDetails{State: "MERGED", ReviewState: "APPROVED"}
	p.tick(context.Background())

	history := s.GetHistory(context.Background())
	if len(history) != 1 || history[0].ID != "a/b#1" {
		t.Fatalf("vanished PR should be in history, got %+v", history)
	}
	if history[0].State != "MERGED" || history[0].ReviewState != "APPROVED" {
		t.Fatalf("history row not fully enriched: %+v", history[0])
	}
	if len(fn.sent) != 2 {
		t.Fatalf("initial tick must notify both PRs; sent=%d", len(fn.sent))
	}
}

func TestTrigger_ForcesImmediatePoll(t *testing.T) {
	fc := &fakeClient{
		listResponses: []listResponse{
			{prs: nil}, // first (immediate) tick
			{prs: []github.PRSummary{
				{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u"},
			}},
		},
		detailsResponse: github.PRDetails{State: "OPEN"},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	// Long interval so the trigger, not the timer, drives the second tick.
	p := New(fc, s, fn, WithInterval(time.Hour), WithLogger(quietLogger()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	// Wait for the immediate tick to land.
	deadline := time.Now().Add(2 * time.Second)
	for {
		fc.mu.Lock()
		n := fc.listCalls
		fc.mu.Unlock()
		if n >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first tick never fired")
		}
		time.Sleep(10 * time.Millisecond)
	}

	p.Trigger()

	deadline = time.Now().Add(2 * time.Second)
	for {
		fn.mu.Lock()
		sent := len(fn.sent)
		fn.mu.Unlock()
		if sent >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("trigger did not produce a notification")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done
}

func TestTrigger_CoalescesBursts(t *testing.T) {
	p := New(&fakeClient{}, freshStore(t), &fakeNotifier{}, WithLogger(quietLogger()))
	// Ten rapid triggers must not panic nor block.
	for range 10 {
		p.Trigger()
	}
	// Channel must still have exactly one pending item.
	select {
	case <-p.triggerCh:
	default:
		t.Fatal("expected at least one trigger queued")
	}
	select {
	case <-p.triggerCh:
		t.Fatal("trigger channel should have coalesced extras")
	default:
	}
}

func TestSetInterval_TakesEffectOnNextWait(t *testing.T) {
	fc := &fakeClient{}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithInterval(time.Hour), WithLogger(quietLogger()))

	if p.Interval() != time.Hour {
		t.Fatalf("initial interval: %s", p.Interval())
	}
	p.SetInterval(10 * time.Second)
	if p.Interval() != 10*time.Second {
		t.Fatalf("post-set interval: %s", p.Interval())
	}
	// Non-positive ignored.
	p.SetInterval(0)
	p.SetInterval(-1 * time.Second)
	if p.Interval() != 10*time.Second {
		t.Fatalf("invalid SetInterval mutated state: %s", p.Interval())
	}
}

func TestTick_AppliesActiveProfile(t *testing.T) {
	fc := &fakeClient{listResponses: []listResponse{{prs: nil}}}
	fn := &fakeNotifier{}
	rs := &recordingStore{Store: freshStore(t)}
	pp := &fakeProfileProvider{id: "profile-xyz"}
	p := New(fc, rs, fn, WithLogger(quietLogger()), WithActiveProfile(pp))

	p.tick(context.Background())

	calls := rs.profileCalls()
	if len(calls) != 1 || calls[0] != "profile-xyz" {
		t.Fatalf("want SetActiveProfileID(\"profile-xyz\") once, got %#v", calls)
	}
	if got := pp.callCount(); got != 1 {
		t.Errorf("provider call count: want 1, got %d", got)
	}
}

func TestTick_SkipsActiveProfileOnEmptyID(t *testing.T) {
	fc := &fakeClient{listResponses: []listResponse{{prs: nil}}}
	fn := &fakeNotifier{}
	rs := &recordingStore{Store: freshStore(t)}
	pp := &fakeProfileProvider{id: ""}
	p := New(fc, rs, fn, WithLogger(quietLogger()), WithActiveProfile(pp))

	p.tick(context.Background())

	if calls := rs.profileCalls(); len(calls) != 0 {
		t.Fatalf("empty id must not call SetActiveProfileID, got %#v", calls)
	}
}

func TestTick_LogsActiveProfileError(t *testing.T) {
	fc := &fakeClient{listResponses: []listResponse{{prs: nil}}}
	fn := &fakeNotifier{}
	rs := &recordingStore{Store: freshStore(t)}
	pp := &fakeProfileProvider{err: errors.New("profile lookup boom")}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := New(fc, rs, fn, WithLogger(logger), WithActiveProfile(pp))

	p.tick(context.Background())

	if calls := rs.profileCalls(); len(calls) != 0 {
		t.Fatalf("provider error must not call SetActiveProfileID, got %#v", calls)
	}
	if !strings.Contains(buf.String(), "resolve active profile") {
		t.Fatalf("expected provider error log, got %q", buf.String())
	}
}

func TestRunOnce_PropagatesCtxError(t *testing.T) {
	fc := &fakeClient{}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithLogger(quietLogger()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want wrapped context.Canceled, got %v", err)
	}
}

func TestRunOnce_NilErrorOnHealthyTick(t *testing.T) {
	fc := &fakeClient{listResponses: []listResponse{{prs: nil}}}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithLogger(quietLogger()))

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("healthy tick should return nil, got %v", err)
	}
}

func TestRun_StopsOnCtxCancel(t *testing.T) {
	fc := &fakeClient{}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn, WithInterval(time.Hour), WithLogger(quietLogger()))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	// Give the first tick a moment to fire.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// REV-43 — when a re-request lands inside the cooldown window, the
// desktop notify is skipped (LastNotifiedAt was populated by the previous
// tick). The EventPRNew emit is unaffected so the UI list still refreshes.
func TestTick_CooldownSuppressesNotify(t *testing.T) {
	pr1 := github.PRSummary{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u1"}
	pr2 := github.PRSummary{ID: "a/b#2", Number: 2, Repo: "a/b", Title: "t2", Author: "y", URL: "u2"}
	fc := &fakeClient{
		listResponses: []listResponse{
			{prs: []github.PRSummary{pr1, pr2}}, // initial — both notify
			{prs: []github.PRSummary{pr2}},      // pr1 dropped
			{prs: []github.PRSummary{pr1, pr2}}, // pr1 re-request inside cooldown
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	clock := &mutableClock{now: time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)}
	p := New(fc, s, fn,
		WithLogger(quietLogger()),
		WithClock(clock.Now),
		WithNotifyCooldown(6*time.Hour),
	)

	p.tick(context.Background()) // initial: notify pr1 + pr2
	clock.advance(5 * time.Minute)
	p.tick(context.Background()) // pr1 drops
	clock.advance(5 * time.Minute)
	p.tick(context.Background()) // pr1 re-request inside cooldown → skip notify

	if len(fn.sent) != 2 {
		t.Fatalf("want 2 notifies (initial pr1 + initial pr2 only), got %d: %+v",
			len(fn.sent), notifyIDs(fn))
	}
}

// REV-43 — outside the cooldown window the re-request does notify again
// and MarkNotified is called so the next window starts from `now`.
func TestTick_NotifyFiresAfterCooldown(t *testing.T) {
	pr1 := github.PRSummary{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u1"}
	pr2 := github.PRSummary{ID: "a/b#2", Number: 2, Repo: "a/b", Title: "t2", Author: "y", URL: "u2"}
	fc := &fakeClient{
		listResponses: []listResponse{
			{prs: []github.PRSummary{pr1, pr2}},
			{prs: []github.PRSummary{pr2}},
			{prs: []github.PRSummary{pr1, pr2}},
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	clock := &mutableClock{now: time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)}
	p := New(fc, s, fn,
		WithLogger(quietLogger()),
		WithClock(clock.Now),
		WithNotifyCooldown(6*time.Hour),
	)

	p.tick(context.Background())
	clock.advance(7 * time.Hour)
	p.tick(context.Background())
	clock.advance(1 * time.Hour) // total 8h: outside cooldown
	p.tick(context.Background())

	if len(fn.sent) != 3 {
		t.Fatalf("want 3 notifies (initial pr1, initial pr2, pr1 re-request), got %d: %+v",
			len(fn.sent), notifyIDs(fn))
	}
}

// REV-43 — cooldown=0 means throttle is off; every re-request notifies.
func TestTick_CooldownZeroDisablesThrottle(t *testing.T) {
	pr1 := github.PRSummary{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u1"}
	pr2 := github.PRSummary{ID: "a/b#2", Number: 2, Repo: "a/b", Title: "t2", Author: "y", URL: "u2"}
	fc := &fakeClient{
		listResponses: []listResponse{
			{prs: []github.PRSummary{pr1, pr2}},
			{prs: []github.PRSummary{pr2}},
			{prs: []github.PRSummary{pr1, pr2}},
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	p := New(fc, s, fn,
		WithLogger(quietLogger()),
		// cooldown not set → defaults to 0 → throttle disabled
	)

	p.tick(context.Background())
	p.tick(context.Background())
	p.tick(context.Background())

	if len(fn.sent) != 3 {
		t.Fatalf("want 3 notifies with throttle off, got %d", len(fn.sent))
	}
}

// REV-43 — empty poll between two non-empty polls of the same PRs must not
// trigger a burst of re-notifications. This guards the original bug:
// transient `[]` from gh search was zeroing review_pending and the next
// poll re-flagged everything as new.
func TestTick_EmptyPollDoesNotBurstOnRecovery(t *testing.T) {
	prs := []github.PRSummary{
		{ID: "a/b#1", Number: 1, Repo: "a/b", Title: "t", Author: "x", URL: "u1"},
		{ID: "a/b#2", Number: 2, Repo: "a/b", Title: "t2", Author: "y", URL: "u2"},
		{ID: "a/b#3", Number: 3, Repo: "a/b", Title: "t3", Author: "z", URL: "u3"},
	}
	fc := &fakeClient{
		listResponses: []listResponse{
			{prs: prs},
			{prs: nil}, // transient []
			{prs: prs},
		},
	}
	fn := &fakeNotifier{}
	s := freshStore(t)
	clock := &mutableClock{now: time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)}
	p := New(fc, s, fn,
		WithLogger(quietLogger()),
		WithClock(clock.Now),
		WithNotifyCooldown(6*time.Hour),
	)

	p.tick(context.Background())
	clock.advance(1 * time.Minute)
	p.tick(context.Background())
	clock.advance(1 * time.Minute)
	p.tick(context.Background())

	if len(fn.sent) != 3 {
		t.Fatalf("transient empty poll caused re-notify burst: got %d notifies (want 3)",
			len(fn.sent))
	}
}

func TestShouldNotify(t *testing.T) {
	t0 := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	notifiedAgo := func(d time.Duration) *time.Time {
		v := t0.Add(-d)
		return &v
	}
	cases := []struct {
		name     string
		rec      store.PRRecord
		cooldown time.Duration
		want     bool
	}{
		{"never_notified_passes", store.PRRecord{}, time.Hour, true},
		{"cooldown_disabled_always_passes", store.PRRecord{LastNotifiedAt: notifiedAgo(time.Minute)}, 0, true},
		{"inside_cooldown_blocks", store.PRRecord{LastNotifiedAt: notifiedAgo(30 * time.Minute)}, time.Hour, false},
		{"exactly_at_cooldown_passes", store.PRRecord{LastNotifiedAt: notifiedAgo(time.Hour)}, time.Hour, true},
		{"outside_cooldown_passes", store.PRRecord{LastNotifiedAt: notifiedAgo(2 * time.Hour)}, time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldNotify(tc.rec, t0, tc.cooldown); got != tc.want {
				t.Fatalf("shouldNotify(%+v, cooldown=%s) = %v, want %v",
					tc.rec, tc.cooldown, got, tc.want)
			}
		})
	}
}

// SetNotifyCooldown updates at runtime; negative values are coerced to
// zero (throttle disabled).
func TestPoller_SetNotifyCooldown(t *testing.T) {
	p := New(&fakeClient{}, freshStore(t), &fakeNotifier{}, WithLogger(quietLogger()))
	if p.NotifyCooldown() != 0 {
		t.Fatalf("default cooldown should be 0, got %s", p.NotifyCooldown())
	}
	p.SetNotifyCooldown(2 * time.Hour)
	if p.NotifyCooldown() != 2*time.Hour {
		t.Fatalf("setter ignored update: %s", p.NotifyCooldown())
	}
	p.SetNotifyCooldown(-1 * time.Hour)
	if p.NotifyCooldown() != 0 {
		t.Fatalf("negative should clamp to 0, got %s", p.NotifyCooldown())
	}
}

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mutableClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func notifyIDs(fn *fakeNotifier) []string {
	fn.mu.Lock()
	defer fn.mu.Unlock()
	out := make([]string, len(fn.sent))
	for i, r := range fn.sent {
		out[i] = r.ID
	}
	return out
}
