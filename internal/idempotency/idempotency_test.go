package idempotency

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestFileStore_RecordRedactsFreeText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	s := NewFileStore(path)

	// Result mirrors the post-create response shape: an opaque share URN plus
	// the sensitive free-text fields the caller is forbidden to persist.
	e := Entry{
		TS:        time.Now().UTC(),
		Key:       "abc-123",
		Command:   "post create",
		CommandID: "cmd_post_create_xxx",
		Status:    "ok",
		Result:    json.RawMessage(`{"text":"secret","author_urn":"urn:li:person:xyz","id":"urn:li:share:1"}`),
	}
	if err := s.Record(t.Context(), e); err != nil {
		t.Fatalf("record: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	text := string(raw)

	for _, leaked := range []string{"secret", "urn:li:person:xyz"} {
		if strings.Contains(text, leaked) {
			t.Errorf("idempotency file leaked %q: %s", leaked, text)
		}
	}
	if !strings.Contains(text, "urn:li:share:1") {
		t.Errorf("expected share id to survive redaction: %s", text)
	}

	got, hit, err := s.Lookup(t.Context(), "abc-123", "post create")
	if err != nil || !hit {
		t.Fatalf("lookup after redacted record: hit=%v err=%v", hit, err)
	}
	if got.CommandID != "cmd_post_create_xxx" {
		t.Errorf("command_id corrupted by redaction: %q", got.CommandID)
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

func TestFileStore_LogsCorruptedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")

	// Mix one valid record with one garbage line. readAll must surface
	// the corrupt line via the injected slog handler instead of silently
	// dropping it. Use a fresh TS so the entry falls within Lookup's
	// 24h window.
	validEntry := Entry{
		TS:      time.Now().UTC(),
		Key:     "k1",
		Command: "post create",
		Status:  "ok",
	}
	validBytes, err := json.Marshal(validEntry)
	if err != nil {
		t.Fatalf("marshal valid: %v", err)
	}
	garbage := `{not json`
	if err := os.WriteFile(path, []byte(string(validBytes)+"\n"+garbage+"\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	logger := slog.New(handler)
	s := NewFileStore(path, WithLogger(logger))

	// Trigger readAll via Lookup.
	got, hit, err := s.Lookup(t.Context(), "k1", "post create")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !hit {
		t.Fatalf("expected hit for valid entry")
	}
	if got.Key != "k1" {
		t.Fatalf("got.Key = %q, want k1", got.Key)
	}

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected WARN log, got: %s", out)
	}
	if !strings.Contains(out, "corrupted") {
		t.Fatalf("expected message about corrupted line, got: %s", out)
	}
}

func TestFileStore_ConcurrentRecordIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")

	// One FileStore per writer simulates two processes appending to the
	// same file: each Store has its own mu so the in-process mutex provides
	// no cross-store ordering. Without flock, large payloads interleave.
	bigResult, _ := json.Marshal(map[string]string{
		"id":   "urn:li:share:1",
		"blob": strings.Repeat("Y", 8192),
	})

	const writers = 16
	const perWriter = 5
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := range writers {
		go func(w int) {
			defer wg.Done()
			s := NewFileStore(path)
			for i := range perWriter {
				e := Entry{
					TS:        time.Now().UTC(),
					Key:       "k_" + string(rune('a'+w)) + "_" + string(rune('0'+i)),
					Command:   "post create",
					CommandID: "cmd_" + string(rune('a'+w)) + "_" + string(rune('0'+i)),
					Status:    "ok",
					Result:    bigResult,
				}
				if err := s.Record(t.Context(), e); err != nil {
					t.Errorf("Record: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	if len(lines) != writers*perWriter {
		t.Fatalf("got %d lines, want %d", len(lines), writers*perWriter)
	}
	for i, line := range lines {
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("line %d failed to parse — interleaved write detected: %v", i+1, err)
		}
	}
}

func TestFileStore_ConcurrentRecordDuringPruneDoesNotLoseEntries(t *testing.T) {
	// Given: a FileStore seeded with old + fresh entries.
	// When: one goroutine repeatedly Prunes (read+rewrite) while another
	// repeatedly Records new entries.
	// Then: every Record we observed as success must be present in the final
	// file. Without sidecar locking, Prune's read→rename window swallows
	// concurrent appends.
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")

	pruner := NewFileStore(path)
	now := time.Now().UTC()
	for i := range 4 {
		old := Entry{
			TS:      now.Add(-25 * time.Hour),
			Key:     "old-" + string(rune('a'+i)),
			Command: "post create",
			Status:  "ok",
		}
		if err := pruner.Record(t.Context(), old); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	const writers = 8
	const perWriter = 20
	const pruneRounds = 30

	var wg sync.WaitGroup
	wg.Add(writers + 1)

	for w := range writers {
		go func(w int) {
			defer wg.Done()
			s := NewFileStore(path)
			for i := range perWriter {
				e := Entry{
					TS:      time.Now().UTC(),
					Key:     "k_" + string(rune('a'+w)) + "_" + string(rune('A'+i)),
					Command: "post create",
					Status:  "ok",
				}
				if err := s.Record(t.Context(), e); err != nil {
					t.Errorf("Record: %v", err)
					return
				}
			}
		}(w)
	}

	go func() {
		defer wg.Done()
		for range pruneRounds {
			if err := pruner.Prune(t.Context(), 24*time.Hour, 10_000); err != nil {
				t.Errorf("Prune: %v", err)
				return
			}
		}
	}()

	wg.Wait()

	final := pruner.mustReadAll(t)
	got := make(map[string]struct{}, len(final))
	for _, e := range final {
		got[e.Key] = struct{}{}
	}

	want := writers * perWriter
	have := 0
	for w := range writers {
		for i := range perWriter {
			key := "k_" + string(rune('a'+w)) + "_" + string(rune('A'+i))
			if _, ok := got[key]; ok {
				have++
			}
		}
	}
	if have != want {
		t.Fatalf("lost %d entries to Prune race; have=%d want=%d", want-have, have, want)
	}
}

func TestFileStore_AcquireSerialisesAcrossInstances(t *testing.T) {
	// Two FileStore instances at the same path simulate two golink processes.
	// Acquire on the second instance must block until the first releases.
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	a := NewFileStore(path)
	b := NewFileStore(path)

	release1, err := a.Acquire(t.Context(), "shared-key")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := b.Acquire(t.Context(), "shared-key")
		if err != nil {
			t.Errorf("second acquire: %v", err)
			close(acquired)
			return
		}
		close(acquired)
		if err := release2(); err != nil {
			t.Errorf("second release: %v", err)
		}
	}()

	select {
	case <-acquired:
		t.Fatal("second Acquire returned before first release; cross-process lock not held")
	case <-time.After(100 * time.Millisecond):
	}

	if err := release1(); err != nil {
		t.Fatalf("first release: %v", err)
	}

	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second Acquire did not unblock after first release")
	}
}

func TestFileStore_AcquirePerKeyDoesNotSerialiseDistinctKeys(t *testing.T) {
	// Distinct keys must not contend — Acquire is per-key.
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")
	a := NewFileStore(path)
	b := NewFileStore(path)

	release1, err := a.Acquire(t.Context(), "key-A")
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	defer func() {
		if err := release1(); err != nil {
			t.Errorf("release A: %v", err)
		}
	}()

	done := make(chan struct{})
	go func() {
		release2, err := b.Acquire(t.Context(), "key-B")
		if err != nil {
			t.Errorf("acquire B: %v", err)
			close(done)
			return
		}
		if err := release2(); err != nil {
			t.Errorf("release B: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Acquire on distinct key blocked; locks must be per-key")
	}
}

func TestFileStore_AcquireClosesLookupRecordTOCTOU(t *testing.T) {
	// Regression for H2-followup: two FileStore instances both Acquire→Lookup
	// →Record the same key. Exactly ONE must observe a miss and write the
	// entry; the other must see the cached value. Without per-key cross-
	// process locking, both lookups race past the miss and double-record.
	path := filepath.Join(t.TempDir(), "idempotency.jsonl")

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	hits := make([]bool, workers)

	for i := range workers {
		go func(i int) {
			defer wg.Done()
			s := NewFileStore(path)
			release, err := s.Acquire(t.Context(), "shared-key")
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			defer func() {
				if err := release(); err != nil {
					t.Errorf("release: %v", err)
				}
			}()

			_, hit, err := s.Lookup(t.Context(), "shared-key", "post create")
			if err != nil {
				t.Errorf("lookup: %v", err)
				return
			}
			hits[i] = hit
			if hit {
				return
			}
			entry := Entry{
				TS:        time.Now().UTC(),
				Key:       "shared-key",
				Command:   "post create",
				CommandID: "cmd_x",
				Status:    "ok",
			}
			if err := s.Record(t.Context(), entry); err != nil {
				t.Errorf("record: %v", err)
			}
		}(i)
	}
	wg.Wait()

	final := NewFileStore(path).mustReadAll(t)
	matching := 0
	for _, e := range final {
		if e.Key == "shared-key" {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("expected exactly 1 entry for shared-key, got %d", matching)
	}

	misses := 0
	for _, hit := range hits {
		if !hit {
			misses++
		}
	}
	if misses != 1 {
		t.Fatalf("expected exactly 1 miss across %d workers, got %d", workers, misses)
	}
}

func TestMemoryStore_AcquireSerialisesSameKey(t *testing.T) {
	s := NewMemoryStore()
	release1, err := s.Acquire(t.Context(), "k")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := s.Acquire(t.Context(), "k")
		if err != nil {
			t.Errorf("second acquire: %v", err)
			close(acquired)
			return
		}
		close(acquired)
		_ = release2()
	}()

	select {
	case <-acquired:
		t.Fatal("MemoryStore.Acquire did not serialise same key")
	case <-time.After(50 * time.Millisecond):
	}

	if err := release1(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second Acquire never returned after release")
	}
}

func TestNoopStore_AcquireIsNoop(t *testing.T) {
	s := NoopStore{}
	release, err := s.Acquire(t.Context(), "k")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Same key twice in a row must not block.
	release2, err := s.Acquire(t.Context(), "k")
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if err := release2(); err != nil {
		t.Fatalf("second release: %v", err)
	}
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
