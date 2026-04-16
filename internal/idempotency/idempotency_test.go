package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStoreLookupMiss(t *testing.T) {
	s := NewMemoryStore()
	_, hit, err := s.Lookup(context.Background(), "k1", "post create")
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
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, hit, err := s.Lookup(context.Background(), "k1", "post create")
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

func TestMemoryStoreMismatch(t *testing.T) {
	s := NewMemoryStore()
	e := Entry{
		TS:      time.Now().UTC(),
		Key:     "k1",
		Command: "post create",
		Status:  "ok",
	}
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	_, _, err := s.Lookup(context.Background(), "k1", "post delete")
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
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	_, hit, err := s.Lookup(context.Background(), "k1", "post create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Fatal("expected miss for expired entry")
	}
}

func TestFileStoreLookupMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	s := NewFileStore(path)

	_, hit, err := s.Lookup(context.Background(), "k1", "post create")
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
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, hit, err := s.Lookup(context.Background(), "abc-123", "post create")
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
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	_, _, err := s.Lookup(context.Background(), "k1", "comment add")
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
		if err := s.Record(context.Background(), e); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	if err := s.Prune(context.Background(), 24*time.Hour, 10000); err != nil {
		t.Fatalf("prune: %v", err)
	}

	// old-1 should be gone; fresh-1 and fresh-2 should remain
	_, hit, err := s.Lookup(context.Background(), "old-1", "post create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Error("expected old-1 to be pruned")
	}

	_, hit, err = s.Lookup(context.Background(), "fresh-2", "comment add")
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
		if err := s.Record(context.Background(), e); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	if err := s.Prune(context.Background(), 24*time.Hour, 3); err != nil {
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
}
