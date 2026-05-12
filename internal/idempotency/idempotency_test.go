package idempotency

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStoreLookupMiss(t *testing.T) {
	s := NewMemoryStore()
	_, hit, err := s.Lookup(t.Context(), "k1", "post create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Fatal("expected miss, got hit")
	}
}

func TestMemoryStoreLookupHit(t *testing.T) {
	s := NewMemoryStore()
	e := Entry{
		TS:      time.Now().UTC(),
		Key:     "k1",
		Command: "post create",
		Status:  "ok",
	}
	if err := s.Record(t.Context(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, hit, err := s.Lookup(t.Context(), "k1", "post create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hit {
		t.Fatal("expected hit, got miss")
	}
	if got.Key != "k1" {
		t.Errorf("key: want k1, got %q", got.Key)
	}
}

func TestMemoryStoreEntriesReturnsCopyAndPruneNoops(t *testing.T) {
	s := NewMemoryStore()
	e := Entry{
		TS:      time.Now().UTC(),
		Key:     "k1",
		Command: "post create",
		Status:  "ok",
	}
	if err := s.Record(t.Context(), e); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := s.Prune(t.Context(), time.Nanosecond, 0); err != nil {
		t.Fatalf("prune: %v", err)
	}

	entries := s.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	entries[0].Key = "mutated"

	got := s.Entries()
	if got[0].Key != "k1" {
		t.Fatalf("Entries returned mutable backing slice; key = %q", got[0].Key)
	}
}

func TestMemoryStoreMismatch(t *testing.T) {
	s := NewMemoryStore()
	e := Entry{
		TS:      time.Now().UTC(),
		Key:     "k1",
		Command: "post create",
		Status:  "ok",
	}
	if err := s.Record(t.Context(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	_, _, err := s.Lookup(t.Context(), "k1", "post delete")
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !errors.Is(err, ErrKeyCommandMismatch) {
		t.Errorf("expected ErrKeyCommandMismatch, got %v", err)
	}
}

func TestMemoryStoreExpiredEntryIsAMiss(t *testing.T) {
	s := NewMemoryStore()
	e := Entry{
		TS:      time.Now().UTC().Add(-25 * time.Hour),
		Key:     "k1",
		Command: "post create",
		Status:  "ok",
	}
	if err := s.Record(t.Context(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	_, hit, err := s.Lookup(t.Context(), "k1", "post create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Fatal("expected miss for expired entry")
	}
}

func TestNoopStoreAlwaysMissesAndDiscards(t *testing.T) {
	store := NoopStore{}
	entry := Entry{
		TS:      time.Now().UTC(),
		Key:     "k1",
		Command: "post create",
		Status:  "ok",
	}
	if err := store.Record(t.Context(), entry); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := store.Prune(t.Context(), 24*time.Hour, 1); err != nil {
		t.Fatalf("prune: %v", err)
	}
	got, hit, err := store.Lookup(t.Context(), "k1", "post create")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if hit || got.Key != "" {
		t.Fatalf("noop lookup = (%+v, %v), want zero miss", got, hit)
	}
}

func TestFileStoreLookupMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	s := NewFileStore(path)

	_, hit, err := s.Lookup(t.Context(), "k1", "post create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Fatal("expected miss")
	}
}

func TestFileStoreRecordAndLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	s := NewFileStore(path)

	result, _ := json.Marshal(map[string]string{"id": "post-1"})
	e := Entry{
		TS:         time.Now().UTC(),
		Key:        "abc-123",
		Command:    "post create",
		CommandID:  "cmd_post_create_xxx",
		Status:     "ok",
		HTTPStatus: 201,
		Result:     result,
	}
	if err := s.Record(t.Context(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, hit, err := s.Lookup(t.Context(), "abc-123", "post create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hit {
		t.Fatal("expected hit")
	}
	if got.CommandID != "cmd_post_create_xxx" {
		t.Errorf("command_id: want cmd_post_create_xxx, got %q", got.CommandID)
	}
	if got.HTTPStatus != 201 {
		t.Errorf("http_status: want 201, got %d", got.HTTPStatus)
	}
}

func TestFileStoreMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	s := NewFileStore(path)

	e := Entry{
		TS:      time.Now().UTC(),
		Key:     "k1",
		Command: "post create",
		Status:  "ok",
	}
	if err := s.Record(t.Context(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	_, _, err := s.Lookup(t.Context(), "k1", "comment add")
	if !errors.Is(err, ErrKeyCommandMismatch) {
		t.Errorf("expected ErrKeyCommandMismatch, got %v", err)
	}
}

func TestFileStorePrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	s := NewFileStore(path)

	now := time.Now().UTC()
	entries := []Entry{
		{TS: now.Add(-25 * time.Hour), Key: "old-1", Command: "post create", Status: "ok"},
		{TS: now.Add(-23 * time.Hour), Key: "fresh-1", Command: "post create", Status: "ok"},
		{TS: now.Add(-1 * time.Hour), Key: "fresh-2", Command: "comment add", Status: "ok"},
	}
	for _, e := range entries {
		if err := s.Record(t.Context(), e); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	if err := s.Prune(t.Context(), 24*time.Hour, 10000); err != nil {
		t.Fatalf("prune: %v", err)
	}

	// old-1 should be gone; fresh-1 and fresh-2 should remain
	_, hit, err := s.Lookup(t.Context(), "old-1", "post create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Error("expected old-1 to be pruned")
	}

	_, hit, err = s.Lookup(t.Context(), "fresh-2", "comment add")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hit {
		t.Error("expected fresh-2 to survive prune")
	}
}

func TestFileStorePruneMaxLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	s := NewFileStore(path)

	now := time.Now().UTC()
	for i := range 5 {
		e := Entry{
			TS:      now.Add(-time.Duration(5-i) * time.Minute),
			Key:     "k" + string(rune('0'+i)),
			Command: "post create",
			Status:  "ok",
		}
		if err := s.Record(t.Context(), e); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	if err := s.Prune(t.Context(), 24*time.Hour, 3); err != nil {
		t.Fatalf("prune: %v", err)
	}

	remaining := s.mustReadAll(t)
	if len(remaining) != 3 {
		t.Errorf("expected 3 entries after maxLines prune, got %d", len(remaining))
	}
}

func (s *FileStore) mustReadAll(t *testing.T) []Entry {
	t.Helper()
	entries, err := s.readAll()
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	return entries
}

func TestResolvePath(t *testing.T) {
	t.Setenv("GOLINK_IDEMPOTENCY_PATH", "/tmp/test-idempotency.jsonl")
	got := ResolvePath()
	if got != "/tmp/test-idempotency.jsonl" {
		t.Errorf("expected env override, got %q", got)
	}

	t.Setenv("GOLINK_IDEMPOTENCY_PATH", "")
	t.Setenv("XDG_STATE_HOME", "/tmp/test-state")
	got = ResolvePath()
	if got != "/tmp/test-state/golink/idempotency.jsonl" {
		t.Errorf("expected xdg path, got %q", got)
	}

	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	got = ResolvePath()
	if got != "/tmp/home/.local/state/golink/idempotency.jsonl" {
		t.Errorf("expected home path, got %q", got)
	}

	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	got = ResolvePath()
	if got != filepath.Join(".local", "state", "golink", "idempotency.jsonl") {
		t.Errorf("expected relative fallback path, got %q", got)
	}
}
