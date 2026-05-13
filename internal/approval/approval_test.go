package approval_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/output"
)

func testEntry(id, cmd string) approval.Entry {
	return approval.Entry{
		CommandID: id,
		Command:   cmd,
		CreatedAt: time.Now().UTC(),
		Transport: "official",
		Profile:   "default",
		Payload:   map[string]any{"endpoint": "POST /rest/posts", "text": "hello"},
	}
}

func TestMemoryStore_StageAndList(t *testing.T) {
	ctx := t.Context()
	store := approval.NewMemoryStore()

	e := testEntry("cmd_post_create_001", "post create")
	path, err := store.Stage(ctx, e)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if path == "" {
		t.Fatal("stage returned empty path")
	}

	items, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].State != approval.StatePending {
		t.Errorf("expected pending, got %s", items[0].State)
	}
	if items[0].CommandID != e.CommandID {
		t.Errorf("command_id mismatch: %s", items[0].CommandID)
	}
}

func TestMemoryStore_GrantAndRun(t *testing.T) {
	ctx := t.Context()
	store := approval.NewMemoryStore()

	e := testEntry("cmd_post_create_002", "post create")
	if _, err := store.Stage(ctx, e); err != nil {
		t.Fatal(err)
	}

	if err := store.Grant(ctx, e.CommandID); err != nil {
		t.Fatalf("grant: %v", err)
	}

	loaded, err := store.LoadApproved(ctx, e.CommandID)
	if err != nil {
		t.Fatalf("load approved: %v", err)
	}
	if loaded.CommandID != e.CommandID {
		t.Errorf("command_id mismatch")
	}

	if err := store.Complete(ctx, e.CommandID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	items, _ := store.List(ctx)
	if items[0].State != approval.StateCompleted {
		t.Errorf("expected completed, got %s", items[0].State)
	}
}

func TestMemoryStore_Deny(t *testing.T) {
	ctx := t.Context()
	store := approval.NewMemoryStore()

	e := testEntry("cmd_post_create_003", "post create")
	if _, err := store.Stage(ctx, e); err != nil {
		t.Fatal(err)
	}

	if err := store.Deny(ctx, e.CommandID); err != nil {
		t.Fatalf("deny: %v", err)
	}

	_, err := store.LoadApproved(ctx, e.CommandID)
	if !errors.Is(err, approval.ErrWrongState) {
		t.Errorf("expected ErrWrongState, got %v", err)
	}
}

func TestMemoryStore_RunWithoutGrant(t *testing.T) {
	ctx := t.Context()
	store := approval.NewMemoryStore()

	e := testEntry("cmd_post_create_004", "post create")
	if _, err := store.Stage(ctx, e); err != nil {
		t.Fatal(err)
	}

	_, err := store.LoadApproved(ctx, e.CommandID)
	if !errors.Is(err, approval.ErrWrongState) {
		t.Errorf("expected ErrWrongState for pending entry, got %v", err)
	}
}

func TestMemoryStore_Cancel(t *testing.T) {
	ctx := t.Context()
	store := approval.NewMemoryStore()

	e := testEntry("cmd_post_create_005", "post create")
	if _, err := store.Stage(ctx, e); err != nil {
		t.Fatal(err)
	}

	if err := store.Cancel(ctx, e.CommandID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	items, _ := store.List(ctx)
	if len(items) != 0 {
		t.Errorf("expected empty list after cancel, got %d", len(items))
	}
}

func TestMemoryStore_StagedPathAndWrongStateTransitions(t *testing.T) {
	ctx := t.Context()
	store := approval.NewMemoryStore()

	e := testEntry("cmd_post_create_006", "post create")
	path, err := store.Stage(ctx, e)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if got := store.StagedPath(e.CommandID); got != path {
		t.Fatalf("staged path = %q, want %q", got, path)
	}
	if _, err := store.Stage(ctx, e); !errors.Is(err, approval.ErrAlreadyStaged) {
		t.Fatalf("duplicate stage error = %v, want ErrAlreadyStaged", err)
	}
	if err := store.Complete(ctx, e.CommandID); !errors.Is(err, approval.ErrWrongState) {
		t.Fatalf("complete pending error = %v, want ErrWrongState", err)
	}
	if err := store.Deny(ctx, e.CommandID); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := store.Cancel(ctx, e.CommandID); !errors.Is(err, approval.ErrWrongState) {
		t.Fatalf("cancel denied error = %v, want ErrWrongState", err)
	}
}

func TestMemoryStore_NotFound(t *testing.T) {
	ctx := t.Context()
	store := approval.NewMemoryStore()

	_, _, err := store.Show(ctx, "nonexistent")
	if !errors.Is(err, approval.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFileStore_StageListGrantRun(t *testing.T) {
	dir := t.TempDir()
	store := approval.NewFileStore(dir)
	ctx := t.Context()

	e := testEntry("cmd_post_create_file_001", "post create")
	path, err := store.Stage(ctx, e)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if path == "" {
		t.Fatal("empty path")
	}

	items, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].State != approval.StatePending {
		t.Fatalf("unexpected list: %+v", items)
	}

	if err := store.Grant(ctx, e.CommandID); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadApproved(ctx, e.CommandID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CommandID != e.CommandID {
		t.Error("command_id mismatch")
	}

	if err := store.Complete(ctx, e.CommandID); err != nil {
		t.Fatal(err)
	}

	items2, _ := store.List(ctx)
	if items2[0].State != approval.StateCompleted {
		t.Errorf("expected completed, got %s", items2[0].State)
	}
}

func TestFileStore_StageRedactsFreeText(t *testing.T) {
	dir := t.TempDir()
	store := approval.NewFileStore(dir)

	e := approval.Entry{
		CommandID: "cmd_post_create_redact",
		Command:   "post create",
		CreatedAt: time.Now().UTC(),
		Transport: "official",
		Profile:   "default",
		Payload: output.PostPayloadPreview{
			Endpoint:   "POST /rest/posts",
			Text:       "confidential",
			Visibility: output.Visibility("PUBLIC"),
			AuthorURN:  "urn:li:person:abc123",
		},
	}
	path, err := store.Stage(t.Context(), e)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(raw)

	for _, leaked := range []string{"confidential", "urn:li:person:abc123"} {
		if strings.Contains(text, leaked) {
			t.Errorf("approval file leaked %q: %s", leaked, text)
		}
	}
	if !strings.Contains(text, "PUBLIC") {
		t.Errorf("expected non-sensitive visibility to survive: %s", text)
	}
}

func TestFileStore_DuplicateStageAndCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	store := approval.NewFileStore(dir)
	ctx := t.Context()

	e := testEntry("cmd_file_duplicate_001", "post create")
	if _, err := store.Stage(ctx, e); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := store.Stage(ctx, e); !errors.Is(err, approval.ErrAlreadyStaged) {
		t.Fatalf("duplicate stage error = %v, want ErrAlreadyStaged", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "cmd_bad.pending.json"), []byte(`{`), 0o600); err != nil {
		t.Fatalf("write corrupt approval: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd_ignored.unknown.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write ignored approval: %v", err)
	}

	items, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("list should skip corrupt and unknown files; got %+v", items)
	}

	_, _, err = store.Show(ctx, "cmd_bad")
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("show corrupt error = %v, want unmarshal error", err)
	}
}

func TestFileStore_StageReturnsDirectoryCreationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	store := approval.NewFileStore(filepath.Join(path, "nested"))

	if _, err := store.Stage(t.Context(), testEntry("cmd_mkdir_fails", "post create")); err == nil || !strings.Contains(err.Error(), "approval mkdir") {
		t.Fatalf("stage error = %v, want approval mkdir", err)
	}
}

func TestFileStore_WrongStateAndInvalidCommandID(t *testing.T) {
	dir := t.TempDir()
	store := approval.NewFileStore(dir)
	ctx := t.Context()

	if _, err := store.Stage(ctx, testEntry("bad id with spaces", "post create")); err == nil {
		t.Fatal("expected invalid command id error")
	}

	e := testEntry("cmd_wrong_state_001", "post create")
	if _, err := store.Stage(ctx, e); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := store.Complete(ctx, e.CommandID); !errors.Is(err, approval.ErrNotFound) {
		t.Fatalf("complete pending error = %v, want ErrNotFound", err)
	}
	if err := store.Deny(ctx, e.CommandID); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := store.Cancel(ctx, e.CommandID); !errors.Is(err, approval.ErrNotFound) {
		t.Fatalf("cancel denied error = %v, want ErrNotFound", err)
	}
	if _, err := store.LoadApproved(ctx, e.CommandID); !errors.Is(err, approval.ErrWrongState) {
		t.Fatalf("load denied error = %v, want ErrWrongState", err)
	}
}

func TestResolvePath(t *testing.T) {
	t.Setenv("GOLINK_APPROVAL_DIR", "/tmp/test-approvals")
	if got := approval.ResolvePath(); got != "/tmp/test-approvals" {
		t.Fatalf("env override path = %q", got)
	}

	t.Setenv("GOLINK_APPROVAL_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/tmp/test-state")
	want := "/tmp/test-state/golink/approvals"
	if got := approval.ResolvePath(); got != want {
		t.Fatalf("xdg path = %q, want %q", got, want)
	}

	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	want = filepath.Join(".local", "state", "golink", "approvals")
	if got := approval.ResolvePath(); got != want {
		t.Fatalf("fallback path = %q, want %q", got, want)
	}
}

func TestFileStore_DenyAndCancel(t *testing.T) {
	dir := t.TempDir()
	store := approval.NewFileStore(dir)
	ctx := t.Context()

	e1 := testEntry("cmd_deny_001", "post create")
	if _, err := store.Stage(ctx, e1); err != nil {
		t.Fatal(err)
	}
	if err := store.Deny(ctx, e1.CommandID); err != nil {
		t.Fatal(err)
	}
	_, err := store.LoadApproved(ctx, e1.CommandID)
	if !errors.Is(err, approval.ErrWrongState) {
		t.Errorf("expected ErrWrongState after deny, got %v", err)
	}

	e2 := testEntry("cmd_cancel_001", "post create")
	if _, err := store.Stage(ctx, e2); err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel(ctx, e2.CommandID); err != nil {
		t.Fatal(err)
	}

	_, _, err = store.Show(ctx, e2.CommandID)
	if !errors.Is(err, approval.ErrNotFound) {
		t.Errorf("expected ErrNotFound after cancel, got %v", err)
	}
}
