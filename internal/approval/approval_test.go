package approval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/approval"
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	ctx := context.Background()
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

func TestMemoryStore_NotFound(t *testing.T) {
	ctx := context.Background()
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
