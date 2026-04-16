package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is a single audit log record written for every mutating command.
type Entry struct {
	TS            time.Time       `json:"ts"`
	Profile       string          `json:"profile"`
	Transport     string          `json:"transport"`
	Command       string          `json:"command"`
	CommandID     string          `json:"command_id"`
	Mode          string          `json:"mode,omitempty"`
	Status        string          `json:"status"`
	RequestID     string          `json:"request_id,omitempty"`
	HTTPStatus    int             `json:"http_status,omitempty"`
	ErrorCode     string          `json:"error_code,omitempty"`
	DryRunPreview json.RawMessage `json:"dry_run_preview,omitempty"`
}

// Sink appends an Entry to the audit destination.
type Sink interface {
	Append(ctx context.Context, entry Entry) error
}

// FileSink writes JSONL entries to a file, creating it on first use.
type FileSink struct {
	path string
}

// NewFileSink returns a FileSink that writes to path.
// If path is empty, ResolvePath() is used.
func NewFileSink(path string) *FileSink {
	if path == "" {
		path = ResolvePath()
	}
	return &FileSink{path: path}
}

// Append marshals entry as a JSON line and appends it to the file.
// The directory is created (mode 0o700) and the file opened (mode 0o600) on
// every call — CLI lifetime is short and this avoids fd leaks.
func (s *FileSink) Append(_ context.Context, entry Entry) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("audit mkdir: %w", err)
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit open: %w", err)
	}

	line, err := json.Marshal(entry)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("audit marshal: %w", err)
	}
	line = append(line, '\n')

	_, writeErr := f.Write(line)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("audit write: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("audit close: %w", closeErr)
	}
	return nil
}

// MemorySink stores entries in memory; safe for concurrent use. Used in tests.
type MemorySink struct {
	mu      sync.Mutex
	entries []Entry
}

// NewMemorySink returns a MemorySink with an empty entry list.
func NewMemorySink() *MemorySink {
	return &MemorySink{}
}

// Append stores the entry in memory.
func (m *MemorySink) Append(_ context.Context, entry Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

// Entries returns a copy of all stored entries.
func (m *MemorySink) Entries() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Entry, len(m.entries))
	copy(out, m.entries)
	return out
}

// NoopSink discards every entry. Used when audit is disabled.
type NoopSink struct{}

// Append does nothing.
func (NoopSink) Append(_ context.Context, _ Entry) error { return nil }

// ResolvePath returns the effective audit log path.
// Precedence: GOLINK_AUDIT_PATH env > XDG_STATE_HOME/golink/audit.jsonl >
// $HOME/.local/state/golink/audit.jsonl.
func ResolvePath() string {
	if v := os.Getenv("GOLINK_AUDIT_PATH"); v != "" {
		return v
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "golink", "audit.jsonl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "state", "golink", "audit.jsonl")
	}
	return filepath.Join(home, ".local", "state", "golink", "audit.jsonl")
}
