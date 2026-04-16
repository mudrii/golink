package idempotency

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultWindow   = 24 * time.Hour
	defaultMaxLines = 10_000
)

// ErrKeyCommandMismatch is returned when an idempotency key was previously used
// for a different command.
var ErrKeyCommandMismatch = errors.New("idempotency key-command mismatch")

// Entry is a single idempotency record written after a successful mutation.
type Entry struct {
	TS         time.Time       `json:"ts"`
	Key        string          `json:"key"`
	Command    string          `json:"command"`
	CommandID  string          `json:"command_id"`
	Status     string          `json:"status"`
	HTTPStatus int             `json:"http_status,omitempty"`
	RequestID  string          `json:"request_id,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

// Store is the idempotency record backend.
type Store interface {
	// Lookup returns (entry, true, nil) on a cache hit, (zero, false, nil) on a
	// miss, and (zero, false, err) if the key was used for a different command.
	Lookup(ctx context.Context, key, command string) (Entry, bool, error)

	// Record persists a new idempotency entry.
	Record(ctx context.Context, entry Entry) error

	// Prune removes entries older than window and caps the store at maxLines.
	Prune(ctx context.Context, window time.Duration, maxLines int) error
}

// FileStore implements Store using an append-only JSONL file.
// It is safe for concurrent use within one process.
type FileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore returns a FileStore that writes to path.
// If path is empty, ResolvePath() is used.
func NewFileStore(path string) *FileStore {
	if path == "" {
		path = ResolvePath()
	}
	return &FileStore{path: path}
}

// Lookup scans the file for the most recent entry matching key.
// Returns ErrKeyCommandMismatch wrapped in an error when the key exists but
// belongs to a different command.
func (s *FileStore) Lookup(_ context.Context, key, command string) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAll()
	if err != nil {
		return Entry{}, false, fmt.Errorf("idempotency lookup: %w", err)
	}

	cutoff := time.Now().UTC().Add(-defaultWindow)
	var found *Entry
	for i := range entries {
		e := &entries[i]
		if e.Key != key {
			continue
		}
		if e.TS.Before(cutoff) {
			continue
		}
		found = e
	}

	if found == nil {
		return Entry{}, false, nil
	}
	if found.Command != command {
		return Entry{}, false, fmt.Errorf("%w: key %q was already used for %q (not %q); use a different key",
			ErrKeyCommandMismatch, key, found.Command, command)
	}
	return *found, true, nil
}

// Record appends entry to the file, creating it if necessary.
func (s *FileStore) Record(_ context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("idempotency mkdir: %w", err)
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("idempotency open: %w", err)
	}

	line, err := json.Marshal(entry)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("idempotency marshal: %w", err)
	}
	line = append(line, '\n')

	_, writeErr := f.Write(line)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("idempotency write: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("idempotency close: %w", closeErr)
	}
	return nil
}

// Prune removes entries older than window and caps the file at maxLines,
// keeping the most recent entries. A no-op when the file does not exist.
func (s *FileStore) Prune(_ context.Context, window time.Duration, maxLines int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAll()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("idempotency prune read: %w", err)
	}

	cutoff := time.Now().UTC().Add(-window)
	kept := entries[:0]
	for i := range entries {
		if !entries[i].TS.Before(cutoff) {
			kept = append(kept, entries[i])
		}
	}
	if maxLines > 0 && len(kept) > maxLines {
		kept = kept[len(kept)-maxLines:]
	}

	if len(kept) == len(entries) {
		return nil
	}

	return s.writeAll(kept)
}

func (s *FileStore) readAll() ([]Entry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var entries []Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}

func (s *FileStore) writeAll(entries []Entry) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("idempotency mkdir: %w", err)
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("idempotency open for write: %w", err)
	}

	enc := json.NewEncoder(f)
	var writeErr error
	for i := range entries {
		if writeErr = enc.Encode(&entries[i]); writeErr != nil {
			break
		}
	}
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("idempotency write: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("idempotency close: %w", closeErr)
	}
	return nil
}

// MemoryStore is an in-memory Store for tests; safe for concurrent use.
type MemoryStore struct {
	mu      sync.Mutex
	entries []Entry
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Lookup scans memory for the most recent matching key within the default window.
func (m *MemoryStore) Lookup(_ context.Context, key, command string) (Entry, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().UTC().Add(-defaultWindow)
	var found *Entry
	for i := range m.entries {
		e := &m.entries[i]
		if e.Key != key {
			continue
		}
		if e.TS.Before(cutoff) {
			continue
		}
		found = e
	}

	if found == nil {
		return Entry{}, false, nil
	}
	if found.Command != command {
		return Entry{}, false, fmt.Errorf("%w: key %q was already used for %q (not %q); use a different key",
			ErrKeyCommandMismatch, key, found.Command, command)
	}
	return *found, true, nil
}

// Record appends entry to memory.
func (m *MemoryStore) Record(_ context.Context, entry Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

// Prune is a no-op for MemoryStore (tests control entries directly).
func (m *MemoryStore) Prune(_ context.Context, _ time.Duration, _ int) error {
	return nil
}

// Entries returns a copy of all stored entries; used in tests.
func (m *MemoryStore) Entries() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Entry, len(m.entries))
	copy(out, m.entries)
	return out
}

// NoopStore discards every operation; used when idempotency is disabled.
type NoopStore struct{}

// Lookup always returns a miss.
func (NoopStore) Lookup(_ context.Context, _, _ string) (Entry, bool, error) {
	return Entry{}, false, nil
}

// Record does nothing.
func (NoopStore) Record(_ context.Context, _ Entry) error { return nil }

// Prune does nothing.
func (NoopStore) Prune(_ context.Context, _ time.Duration, _ int) error { return nil }

// ResolvePath returns the effective idempotency store path.
// Precedence: GOLINK_IDEMPOTENCY_PATH > XDG_STATE_HOME/golink/idempotency.jsonl >
// $HOME/.local/state/golink/idempotency.jsonl.
func ResolvePath() string {
	if v := os.Getenv("GOLINK_IDEMPOTENCY_PATH"); v != "" {
		return v
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "golink", "idempotency.jsonl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "state", "golink", "idempotency.jsonl")
	}
	return filepath.Join(home, ".local", "state", "golink", "idempotency.jsonl")
}
