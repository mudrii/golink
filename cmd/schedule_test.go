package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/mudrii/golink/internal/schedule"
)

func fixedNow() time.Time {
	return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
}

func futureAt() string {
	return fixedNow().Add(2 * time.Hour).Format(time.RFC3339)
}

func pastAt() string {
	return fixedNow().Add(-time.Hour).Format(time.RFC3339)
}

// failingTransport embeds fakeTransport but returns an error for CreatePost.
type failingTransport struct {
	fakeTransport
	createErr error
}

func (f *failingTransport) CreatePost(_ context.Context, _ api.CreatePostRequest) (*output.PostSummary, error) {
	return nil, f.createErr
}

// scheduleAuthStore builds an in-memory session store with a valid fake session.
func scheduleAuthStore(t *testing.T) auth.Store {
	t.Helper()
	store := auth.NewMemoryStore()
	saveScheduleSession(t, store, "default", "official", "urn:li:person:sched123")
	return store
}

func saveScheduleSession(t *testing.T, store auth.Store, profile, transport, memberURN string) {
	t.Helper()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:     profile,
		Transport:   transport,
		AccessToken: "token-" + profile,
		MemberURN:   memberURN,
		ExpiresAt:   fixedNow().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}
}

func newScheduleTestDeps(sched schedule.Store) Dependencies {
	return Dependencies{
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
		Now:           fixedNow,
		AuditSink:     audit.NewMemorySink(),
		ScheduleStore: sched,
	}
}

type markRunningOverrideStore struct {
	schedule.Store
	blocked map[string]error
}

func (s *markRunningOverrideStore) MarkRunning(ctx context.Context, commandID string) error {
	if err, ok := s.blocked[commandID]; ok {
		return err
	}
	return s.Store.MarkRunning(ctx, commandID)
}

// decodeSchedulePost decodes a schedule post JSON envelope from stdout.
func decodeSchedulePost(t *testing.T, buf *bytes.Buffer) output.ScheduledPostData {
	t.Helper()
	var env struct {
		Data output.ScheduledPostData `json:"data"`
	}
	if err := json.NewDecoder(buf).Decode(&env); err != nil {
		t.Fatalf("decode schedule post output: %v (body=%s)", err, buf.String())
	}
	return env.Data
}

func decodeScheduleList(t *testing.T, buf *bytes.Buffer) output.ScheduleListData {
	t.Helper()
	var env struct {
		Data output.ScheduleListData `json:"data"`
	}
	if err := json.NewDecoder(buf).Decode(&env); err != nil {
		t.Fatalf("decode schedule list output: %v (body=%s)", err, buf.String())
	}
	return env.Data
}

func decodeScheduleRun(t *testing.T, buf *bytes.Buffer) output.ScheduleRunData {
	t.Helper()
	var env struct {
		Data output.ScheduleRunData `json:"data"`
	}
	if err := json.NewDecoder(buf).Decode(&env); err != nil {
		t.Fatalf("decode schedule run output: %v (body=%s)", err, buf.String())
	}
	return env.Data
}

func TestPostSchedule_ValidInput(t *testing.T) {
	sched := schedule.NewMemoryStore()
	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout

	code := ExecuteContext(context.Background(), []string{
		"--json", "post", "schedule",
		"--at", futureAt(),
		"--text", "hello scheduled world",
	}, deps, BuildInfo{})

	if code != 0 {
		t.Fatalf("want exit 0, got %d: %s", code, stdout.String())
	}

	data := decodeSchedulePost(t, stdout)
	if data.State != output.ScheduleStatePending {
		t.Errorf("want pending, got %s", data.State)
	}
	if data.Request.Text != "hello scheduled world" {
		t.Errorf("unexpected text: %s", data.Request.Text)
	}
	if data.CommandID == "" {
		t.Error("want non-empty command_id")
	}
}

func TestPostSchedule_PastAt(t *testing.T) {
	sched := schedule.NewMemoryStore()
	stderr := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stderr = stderr

	code := ExecuteContext(context.Background(), []string{
		"--json", "post", "schedule",
		"--at", pastAt(),
		"--text", "too late",
	}, deps, BuildInfo{})

	if code != 2 {
		t.Errorf("want exit 2 (validation), got %d", code)
	}
	if !strings.Contains(stderr.String(), "at least 30 seconds") {
		t.Errorf("want clock-skew error, got: %s", stderr.String())
	}
}

func TestPostSchedule_MissingText(t *testing.T) {
	sched := schedule.NewMemoryStore()
	stderr := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stderr = stderr

	code := ExecuteContext(context.Background(), []string{
		"--json", "post", "schedule",
		"--at", futureAt(),
	}, deps, BuildInfo{})

	if code != 2 {
		t.Errorf("want exit 2, got %d", code)
	}
}

func TestPostSchedule_RelativeImagePath(t *testing.T) {
	sched := schedule.NewMemoryStore()
	stderr := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stderr = stderr

	code := ExecuteContext(context.Background(), []string{
		"--json", "post", "schedule",
		"--at", futureAt(),
		"--text", "image post",
		"--image", "relative/path.jpg",
	}, deps, BuildInfo{})

	if code != 2 {
		t.Errorf("want exit 2 for relative path, got %d", code)
	}
	if !strings.Contains(stderr.String(), "absolute") {
		t.Errorf("want absolute path error, got: %s", stderr.String())
	}
}

func TestPostSchedule_RequireApprovalRejected(t *testing.T) {
	sched := schedule.NewMemoryStore()
	stderr := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stderr = stderr

	code := ExecuteContext(context.Background(), []string{
		"--json", "--require-approval", "post", "schedule",
		"--at", futureAt(),
		"--text", "needs approval",
	}, deps, BuildInfo{})

	if code != 2 {
		t.Errorf("want exit 2 (validation), got %d", code)
	}
	if !strings.Contains(stderr.String(), "not supported") {
		t.Errorf("want not-supported error, got: %s", stderr.String())
	}
}

func TestScheduleList_OrderedByScheduledAt(t *testing.T) {
	sched := schedule.NewMemoryStore()
	ctx := context.Background()

	// Add two entries in reverse order.
	later := fixedNow().Add(3 * time.Hour)
	earlier := fixedNow().Add(1 * time.Hour)
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "later-id", ScheduledAt: later, CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "later", Visibility: "PUBLIC"},
	})
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "earlier-id", ScheduledAt: earlier, CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "earlier", Visibility: "PUBLIC"},
	})

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout

	code := ExecuteContext(ctx, []string{"--json", "schedule", "list"}, deps, BuildInfo{})
	if code != 0 {
		t.Fatalf("want exit 0, got %d: %s", code, stdout.String())
	}

	data := decodeScheduleList(t, stdout)
	if len(data.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(data.Items))
	}
	if data.Items[0].CommandID != "earlier-id" {
		t.Errorf("want earlier-id first, got %s", data.Items[0].CommandID)
	}
	if data.Counts.Pending != 2 {
		t.Errorf("want pending=2, got %d", data.Counts.Pending)
	}
}

func TestScheduleShow_ByCommandID(t *testing.T) {
	sched := schedule.NewMemoryStore()
	ctx := context.Background()
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "show-id", ScheduledAt: fixedNow().Add(time.Hour), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "show me", Visibility: "PUBLIC"},
	})

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout

	code := ExecuteContext(ctx, []string{"--json", "schedule", "show", "show-id"}, deps, BuildInfo{})
	if code != 0 {
		t.Fatalf("want exit 0, got %d: %s", code, stdout.String())
	}

	var env struct {
		Data output.ScheduledPostItem `json:"data"`
	}
	if err := json.NewDecoder(stdout).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.CommandID != "show-id" {
		t.Errorf("want show-id, got %s", env.Data.CommandID)
	}
}

func TestScheduleShow_NotFound(t *testing.T) {
	sched := schedule.NewMemoryStore()
	stderr := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stderr = stderr

	code := ExecuteContext(context.Background(), []string{"--json", "schedule", "show", "nonexistent"}, deps, BuildInfo{})
	if code != 5 {
		t.Errorf("want exit 5 (not found), got %d", code)
	}
}

func TestScheduleCancel_DeletesPending(t *testing.T) {
	sched := schedule.NewMemoryStore()
	ctx := context.Background()
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "cancel-id", ScheduledAt: fixedNow().Add(time.Hour), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "cancel me", Visibility: "PUBLIC"},
	})

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout

	code := ExecuteContext(ctx, []string{"--json", "schedule", "cancel", "cancel-id"}, deps, BuildInfo{})
	if code != 0 {
		t.Fatalf("want exit 0, got %d: %s", code, stdout.String())
	}

	var env struct {
		Data output.ScheduledPostData `json:"data"`
	}
	if err := json.NewDecoder(stdout).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.State != output.ScheduleStateCancelled {
		t.Errorf("want cancelled, got %s", env.Data.State)
	}
}

func TestScheduleNext_ReturnEarliest(t *testing.T) {
	sched := schedule.NewMemoryStore()
	ctx := context.Background()
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "next-later", ScheduledAt: fixedNow().Add(3 * time.Hour), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "later", Visibility: "PUBLIC"},
	})
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "next-earlier", ScheduledAt: fixedNow().Add(time.Hour), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "earlier", Visibility: "PUBLIC"},
	})

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout

	code := ExecuteContext(ctx, []string{"--json", "schedule", "next"}, deps, BuildInfo{})
	if code != 0 {
		t.Fatalf("want exit 0, got %d: %s", code, stdout.String())
	}

	var env struct {
		Data output.ScheduledPostItem `json:"data"`
	}
	if err := json.NewDecoder(stdout).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.CommandID != "next-earlier" {
		t.Errorf("want next-earlier, got %s", env.Data.CommandID)
	}
}

func TestScheduleNext_NoPending(t *testing.T) {
	sched := schedule.NewMemoryStore()
	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout

	// No entries — should return unsupported envelope (exit 0).
	code := ExecuteContext(context.Background(), []string{"--json", "schedule", "next"}, deps, BuildInfo{})
	if code != 0 {
		t.Errorf("want exit 0 (unsupported), got %d: %s", code, stdout.String())
	}
	var env struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(stdout).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Status != "unsupported" {
		t.Errorf("want unsupported status, got %s", env.Status)
	}
}

func TestScheduleRun_NoDueEntries(t *testing.T) {
	sched := schedule.NewMemoryStore()
	ctx := context.Background()
	// Add a future entry — not yet due.
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "future-run", ScheduledAt: fixedNow().Add(24 * time.Hour), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "in the future", Visibility: "PUBLIC"},
	})

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout

	code := ExecuteContext(ctx, []string{"--json", "schedule", "run"}, deps, BuildInfo{})
	if code != 0 {
		t.Fatalf("want exit 0, got %d: %s", code, stdout.String())
	}
	data := decodeScheduleRun(t, stdout)
	if data.Ran != 0 {
		t.Errorf("want ran=0, got %d", data.Ran)
	}
}

func TestScheduleRun_UsesStoredProfileAndTransportPerEntry(t *testing.T) {
	sched := schedule.NewMemoryStore()
	ctx := context.Background()
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "alpha-run", ScheduledAt: fixedNow().Add(-2 * time.Minute), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "alpha", Transport: "official",
		Request: schedule.Request{Text: "alpha text", Visibility: "PUBLIC"},
	})
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "beta-run", ScheduledAt: fixedNow().Add(-time.Minute), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "beta", Transport: "unofficial",
		Request: schedule.Request{Text: "beta text", Visibility: "PUBLIC"},
	})

	store := auth.NewMemoryStore()
	saveScheduleSession(t, store, "alpha", "official", "urn:li:person:alpha")
	saveScheduleSession(t, store, "beta", "unofficial", "urn:li:person:beta")

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout
	deps.SessionStore = store

	var calls []string
	deps.TransportFactory = func(_ context.Context, settings config.Settings, session auth.Session, _ *slog.Logger) (api.Transport, error) {
		calls = append(calls, settings.Profile+"/"+settings.Transport+"/"+session.Profile)
		return &fakeTransport{name: settings.Transport}, nil
	}

	code := ExecuteContext(ctx, []string{"--json", "schedule", "run"}, deps, BuildInfo{})
	if code != 0 {
		t.Fatalf("want exit 0, got %d: %s", code, stdout.String())
	}

	data := decodeScheduleRun(t, stdout)
	if data.Succeeded != 2 {
		t.Fatalf("want succeeded=2, got %+v", data)
	}
	if len(calls) != 2 {
		t.Fatalf("want 2 transport factory calls, got %d (%v)", len(calls), calls)
	}
	if calls[0] != "alpha/official/alpha" || calls[1] != "beta/unofficial/beta" {
		t.Fatalf("transport factory calls = %v, want [alpha/official/alpha beta/unofficial/beta]", calls)
	}
}

func TestScheduleRun_CachedReplayUsesStoredResultPostURN(t *testing.T) {
	sched := schedule.NewMemoryStore()
	ctx := context.Background()
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "cached-run", ScheduledAt: fixedNow().Add(-time.Minute), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "cached-profile", Transport: "official", IdempotencyKey: "sched-key",
		Request: schedule.Request{Text: "cached text", Visibility: "PUBLIC"},
	})

	istore := idempotency.NewMemoryStore()
	resultBytes, err := json.Marshal(output.PostCreateData{
		PostSummary: output.PostSummary{ID: "urn:li:share:cached"},
	})
	if err != nil {
		t.Fatalf("marshal cached result: %v", err)
	}
	if err := istore.Record(ctx, idempotency.Entry{
		TS:         time.Now().UTC(),
		Key:        "sched-key",
		Command:    "post create",
		CommandID:  "cached-post",
		HTTPStatus: 201,
		RequestID:  "req-999",
		Result:     resultBytes,
	}); err != nil {
		t.Fatalf("record idempotency: %v", err)
	}

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout
	deps.IdempotencyStore = istore

	code := ExecuteContext(ctx, []string{"--json", "schedule", "run"}, deps, BuildInfo{})
	if code != 0 {
		t.Fatalf("want exit 0, got %d: %s", code, stdout.String())
	}

	data := decodeScheduleRun(t, stdout)
	if data.Ran != 1 || len(data.Results) != 1 {
		t.Fatalf("unexpected run payload: %+v", data)
	}
	if data.Results[0].PostURN != "urn:li:share:cached" {
		t.Fatalf("post_urn = %q, want urn:li:share:cached", data.Results[0].PostURN)
	}
}

func TestScheduleRun_FailingEntryIncrementsRetryCount(t *testing.T) {
	sched := schedule.NewMemoryStore()
	ctx := context.Background()
	// Entry is in the past — past-due.
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "fail-run", ScheduledAt: fixedNow().Add(-time.Hour), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "will fail", Visibility: "PUBLIC"},
	})

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout
	// Provide a valid session so resolveSession passes.
	deps.SessionStore = scheduleAuthStore(t)
	// Provide a transport that fails CreatePost so runOneEntry marks the entry failed.
	deps.TransportFactory = factoryReturning(&failingTransport{
		fakeTransport: fakeTransport{name: "official"},
		createErr:     fmt.Errorf("simulated api error"),
	})

	code := ExecuteContext(ctx, []string{"--json", "schedule", "run"}, deps, BuildInfo{})
	// run should succeed (exit 0) even with failed entries.
	if code != 0 {
		t.Fatalf("want exit 0, got %d stdout=%s", code, stdout.String())
	}

	data := decodeScheduleRun(t, stdout)
	if data.Ran != 1 {
		t.Errorf("want ran=1, got %d", data.Ran)
	}
	if data.Failed != 1 {
		t.Errorf("want failed=1, got %d", data.Failed)
	}
	if len(data.Results) != 1 || data.Results[0].Status != "failed" {
		t.Errorf("want results[0].status=failed, got %+v", data.Results)
	}

	// Verify retry_count incremented in the store.
	e, err := sched.Get(ctx, "fail-run")
	if err != nil {
		t.Fatalf("Get after run: %v", err)
	}
	if e.RetryCount != 1 {
		t.Errorf("want retry_count=1, got %d", e.RetryCount)
	}
}

func TestScheduleRun_SkippedEntryDoesNotCountAsFailureOrTriggerFailFast(t *testing.T) {
	baseStore := schedule.NewMemoryStore()
	sched := &markRunningOverrideStore{
		Store:   baseStore,
		blocked: map[string]error{"skip-run": fmt.Errorf("already claimed")},
	}
	ctx := context.Background()

	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "skip-run", ScheduledAt: fixedNow().Add(-2 * time.Minute), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "skip me", Visibility: "PUBLIC"},
	})
	_ = sched.Add(ctx, schedule.Entry{
		CommandID: "succeed-run", ScheduledAt: fixedNow().Add(-time.Minute), CreatedAt: fixedNow(),
		State: schedule.StatePending, Profile: "default", Transport: "official",
		Request: schedule.Request{Text: "run me", Visibility: "PUBLIC"},
	})

	stdout := &bytes.Buffer{}
	deps := newScheduleTestDeps(sched)
	deps.Stdout = stdout
	deps.SessionStore = scheduleAuthStore(t)
	deps.TransportFactory = factoryReturning(&fakeTransport{name: "official"})

	code := ExecuteContext(ctx, []string{"--json", "schedule", "run", "--fail-fast"}, deps, BuildInfo{})
	if code != 0 {
		t.Fatalf("want exit 0, got %d stdout=%s", code, stdout.String())
	}

	data := decodeScheduleRun(t, stdout)
	if data.Ran != 2 {
		t.Fatalf("want ran=2, got %+v", data)
	}
	if data.Succeeded != 1 {
		t.Errorf("want succeeded=1, got %d", data.Succeeded)
	}
	if data.Failed != 0 {
		t.Errorf("want failed=0, got %d", data.Failed)
	}
	if data.Skipped != 1 {
		t.Errorf("want skipped=1, got %d", data.Skipped)
	}
	if len(data.Results) != 2 {
		t.Fatalf("want 2 results, got %d", len(data.Results))
	}
	if data.Results[0].Status != "skipped" {
		t.Fatalf("first result status = %q, want skipped", data.Results[0].Status)
	}
	if data.Results[1].Status != "succeeded" {
		t.Fatalf("second result status = %q, want succeeded", data.Results[1].Status)
	}
}
