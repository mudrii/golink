package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// State represents the lifecycle state of an approval entry.
type State string

const (
	// StatePending means the entry is staged and awaiting operator decision.
	StatePending State = "pending"
	// StateApproved means the operator has approved the entry for execution.
	StateApproved State = "approved"
	// StateDenied means the operator has rejected the entry.
	StateDenied State = "denied"
	// StateCompleted means the entry was executed successfully.
	StateCompleted State = "completed"
)

// ErrNotFound is returned when a command_id does not exist in the store.
var ErrNotFound = errors.New("approval entry not found")

// ErrWrongState is returned when an operation cannot proceed because the entry
// is in an incompatible state (e.g. Run on a pending entry).
var ErrWrongState = errors.New("approval entry is in wrong state")

// Entry is the on-disk representation of a staged approval request.
type Entry struct {
	CommandID      string    `json:"command_id"`
	Command        string    `json:"command"`
	CreatedAt      time.Time `json:"created_at"`
	Transport      string    `json:"transport"`
	Profile        string    `json:"profile"`
	Payload        any       `json:"payload"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
}

// ListItem summarises one approval entry for the list subcommand.
type ListItem struct {
	CommandID      string    `json:"command_id"`
	Command        string    `json:"command"`
	State          State     `json:"state"`
	StagedAt       time.Time `json:"staged_at"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
}

// Store is the approval record backend.
type Store interface {
	// Stage writes a new .pending.json file. Returns the file path on success.
	Stage(ctx context.Context, entry Entry) (string, error)

	// List returns summary rows for all entries in the directory.
	List(ctx context.Context) ([]ListItem, error)

	// Show returns the full entry and its current state.
	Show(ctx context.Context, commandID string) (Entry, State, error)

	// Grant renames .pending.json → .approved.json.
	Grant(ctx context.Context, commandID string) error

	// Deny renames .pending.json → .denied.json.
	Deny(ctx context.Context, commandID string) error

	// LoadApproved reads an approved entry without renaming it. The caller
	// is responsible for calling Complete after successful execution.
	LoadApproved(ctx context.Context, commandID string) (Entry, error)

	// Complete renames .approved.json → .completed.json.
	Complete(ctx context.Context, commandID string) error

	// Cancel removes a pending or approved entry without executing it.
	Cancel(ctx context.Context, commandID string) error
}

// FileStore implements Store using the approvals directory.
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
	return os.MkdirAll(s.dir, 0o700)
}

func (s *FileStore) filePath(commandID string, state State) string {
	return filepath.Join(s.dir, commandID+"."+string(state)+".json")
}

// Stage writes a new .pending.json file.
func (s *FileStore) Stage(_ context.Context, entry Entry) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDir(); err != nil {
		return "", fmt.Errorf("approval mkdir: %w", err)
	}

	path := s.filePath(entry.CommandID, StatePending)
	data, err := json.Marshal(entry)
	if err != nil {
		return "", fmt.Errorf("approval marshal: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("approval write: %w", err)
	}
	return path, nil
}

// List reads the directory and returns summary items for all entries.
func (s *FileStore) List(_ context.Context) ([]ListItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("approval list: %w", err)
	}

	var items []ListItem
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		commandID, state, ok := parseFilename(name)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		items = append(items, ListItem{
			CommandID:      commandID,
			Command:        e.Command,
			State:          state,
			StagedAt:       e.CreatedAt,
			IdempotencyKey: e.IdempotencyKey,
		})
	}
	return items, nil
}

// Show returns the entry and its current state.
func (s *FileStore) Show(_ context.Context, commandID string) (Entry, State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, state := range []State{StatePending, StateApproved, StateDenied, StateCompleted} {
		path := s.filePath(commandID, state)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Entry{}, "", fmt.Errorf("approval show read: %w", err)
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			return Entry{}, "", fmt.Errorf("approval show unmarshal: %w", err)
		}
		return e, state, nil
	}
	return Entry{}, "", fmt.Errorf("%w: %s", ErrNotFound, commandID)
}

// Grant renames .pending.json → .approved.json.
func (s *FileStore) Grant(_ context.Context, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	src := s.filePath(commandID, StatePending)
	dst := s.filePath(commandID, StateApproved)
	if err := os.Rename(src, dst); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s (not pending)", ErrNotFound, commandID)
		}
		return fmt.Errorf("approval grant: %w", err)
	}
	return nil
}

// Deny renames .pending.json → .denied.json.
func (s *FileStore) Deny(_ context.Context, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	src := s.filePath(commandID, StatePending)
	dst := s.filePath(commandID, StateDenied)
	if err := os.Rename(src, dst); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s (not pending)", ErrNotFound, commandID)
		}
		return fmt.Errorf("approval deny: %w", err)
	}
	return nil
}

// LoadApproved reads an approved entry without renaming it.
func (s *FileStore) LoadApproved(_ context.Context, commandID string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if pending first to give a clear error message.
	if _, err := os.Stat(s.filePath(commandID, StatePending)); err == nil {
		return Entry{}, fmt.Errorf("%w: %s is pending, not approved", ErrWrongState, commandID)
	}
	if _, err := os.Stat(s.filePath(commandID, StateDenied)); err == nil {
		return Entry{}, fmt.Errorf("%w: %s was denied", ErrWrongState, commandID)
	}
	if _, err := os.Stat(s.filePath(commandID, StateCompleted)); err == nil {
		return Entry{}, fmt.Errorf("%w: %s was already completed", ErrWrongState, commandID)
	}

	path := s.filePath(commandID, StateApproved)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, commandID)
		}
		return Entry{}, fmt.Errorf("approval load: %w", err)
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, fmt.Errorf("approval load unmarshal: %w", err)
	}
	return e, nil
}

// Complete renames .approved.json → .completed.json.
func (s *FileStore) Complete(_ context.Context, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	src := s.filePath(commandID, StateApproved)
	dst := s.filePath(commandID, StateCompleted)
	if err := os.Rename(src, dst); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s (not approved)", ErrNotFound, commandID)
		}
		return fmt.Errorf("approval complete: %w", err)
	}
	return nil
}

// Cancel removes a pending or approved entry.
func (s *FileStore) Cancel(_ context.Context, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, state := range []State{StatePending, StateApproved} {
		path := s.filePath(commandID, state)
		err := os.Remove(path)
		if err == nil {
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("approval cancel: %w", err)
		}
	}
	return fmt.Errorf("%w: %s (not pending or approved)", ErrNotFound, commandID)
}

// MemoryStore is an in-memory Store for tests.
type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]Entry
	states  map[string]State
	paths   map[string]string
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[string]Entry),
		states:  make(map[string]State),
		paths:   make(map[string]string),
	}
}

// Stage stores the entry in memory and returns a synthetic path.
func (m *MemoryStore) Stage(_ context.Context, entry Entry) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[entry.CommandID] = entry
	m.states[entry.CommandID] = StatePending
	path := "/tmp/approvals/" + entry.CommandID + ".pending.json"
	m.paths[entry.CommandID] = path
	return path, nil
}

// List returns all entries currently held in memory.
func (m *MemoryStore) List(_ context.Context) ([]ListItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]ListItem, 0, len(m.entries))
	for id, e := range m.entries {
		items = append(items, ListItem{
			CommandID:      id,
			Command:        e.Command,
			State:          m.states[id],
			StagedAt:       e.CreatedAt,
			IdempotencyKey: e.IdempotencyKey,
		})
	}
	return items, nil
}

// Show returns the entry and its current state.
func (m *MemoryStore) Show(_ context.Context, commandID string) (Entry, State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[commandID]
	if !ok {
		return Entry{}, "", fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	return e, m.states[commandID], nil
}

// Grant transitions a pending entry to approved.
func (m *MemoryStore) Grant(_ context.Context, commandID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[commandID]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	if m.states[commandID] != StatePending {
		return fmt.Errorf("%w: %s (not pending)", ErrWrongState, commandID)
	}
	m.states[commandID] = StateApproved
	return nil
}

// Deny transitions a pending entry to denied.
func (m *MemoryStore) Deny(_ context.Context, commandID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[commandID]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	if m.states[commandID] != StatePending {
		return fmt.Errorf("%w: %s (not pending)", ErrWrongState, commandID)
	}
	m.states[commandID] = StateDenied
	return nil
}

// LoadApproved returns an approved entry or ErrWrongState if not in approved state.
func (m *MemoryStore) LoadApproved(_ context.Context, commandID string) (Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[commandID]
	if !ok {
		return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	st := m.states[commandID]
	switch st {
	case StatePending:
		return Entry{}, fmt.Errorf("%w: %s is pending, not approved", ErrWrongState, commandID)
	case StateDenied:
		return Entry{}, fmt.Errorf("%w: %s was denied", ErrWrongState, commandID)
	case StateCompleted:
		return Entry{}, fmt.Errorf("%w: %s was already completed", ErrWrongState, commandID)
	case StateApproved:
		return e, nil
	default:
		return Entry{}, fmt.Errorf("%w: %s", ErrWrongState, commandID)
	}
}

// Complete marks an entry as completed after successful execution.
func (m *MemoryStore) Complete(_ context.Context, commandID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[commandID]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	m.states[commandID] = StateCompleted
	return nil
}

// Cancel removes a pending or approved entry from the store.
func (m *MemoryStore) Cancel(_ context.Context, commandID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.states[commandID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, commandID)
	}
	if st != StatePending && st != StateApproved {
		return fmt.Errorf("%w: %s (not pending or approved)", ErrWrongState, commandID)
	}
	delete(m.entries, commandID)
	delete(m.states, commandID)
	delete(m.paths, commandID)
	return nil
}

// StagedPath returns the path recorded for commandID; used in tests.
func (m *MemoryStore) StagedPath(commandID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paths[commandID]
}

// parseFilename splits "<commandID>.<state>.json" into parts.
func parseFilename(name string) (commandID string, state State, ok bool) {
	name = strings.TrimSuffix(name, ".json")
	for _, s := range []State{StatePending, StateApproved, StateDenied, StateCompleted} {
		suffix := "." + string(s)
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix), s, true
		}
	}
	return "", "", false
}

// ResolvePath returns the effective approvals directory.
// Precedence: GOLINK_APPROVAL_DIR > XDG_STATE_HOME/golink/approvals/ >
// $HOME/.local/state/golink/approvals/.
func ResolvePath() string {
	if v := os.Getenv("GOLINK_APPROVAL_DIR"); v != "" {
		return v
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "golink", "approvals")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "state", "golink", "approvals")
	}
	return filepath.Join(home, ".local", "state", "golink", "approvals")
}
