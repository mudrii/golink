package schedule

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2027, 1, 1, 9, 0, 0, 0, time.UTC)
var t1 = time.Date(2027, 1, 2, 9, 0, 0, 0, time.UTC)
var t2 = time.Date(2027, 1, 3, 9, 0, 0, 0, time.UTC)

func sampleEntry(id string, at time.Time) Entry {
	return Entry{
		CommandID:   id,
		State:       StatePending,
		ScheduledAt: at,
		CreatedAt:   at.Add(-time.Hour),
		Profile:     "default",
		Transport:   "official",
		Request: Request{
			Text:       "hello world",
			Visibility: "PUBLIC",
		},
	}
}

func TestMemoryStore_AddListGet(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()

	e1 := sampleEntry("id1", t1)
	e2 := sampleEntry("id2", t0)

	if err := m.Add(ctx, e1); err != nil {
		t.Fatalf("Add e1: %v", err)
	}
	if err := m.Add(ctx, e2); err != nil {
		t.Fatalf("Add e2: %v", err)
	}

	entries, err := m.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	// sorted by scheduled_at: e2 (t0) < e1 (t1)
	if entries[0].CommandID != "id2" {
		t.Errorf("want id2 first, got %s", entries[0].CommandID)
	}

	got, err := m.Get(ctx, "id1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CommandID != "id1" {
		t.Errorf("want id1, got %s", got.CommandID)
	}
}

func TestMemoryStore_Due(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()

	_ = m.Add(ctx, sampleEntry("past", t0))
	_ = m.Add(ctx, sampleEntry("future", t2))

	due, err := m.Due(ctx, t1, 50)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("want 1 due, got %d", len(due))
	}
	if due[0].CommandID != "past" {
		t.Errorf("want past, got %s", due[0].CommandID)
	}
}

func TestMemoryStore_DueLimit(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()

	for i := range 5 {
		_ = m.Add(ctx, sampleEntry("id"+string(rune('A'+i)), t0))
	}
	due, err := m.Due(ctx, t1, 3)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 3 {
		t.Errorf("want 3, got %d", len(due))
	}
}

func TestMemoryStore_StateTransitions(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	_ = m.Add(ctx, sampleEntry("x", t0))

	if err := m.MarkRunning(ctx, "x"); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	e, _ := m.Get(ctx, "x")
	if e.State != StateRunning {
		t.Errorf("want running, got %s", e.State)
	}

	if err := m.MarkCompleted(ctx, "x"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	e, _ = m.Get(ctx, "x")
	if e.State != StateCompleted {
		t.Errorf("want completed, got %s", e.State)
	}
}

func TestMemoryStore_MarkFailed(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	_ = m.Add(ctx, sampleEntry("y", t0))
	_ = m.MarkRunning(ctx, "y")

	now := time.Now()
	if err := m.MarkFailed(ctx, "y", "api error", now); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	e, _ := m.Get(ctx, "y")
	if e.State != StateFailed {
		t.Errorf("want failed, got %s", e.State)
	}
	if e.RetryCount != 1 {
		t.Errorf("want retry_count=1, got %d", e.RetryCount)
	}
	if e.LastError != "api error" {
		t.Errorf("want last_error=api error, got %s", e.LastError)
	}
}

func TestMemoryStore_MarkRetrying(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	_ = m.Add(ctx, sampleEntry("retry", t0))
	_ = m.MarkRunning(ctx, "retry")
	_ = m.MarkFailed(ctx, "retry", "api error", t1)

	if err := m.MarkRetrying(ctx, "retry"); err != nil {
		t.Fatalf("MarkRetrying: %v", err)
	}
	got, err := m.Get(ctx, "retry")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != StatePending {
		t.Fatalf("state = %s, want pending", got.State)
	}
	if got.LastError != "" {
		t.Fatalf("last_error = %q, want empty", got.LastError)
	}
	if got.RetryCount != 1 {
		t.Fatalf("retry_count = %d, want 1", got.RetryCount)
	}

	if err := m.MarkRetrying(ctx, "retry"); err == nil {
		t.Fatal("expected retrying pending entry to fail")
	}
	if err := m.MarkRetrying(ctx, "missing"); err == nil {
		t.Fatal("expected retrying missing entry to fail")
	}
}

func TestMemoryStore_Cancel(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	_ = m.Add(ctx, sampleEntry("z", t0))

	if err := m.MarkCancelled(ctx, "z"); err != nil {
		t.Fatalf("MarkCancelled: %v", err)
	}
	e, _ := m.Get(ctx, "z")
	if e.State != StateCancelled {
		t.Errorf("want cancelled, got %s", e.State)
	}
}

func TestMemoryStore_Next(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	_ = m.Add(ctx, sampleEntry("later", t1))
	_ = m.Add(ctx, sampleEntry("earlier", t0))

	next, err := m.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if next.CommandID != "earlier" {
		t.Errorf("want earlier, got %s", next.CommandID)
	}
}

func TestMemoryStore_NextNoPending(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	_, err := m.Next(ctx)
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestMemoryStore_InvalidTransition(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	_ = m.Add(ctx, sampleEntry("a", t0))

	// pending → completed is invalid (must go through running first)
	if err := m.MarkCompleted(ctx, "a"); err == nil {
		t.Fatal("want error for invalid transition pending→completed")
	}
}

func TestFileStore_AddListGet(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := NewFileStore(dir)

	e1 := sampleEntry("fid1", t1)
	e2 := sampleEntry("fid2", t0)

	if err := s.Add(ctx, e1); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(ctx, e2); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2, got %d", len(entries))
	}
	if entries[0].CommandID != "fid2" {
		t.Errorf("want fid2 first (earlier), got %s", entries[0].CommandID)
	}

	got, err := s.Get(ctx, "fid1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CommandID != "fid1" {
		t.Errorf("want fid1, got %s", got.CommandID)
	}
}

func TestFileStore_StateTransitionsAndCompleted(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := NewFileStore(dir)

	e := sampleEntry("cx1", t0)
	_ = s.Add(ctx, e)
	_ = s.MarkRunning(ctx, "cx1")
	if err := s.MarkCompleted(ctx, "cx1"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	// File should have moved to completed/
	completedDir := s.completedDir()
	des, err := os.ReadDir(completedDir)
	if err != nil {
		t.Fatalf("read completed dir: %v", err)
	}
	if len(des) != 1 {
		t.Errorf("want 1 file in completed/, got %d", len(des))
	}

	// Original file should be gone from main dir
	mainDes, _ := os.ReadDir(dir)
	jsonCount := 0
	for _, de := range mainDes {
		if !de.IsDir() && de.Name() != "." {
			jsonCount++
		}
	}
	// Only the completed/ subdir should remain in the main dir
	for _, de := range mainDes {
		if !de.IsDir() {
			t.Errorf("unexpected file in main dir after completion: %s", de.Name())
		}
	}
}

func TestFileStore_MarkFailed(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := NewFileStore(dir)

	_ = s.Add(ctx, sampleEntry("fx", t0))
	_ = s.MarkRunning(ctx, "fx")
	if err := s.MarkFailed(ctx, "fx", "transport error", time.Now()); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	got, err := s.Get(ctx, "fx")
	if err != nil {
		t.Fatalf("Get after fail: %v", err)
	}
	if got.State != StateFailed {
		t.Errorf("want failed, got %s", got.State)
	}
	if got.RetryCount != 1 {
		t.Errorf("want retry_count=1, got %d", got.RetryCount)
	}
}

func TestFileStore_DueSkipsMalformedAndNonPendingEntries(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := NewFileStore(dir)

	if err := s.Add(ctx, sampleEntry("due1", t0)); err != nil {
		t.Fatalf("Add due1: %v", err)
	}
	if err := s.Add(ctx, sampleEntry("future1", t2)); err != nil {
		t.Fatalf("Add future1: %v", err)
	}
	if err := s.Add(ctx, sampleEntry("running1", t0)); err != nil {
		t.Fatalf("Add running1: %v", err)
	}
	if err := s.MarkRunning(ctx, "running1"); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "malformed.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	due, err := s.Due(ctx, t1, 10)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("due len = %d, want 1: %+v", len(due), due)
	}
	if due[0].CommandID != "due1" {
		t.Fatalf("due[0] = %s, want due1", due[0].CommandID)
	}
}

func TestFileStore_DueLimitAndMissingDirectory(t *testing.T) {
	ctx := t.Context()

	missing := NewFileStore(filepath.Join(t.TempDir(), "missing"))
	due, err := missing.Due(ctx, t1, 10)
	if err != nil {
		t.Fatalf("Due missing dir: %v", err)
	}
	if due != nil {
		t.Fatalf("due missing dir = %#v, want nil", due)
	}

	s := NewFileStore(t.TempDir())
	if err := s.Add(ctx, sampleEntry("duea", t0)); err != nil {
		t.Fatalf("Add duea: %v", err)
	}
	if err := s.Add(ctx, sampleEntry("dueb", t0.Add(time.Minute))); err != nil {
		t.Fatalf("Add dueb: %v", err)
	}
	limited, err := s.Due(ctx, t1, 1)
	if err != nil {
		t.Fatalf("Due limited: %v", err)
	}
	if len(limited) != 1 || limited[0].CommandID != "duea" {
		t.Fatalf("limited due = %+v", limited)
	}
}

func TestFileStore_MarkRetryingAndCancel(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := NewFileStore(dir)

	if err := s.Add(ctx, sampleEntry("retryf", t0)); err != nil {
		t.Fatalf("Add retryf: %v", err)
	}
	if err := s.MarkRunning(ctx, "retryf"); err != nil {
		t.Fatalf("MarkRunning retryf: %v", err)
	}
	if err := s.MarkFailed(ctx, "retryf", "transport error", t1); err != nil {
		t.Fatalf("MarkFailed retryf: %v", err)
	}
	if err := s.MarkRetrying(ctx, "retryf"); err != nil {
		t.Fatalf("MarkRetrying retryf: %v", err)
	}
	got, err := s.Get(ctx, "retryf")
	if err != nil {
		t.Fatalf("Get retryf: %v", err)
	}
	if got.State != StatePending || got.LastError != "" || got.RetryCount != 1 {
		t.Fatalf("retryf after retrying = %+v", got)
	}
	if err := s.MarkRetrying(ctx, "retryf"); err == nil {
		t.Fatal("expected retrying pending file entry to fail")
	}

	if err := s.Add(ctx, sampleEntry("cancelf", t0)); err != nil {
		t.Fatalf("Add cancelf: %v", err)
	}
	if err := s.MarkCancelled(ctx, "cancelf"); err != nil {
		t.Fatalf("MarkCancelled cancelf: %v", err)
	}
	cancelled, err := s.Get(ctx, "cancelf")
	if err != nil {
		t.Fatalf("Get cancelf: %v", err)
	}
	if cancelled.State != StateCancelled {
		t.Fatalf("cancelled state = %s, want cancelled", cancelled.State)
	}

	if err := s.Add(ctx, sampleEntry("runningcancel", t0)); err != nil {
		t.Fatalf("Add runningcancel: %v", err)
	}
	if err := s.MarkRunning(ctx, "runningcancel"); err != nil {
		t.Fatalf("MarkRunning runningcancel: %v", err)
	}
	if err := s.MarkCancelled(ctx, "runningcancel"); err == nil {
		t.Fatal("expected cancelling running file entry to fail")
	}
}

func TestFileStore_Next(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := NewFileStore(dir)

	_ = s.Add(ctx, sampleEntry("n1", t1))
	_ = s.Add(ctx, sampleEntry("n2", t0))

	next, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if next.CommandID != "n2" {
		t.Errorf("want n2 (earlier), got %s", next.CommandID)
	}
}

func TestFileStore_MarkStaleRecoversRunningEntries(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	s := NewFileStore(dir)

	// Stale: running with old StartedAt — should be recovered to failed.
	stale := sampleEntry("stale1", t0)
	if err := s.Add(ctx, stale); err != nil {
		t.Fatalf("Add stale1: %v", err)
	}
	if err := s.MarkRunning(ctx, "stale1"); err != nil {
		t.Fatalf("MarkRunning stale1: %v", err)
	}
	got, err := s.Get(ctx, "stale1")
	if err != nil {
		t.Fatalf("Get stale1: %v", err)
	}
	old := time.Now().UTC().Add(-1 * time.Hour)
	got.StartedAt = &old
	if err := s.writeEntryAtomic(s.filePath(got), got); err != nil {
		t.Fatalf("write backdated entry: %v", err)
	}

	// Fresh: running but StartedAt is recent — must NOT be recovered.
	fresh := sampleEntry("fresh1", t0)
	if err := s.Add(ctx, fresh); err != nil {
		t.Fatalf("Add fresh1: %v", err)
	}
	if err := s.MarkRunning(ctx, "fresh1"); err != nil {
		t.Fatalf("MarkRunning fresh1: %v", err)
	}

	// Legacy: running without any StartedAt (pre-fix entry from disk) —
	// must NOT be recovered. Only entries with a non-zero StartedAt are
	// considered for stale recovery.
	legacy := sampleEntry("legacy1", t0)
	if err := s.Add(ctx, legacy); err != nil {
		t.Fatalf("Add legacy1: %v", err)
	}
	if err := s.MarkRunning(ctx, "legacy1"); err != nil {
		t.Fatalf("MarkRunning legacy1: %v", err)
	}
	legacyEntry, err := s.Get(ctx, "legacy1")
	if err != nil {
		t.Fatalf("Get legacy1: %v", err)
	}
	legacyEntry.StartedAt = nil
	if err := s.writeEntryAtomic(s.filePath(legacyEntry), legacyEntry); err != nil {
		t.Fatalf("write legacy entry: %v", err)
	}

	recovered, err := s.MarkStale(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("MarkStale: %v", err)
	}
	if len(recovered) != 1 {
		t.Fatalf("recovered = %d, want 1: %+v", len(recovered), recovered)
	}
	if recovered[0].CommandID != "stale1" {
		t.Fatalf("recovered[0] = %s, want stale1", recovered[0].CommandID)
	}
	if recovered[0].State != StateFailed {
		t.Fatalf("recovered[0].State = %s, want failed", recovered[0].State)
	}

	staleNow, err := s.Get(ctx, "stale1")
	if err != nil {
		t.Fatalf("Get stale1 after recovery: %v", err)
	}
	if staleNow.State != StateFailed {
		t.Fatalf("stale1 state = %s, want failed", staleNow.State)
	}
	if staleNow.LastError != "stale: process crashed mid-run" {
		t.Fatalf("stale1 last_error = %q", staleNow.LastError)
	}

	freshNow, err := s.Get(ctx, "fresh1")
	if err != nil {
		t.Fatalf("Get fresh1 after recovery: %v", err)
	}
	if freshNow.State != StateRunning {
		t.Fatalf("fresh1 state = %s, want still running", freshNow.State)
	}

	legacyNow, err := s.Get(ctx, "legacy1")
	if err != nil {
		t.Fatalf("Get legacy1 after recovery: %v", err)
	}
	if legacyNow.State != StateRunning {
		t.Fatalf("legacy1 state = %s, want still running (no StartedAt)", legacyNow.State)
	}
}

func TestMemoryStore_MarkStaleRecoversRunningEntries(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()

	stale := sampleEntry("stale1", t0)
	if err := m.Add(ctx, stale); err != nil {
		t.Fatalf("Add stale1: %v", err)
	}
	if err := m.MarkRunning(ctx, "stale1"); err != nil {
		t.Fatalf("MarkRunning stale1: %v", err)
	}
	e := m.entries["stale1"]
	old := time.Now().UTC().Add(-1 * time.Hour)
	e.StartedAt = &old
	m.entries["stale1"] = e

	fresh := sampleEntry("fresh1", t0)
	if err := m.Add(ctx, fresh); err != nil {
		t.Fatalf("Add fresh1: %v", err)
	}
	if err := m.MarkRunning(ctx, "fresh1"); err != nil {
		t.Fatalf("MarkRunning fresh1: %v", err)
	}

	recovered, err := m.MarkStale(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("MarkStale: %v", err)
	}
	if len(recovered) != 1 || recovered[0].CommandID != "stale1" {
		t.Fatalf("recovered = %+v, want [stale1]", recovered)
	}
	if recovered[0].State != StateFailed {
		t.Fatalf("recovered[0].State = %s, want failed", recovered[0].State)
	}

	got, _ := m.Get(ctx, "stale1")
	if got.State != StateFailed {
		t.Fatalf("stale1 state = %s, want failed", got.State)
	}
	if got.LastError != "stale: process crashed mid-run" {
		t.Fatalf("stale1 last_error = %q", got.LastError)
	}

	fresh1, _ := m.Get(ctx, "fresh1")
	if fresh1.State != StateRunning {
		t.Fatalf("fresh1 state = %s, want still running", fresh1.State)
	}
}

func TestFilenameHasCommandID_HyphenatedID(t *testing.T) {
	cases := []struct {
		name      string
		fileName  string
		commandID string
		want      bool
	}{
		{
			name:      "matches full hyphenated suffix",
			fileName:  "2027-01-01T09-00-00Z-post-create.json",
			commandID: "post-create",
			want:      true,
		},
		{
			name:      "rejects partial trailing token",
			fileName:  "2027-01-01T09-00-00Z-post-create.json",
			commandID: "create",
			want:      false,
		},
		{
			name:      "matches simple id",
			fileName:  "2027-01-01T09-00-00Z-fid1.json",
			commandID: "fid1",
			want:      true,
		},
		{
			name:      "rejects different command id with same suffix",
			fileName:  "2027-01-01T09-00-00Z-other.json",
			commandID: "post-create",
			want:      false,
		},
		{
			name:      "rejects non-json file",
			fileName:  "2027-01-01T09-00-00Z-fid1.txt",
			commandID: "fid1",
			want:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := filenameHasCommandID(tc.fileName, tc.commandID); got != tc.want {
				t.Fatalf("filenameHasCommandID(%q, %q) = %v, want %v", tc.fileName, tc.commandID, got, tc.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	t.Setenv("GOLINK_SCHEDULE_DIR", "/tmp/sched-test")
	p := ResolvePath()
	if p != "/tmp/sched-test" {
		t.Errorf("want /tmp/sched-test, got %s", p)
	}

	t.Setenv("GOLINK_SCHEDULE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/tmp/test-state")
	p = ResolvePath()
	if p != "/tmp/test-state/golink/schedule" {
		t.Errorf("want xdg schedule path, got %s", p)
	}

	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	p = ResolvePath()
	if p != "/tmp/home/.local/state/golink/schedule" {
		t.Errorf("want home schedule path, got %s", p)
	}

	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	p = ResolvePath()
	if p != filepath.Join(".local", "state", "golink", "schedule") {
		t.Errorf("want relative fallback path, got %s", p)
	}
}
