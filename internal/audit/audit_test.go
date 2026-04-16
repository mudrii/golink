package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileSinkWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "audit.jsonl")

	sink := NewFileSink(path)

	entries := []Entry{
		{
			TS:        time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
			Profile:   "default",
			Transport: "official",
			Command:   "post create",
			CommandID: "cmd_post_create_1234",
			Mode:      "normal",
			Status:    "ok",
		},
		{
			TS:        time.Date(2026, 4, 17, 12, 1, 0, 0, time.UTC),
			Profile:   "default",
			Transport: "official",
			Command:   "post delete",
			CommandID: "cmd_post_delete_5678",
			Mode:      "dry_run",
			Status:    "ok",
		},
	}

	ctx := t.Context()
	for _, e := range entries {
		if err := sink.Append(ctx, e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("close audit file: %v", closeErr)
		}
	}()

	var got []Entry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		got = append(got, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(got) != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), len(got))
	}
	for i, want := range entries {
		if got[i].Command != want.Command {
			t.Errorf("[%d] command: want %q, got %q", i, want.Command, got[i].Command)
		}
		if got[i].Status != want.Status {
			t.Errorf("[%d] status: want %q, got %q", i, want.Status, got[i].Status)
		}
		if got[i].Mode != want.Mode {
			t.Errorf("[%d] mode: want %q, got %q", i, want.Mode, got[i].Mode)
		}
		if got[i].CommandID != want.CommandID {
			t.Errorf("[%d] command_id: want %q, got %q", i, want.CommandID, got[i].CommandID)
		}
	}
}

func TestFileSinkPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "golink", "audit.jsonl")

	sink := NewFileSink(path)
	if err := sink.Append(t.Context(), Entry{
		TS:      time.Now().UTC(),
		Command: "post create",
		Status:  "ok",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("file mode: want 0o600, got %04o", fileInfo.Mode().Perm())
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("dir mode: want 0o700, got %04o", dirInfo.Mode().Perm())
	}
}

func TestMemorySinkConcurrent(t *testing.T) {
	sink := NewMemorySink()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			_ = sink.Append(context.Background(), Entry{
				CommandID: string(rune('a' + i%26)),
				Status:    "ok",
			})
		}(i)
	}
	wg.Wait()

	if len(sink.Entries()) != n {
		t.Fatalf("expected %d entries, got %d", n, len(sink.Entries()))
	}
}

func TestResolvePathFromEnv(t *testing.T) {
	t.Setenv("GOLINK_AUDIT_PATH", "/tmp/custom-audit.jsonl")
	t.Setenv("XDG_STATE_HOME", "")

	got := ResolvePath()
	if got != "/tmp/custom-audit.jsonl" {
		t.Errorf("expected /tmp/custom-audit.jsonl, got %q", got)
	}
}

func TestResolvePathFromXDG(t *testing.T) {
	t.Setenv("GOLINK_AUDIT_PATH", "")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")

	got := ResolvePath()
	want := "/xdg/state/golink/audit.jsonl"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestResolvePathFallback(t *testing.T) {
	t.Setenv("GOLINK_AUDIT_PATH", "")
	t.Setenv("XDG_STATE_HOME", "")

	got := ResolvePath()
	if got == "" {
		t.Fatal("expected non-empty path")
	}
	// Should end with the canonical suffix.
	const suffix = ".local/state/golink/audit.jsonl"
	if len(got) < len(suffix) || got[len(got)-len(suffix):] != suffix {
		t.Errorf("expected path ending in %q, got %q", suffix, got)
	}
}
