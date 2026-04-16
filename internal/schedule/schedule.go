package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// State is the lifecycle state of a scheduled post entry.
type State string

const (
	// StatePending means the entry is queued and not yet executed.
	StatePending State = "pending"
	// StateRunning means execution is currently in progress.
	StateRunning State = "running"
	// StateCompleted means the post was successfully created.
	StateCompleted State = "completed"
	// StateFailed means execution was attempted but failed.
	StateFailed State = "failed"
	// StateCancelled means the entry was cancelled before execution.
	StateCancelled State = "cancelled"
)

// ErrNotFound is returned when a command_id does not exist in the store.
var ErrNotFound = errors.New("schedule entry not found")

// ErrInvalidState is returned when a state transition is not allowed.
var ErrInvalidState = errors.New("schedule entry is in invalid state for this operation")

// Request holds the post request parameters stored at schedule time.
type Request struct {
	Text       string `json:"text"`
	Visibility string `json:"visibility"`
	ImagePath  string `json:"image_path,omitempty"`
	ImageAlt   string `json:"image_alt,omitempty"`
}

// Entry is the on-disk representation of a scheduled post.
type Entry struct {
	CommandID      string     `json:"command_id"`
	State          State      `json:"state"`
	ScheduledAt    time.Time  `json:"scheduled_at"`
	CreatedAt      time.Time  `json:"created_at"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	RetryCount     int        `json:"retry_count,omitempty"`
	Profile        string     `json:"profile"`
	Transport      string     `json:"transport"`
	Request        Request    `json:"request"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
}

// Store is the schedule record backend.
type Store interface {
	// Add writes a new pending entry to the store.
	Add(ctx context.Context, entry Entry) error

	// List returns all entries sorted by scheduled_at ascending.
	List(ctx context.Context) ([]Entry, error)

	// Get returns the entry for the given command_id.
	Get(ctx context.Context, commandID string) (Entry, error)

	// Due returns pending entries whose scheduled_at <= now, up to limit.
	Due(ctx context.Context, now time.Time, limit int) ([]Entry, error)

	// MarkRunning transitions a pending entry to running.
	MarkRunning(ctx context.Context, commandID string) error

	// MarkCompleted transitions a running entry to completed.
	MarkCompleted(ctx context.Context, commandID string) error

	// MarkFailed transitions a running entry to failed. It increments
	// retry_count and stores the error message. Failed is terminal —
	// use MarkRetrying to transition back to pending for operator retry.
	MarkFailed(ctx context.Context, commandID, lastError string, now time.Time) error

	// MarkRetrying transitions a failed entry back to pending so it can
	// be re-run. retry_count is preserved; last_error is cleared.
	MarkRetrying(ctx context.Context, commandID string) error

	// MarkCancelled transitions a pending entry to cancelled.
	MarkCancelled(ctx context.Context, commandID string) error

	// Next returns the earliest pending entry.
	Next(ctx context.Context) (Entry, error)
}

// FileStore implements Store using the schedule directory.
// Files are named <scheduled_at_rfc3339>-<command_id>.json for lexical sort.
// Completed entries are moved to a completed/ subdirectory.
type FileStore struct {
	mu  sync.Mutex
	dir string
}

// NewFileStore returns a FileStore rooted at dir.
// If dir is empty, ResolvePath() is used.
func NewFileStore(dir string) *FileStore {
	if dir == "" {
		dir = ResolvePath()
	}
	return &FileStore{dir: dir}
}

func (s *FileStore) ensureDir() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("schedule mkdir: %w", err)
	}
	return nil
}

func (s *FileStore) completedDir() string {
	return filepath.Join(s.dir, "completed")
}

func (s *FileStore) filePath(entry Entry) string {
	ts := entry.ScheduledAt.UTC().Format(time.RFC3339)
	// Replace colons so the filename is safe on all filesystems.
	ts = strings.ReplaceAll(ts, ":", "-")
	return filepath.Join(s.dir, ts+"-"+entry.CommandID+".json")
}

func (s *FileStore) completedPath(entry Entry) string {
	ts := entry.ScheduledAt.UTC().Format(time.RFC3339)
	ts = strings.ReplaceAll(ts, ":", "-")
	return filepath.Join(s.completedDir(), ts+"-"+entry.CommandID+".json")
}

// Add writes a new pending entry.
func (s *FileStore) Add(_ context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDir(); err != nil {
		return err
	}

	entry.State = StatePending
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("schedule marshal: %w", err)
	}

	path := s.filePath(entry)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("schedule write: %w", err)
	}
	return nil
}

// List returns all entries (pending/running/failed in main dir, completed in completed/).
func (s *FileStore) List(_ context.Context) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var entries []Entry

	if err := s.ensureDir(); err != nil {
		return nil, err
	}

	dirs := []string{s.dir, s.completedDir()}
	for _, dir := range dirs {
		des, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("schedule list: %w", err)
		}
		for _, de := range des {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, de.Name()))
			if err != nil {
				continue
			}
			var e Entry
			if err := json.Unmarshal(data, &e); err != nil {
				continue
			}
			entries = append(entries, e)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ScheduledAt.Before(entries[j].ScheduledAt)
	})
	return entries, nil
}

// Get returns the entry for commandID, searching main dir then completed/.
func (s *FileStore) Get(_ context.Context, commandID string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dirs := []string{s.dir, s.completedDir()}
	for _, dir := range dirs {
		des, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Entry{}, fmt.Errorf("schedule get readdir: %w", err)
		}
		for _, de := range des {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
				continue
			}
			if !strings.Contains(de.Name(), commandID) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, de.Name()))
			if err != nil {
				continue
			}
			var e Entry
			if err := json.Unmarshal(data, &e); err != nil {
				continue
			}
			if e.CommandID == commandID {
				return e, nil
			}
		}
	}
	return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, commandID)
}

// Due returns pending entries with scheduled_at <= now, up to limit, sorted by scheduled_at.
func (s *FileStore) Due(_ context.Context, now time.Time, limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	des, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("schedule due readdir: %w", err)
	}

	var due []Entry
	for _, de := range des {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, de.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.State == StatePending && !e.ScheduledAt.After(now) {
			due = append(due, e)
		}
	}

	sort.Slice(due, func(i, j int) bool {
		return due[i].ScheduledAt.Before(due[j].ScheduledAt)
	})

	if limit > 0 && len(due) > limit {
		due = due[:limit]
	}
	return due, nil
}

// MarkRunning transitions pending → running and rewrites the file.
func (s *FileStore) MarkRunning(_ context.Context, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateEntry(commandID, func(e *Entry) error {
		if e.State != StatePending {
			return fmt.Errorf("%w: %s (state=%s, want pending)", ErrInvalidState, commandID, e.State)
		}
		e.State = StateRunning
		return nil
	})
}

// MarkCompleted transitions running → completed and moves the file to completed/.
func (s *FileStore) MarkCompleted(_ context.Context, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, path, err := s.findInMain(commandID)
	if err != nil {
		return err
	}
	if entry.State != StateRunning {
		return fmt.Errorf("%w: %s (state=%s, want running)", ErrInvalidState, commandID, entry.State)
	}
	entry.State = StateCompleted

	if err := os.MkdirAll(s.completedDir(), 0o700); err != nil {
		return fmt.Errorf("schedule completed mkdir: %w", err)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("schedule marshal completed: %w", err)
	}
	dst := s.completedPath(entry)
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("schedule write completed: %w", err)
	}
	return os.Remove(path)
}

// MarkFailed transitions running → failed; increments retry_count and stores error.
func (s *FileStore) MarkFailed(_ context.Context, commandID, lastError string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateEntry(commandID, func(e *Entry) error {
		if e.State != StateRunning {
			return fmt.Errorf("%w: %s (state=%s, want running)", ErrInvalidState, commandID, e.State)
		}
		e.State = StateFailed
		e.LastError = lastError
		e.RetryCount++
		t := now.UTC()
		e.LastRunAt = &t
		return nil
	})
}

// MarkRetrying transitions failed → pending so a failed entry can be re-run.
func (s *FileStore) MarkRetrying(_ context.Context, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateEntry(commandID, func(e *Entry) error {
		if e.State != StateFailed {
			return fmt.Errorf("%w: %s (state=%s, want failed)", ErrInvalidState, commandID, e.State)
		}
		e.State = StatePending
		e.LastError = ""
		return nil
	})
}

// MarkCancelled transitions pending → cancelled and rewrites the file.
func (s *FileStore) MarkCancelled(_ context.Context, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateEntry(commandID, func(e *Entry) error {
		if e.State != StatePending {
			return fmt.Errorf("%w: %s (state=%s, want pending)", ErrInvalidState, commandID, e.State)
		}
		e.State = StateCancelled
		return nil
	})
}

// Next returns the earliest pending entry.
func (s *FileStore) Next(_ context.Context) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	des, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, fmt.Errorf("%w: no entries", ErrNotFound)
		}
		return Entry{}, fmt.Errorf("schedule next readdir: %w", err)
	}

	var earliest *Entry
	for _, de := range des {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, de.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.State != StatePending {
			continue
		}
		if earliest == nil || e.ScheduledAt.Before(earliest.ScheduledAt) {
			cp := e
			earliest = &cp
		}
	}

	if earliest == nil {
		return Entry{}, fmt.Errorf("%w: no pending entries", ErrNotFound)
	}
	return *earliest, nil
}

// findInMain locates a file by commandID in the main schedule directory.
// Must be called with mu held.
func (s *FileStore) findInMain(commandID string) (Entry, string, error) {
	des, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, "", fmt.Errorf("%w: %s", ErrNotFound, commandID)
		}
		return Entry{}, "", fmt.Errorf("schedule readdir: %w", err)
	}
	for _, de := range des {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		if !strings.Contains(de.Name(), commandID) {
			continue
		}
		path := filepath.Join(s.dir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.CommandID == commandID {
			return e, path, nil
		}
	}
	return Entry{}, "", fmt.Errorf("%w: %s", ErrNotFound, commandID)
}

// updateEntry loads, mutates, and rewrites an entry in the main directory.
// Must be called with mu held.
func (s *FileStore) updateEntry(commandID string, mutate func(*Entry) error) error {
	entry, path, err := s.findInMain(commandID)
	if err != nil {
		return err
	}
	if err := mutate(&entry); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("schedule marshal update: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("schedule write update: %w", err)
	}
	return nil
}

// MemoryStore is an in-memory Store for tests.
type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]Entry
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]Entry)}
}

// Add stores the entry in memory.
func (m *MemoryStore) Add(_ context.Context, entry Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.State = StatePending
	m.entries[entry.CommandID] = entry
	return nil
}

// List returns all entries sorted by scheduled_at ascending.
func (m *MemoryStore) List(_ context.Context) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := make([]Entry, 0, len(m.entries))
	for k := range m.entries {
		e := m.entries[k]
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ScheduledAt.Before(entries[j].ScheduledAt)
	})
	return entries, nil
}

// Get returns the entry for commandID.
func (m *MemoryStore) Get(_ context.Context, commandID string) (Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[commandID]
	if !ok {
		return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	return e, nil
}

// Due returns pending entries with scheduled_at <= now, up to limit.
func (m *MemoryStore) Due(_ context.Context, now time.Time, limit int) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var due []Entry
	for k := range m.entries {
		e := m.entries[k]
		if e.State == StatePending && !e.ScheduledAt.After(now) {
			due = append(due, e)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		return due[i].ScheduledAt.Before(due[j].ScheduledAt)
	})
	if limit > 0 && len(due) > limit {
		due = due[:limit]
	}
	return due, nil
}

// MarkRunning transitions pending → running.
func (m *MemoryStore) MarkRunning(_ context.Context, commandID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[commandID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	if e.State != StatePending {
		return fmt.Errorf("%w: %s (state=%s, want pending)", ErrInvalidState, commandID, e.State)
	}
	e.State = StateRunning
	m.entries[commandID] = e
	return nil
}

// MarkCompleted transitions running → completed.
func (m *MemoryStore) MarkCompleted(_ context.Context, commandID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[commandID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	if e.State != StateRunning {
		return fmt.Errorf("%w: %s (state=%s, want running)", ErrInvalidState, commandID, e.State)
	}
	e.State = StateCompleted
	m.entries[commandID] = e
	return nil
}

// MarkFailed transitions running → failed; increments retry_count and stores error.
func (m *MemoryStore) MarkFailed(_ context.Context, commandID, lastError string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[commandID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	if e.State != StateRunning {
		return fmt.Errorf("%w: %s (state=%s, want running)", ErrInvalidState, commandID, e.State)
	}
	e.State = StateFailed
	e.LastError = lastError
	e.RetryCount++
	t := now.UTC()
	e.LastRunAt = &t
	m.entries[commandID] = e
	return nil
}

// MarkRetrying transitions failed → pending so a failed entry can be re-run.
func (m *MemoryStore) MarkRetrying(_ context.Context, commandID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[commandID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	if e.State != StateFailed {
		return fmt.Errorf("%w: %s (state=%s, want failed)", ErrInvalidState, commandID, e.State)
	}
	e.State = StatePending
	e.LastError = ""
	m.entries[commandID] = e
	return nil
}

// MarkCancelled transitions pending → cancelled.
func (m *MemoryStore) MarkCancelled(_ context.Context, commandID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[commandID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	if e.State != StatePending {
		return fmt.Errorf("%w: %s (state=%s, want pending)", ErrInvalidState, commandID, e.State)
	}
	e.State = StateCancelled
	m.entries[commandID] = e
	return nil
}

// Next returns the earliest pending entry.
func (m *MemoryStore) Next(_ context.Context) (Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var earliest *Entry
	for k := range m.entries {
		e := m.entries[k]
		if e.State != StatePending {
			continue
		}
		if earliest == nil || e.ScheduledAt.Before(earliest.ScheduledAt) {
			cp := e
			earliest = &cp
		}
	}
	if earliest == nil {
		return Entry{}, fmt.Errorf("%w: no pending entries", ErrNotFound)
	}
	return *earliest, nil
}

// ResolvePath returns the effective schedule directory.
// Precedence: GOLINK_SCHEDULE_DIR > XDG_STATE_HOME/golink/schedule/ >
// $HOME/.local/state/golink/schedule/.
func ResolvePath() string {
	if v := os.Getenv("GOLINK_SCHEDULE_DIR"); v != "" {
		return v
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "golink", "schedule")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "state", "golink", "schedule")
	}
	return filepath.Join(home, ".local", "state", "golink", "schedule")
}
