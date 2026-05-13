package idempotency

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mudrii/golink/internal/privacy"
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
	mu     sync.Mutex
	path   string
	logger *slog.Logger
	// Now is overridable for testing. Defaults to time.Now when nil.
	Now func() time.Time
}

// Option configures a FileStore at construction time.
type Option func(*FileStore)

// WithLogger overrides the slog.Logger used by FileStore to surface
// corrupted JSONL lines. When unset, slog.Default() is used.
func WithLogger(logger *slog.Logger) Option {
	return func(s *FileStore) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// NewFileStore returns a FileStore that writes to path.
// If path is empty, ResolvePath() is used.
func NewFileStore(path string, opts ...Option) *FileStore {
	if path == "" {
		path = ResolvePath()
	}
	s := &FileStore{path: path, logger: slog.Default()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *FileStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
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

	cutoff := s.now().Add(-defaultWindow)
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

// Record appends entry to the file, creating it if necessary. The append is
// guarded by an exclusive flock on a sidecar lock file so concurrent golink
// processes serialize their writes and never interleave with a Prune rewrite.
// POSIX only guarantees atomic O_APPEND up to PIPE_BUF, and Prune's tmp+rename
// detaches any flock held on the data file itself — locking a stable sidecar
// path survives that rename.
func (s *FileStore) Record(_ context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(entry.Result) > 0 {
		entry.Result = privacy.JSON(entry.Result)
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("idempotency marshal: %w", err)
	}
	line = append(line, '\n')

	return s.withLock(func() error {
		f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("idempotency open: %w", err)
		}
		_, writeErr := f.Write(line)
		closeErr := f.Close()
		if writeErr != nil {
			return fmt.Errorf("idempotency write: %w", writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("idempotency close: %w", closeErr)
		}
		return nil
	})
}

// withLock acquires an exclusive cross-process lock on a sidecar file alongside
// s.path, then runs fn. The sidecar (rather than s.path itself) is locked so
// the lock survives Prune's tmp+rename cycle, which would otherwise detach the
// lock from the live inode.
func (s *FileStore) withLock(fn func() error) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("idempotency mkdir: %w", err)
	}

	lockPath := s.path + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("idempotency lock open: %w", err)
	}
	if err := lockFile(lf); err != nil {
		_ = lf.Close()
		return fmt.Errorf("idempotency lock: %w", err)
	}

	fnErr := fn()
	unlockErr := unlockFile(lf)
	closeErr := lf.Close()
	if fnErr != nil {
		return fnErr
	}
	if unlockErr != nil {
		return unlockErr
	}
	if closeErr != nil {
		return fmt.Errorf("idempotency lock close: %w", closeErr)
	}
	return nil
}

// Prune removes entries older than window and caps the file at maxLines,
// keeping the most recent entries. A no-op when the file does not exist.
// The read-modify-write cycle runs under the same sidecar flock that Record
// uses, so concurrent writers cannot slip an append between readAll and the
// atomic rename in writeAll.
func (s *FileStore) Prune(_ context.Context, window time.Duration, maxLines int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withLock(func() error {
		entries, err := s.readAll()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("idempotency prune read: %w", err)
		}

		cutoff := s.now().Add(-window)
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
	})
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
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			s.logger.Warn("idempotency: corrupted line, skipped",
				"path", s.path,
				"line", lineNum,
				"err", err.Error(),
			)
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

	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("idempotency open for write: %w", err)
	}
	tmpPath := tmp.Name()

	enc := json.NewEncoder(tmp)
	var writeErr error
	for i := range entries {
		if writeErr = enc.Encode(&entries[i]); writeErr != nil {
			break
		}
	}
	closeErr := tmp.Close()
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("idempotency write: %w", writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("idempotency close: %w", closeErr)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("idempotency rename: %w", err)
	}
	return nil
}

// MemoryStore is an in-memory Store for tests; safe for concurrent use.
type MemoryStore struct {
	mu      sync.Mutex
	entries []Entry
	// Now is overridable for testing. Defaults to time.Now when nil.
	Now func() time.Time
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (m *MemoryStore) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

// Lookup scans memory for the most recent matching key within the default window.
func (m *MemoryStore) Lookup(_ context.Context, key, command string) (Entry, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := m.now().Add(-defaultWindow)
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
