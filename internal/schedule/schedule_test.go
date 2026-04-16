package schedule

import (
	"os"
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

func TestResolvePath(t *testing.T) {
	t.Setenv("GOLINK_SCHEDULE_DIR", "/tmp/sched-test")
	p := ResolvePath()
	if p != "/tmp/sched-test" {
		t.Errorf("want /tmp/sched-test, got %s", p)
	}
}
