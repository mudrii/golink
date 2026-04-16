package cmd

import (
	"encoding/json"
	"testing"

	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/audit"
	outputtest "github.com/mudrii/golink/internal/output"
)

// schemaPath is defined in batch_test.go; reused here.

func TestPostCreateRequireApproval_ExitCode3(t *testing.T) {
	sink := audit.NewMemorySink()
	t.Setenv("GOLINK_AUDIT", "on")
	astore := approval.NewMemoryStore()

	code, stdout, _ := executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "approval test", "--require-approval"},
		testDepsOptions{
			auditSink:     sink,
			approvalStore: astore,
		})

	if code != 3 {
		t.Fatalf("expected exit code 3, got %d stdout=%s", code, stdout)
	}

	// Envelope written to stdout with status pending_approval.
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if payload["status"] != "pending_approval" {
		t.Errorf("expected status pending_approval, got %v", payload["status"])
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		t.Fatal("missing data field")
	}
	if data["command"] != "post create" {
		t.Errorf("expected command post create, got %v", data["command"])
	}
	if data["staged_path"] == "" {
		t.Error("staged_path is empty")
	}

	// Staged entry appears in the approval store.
	items, err := astore.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 staged entry, got %d", len(items))
	}
	if items[0].State != approval.StatePending {
		t.Errorf("expected pending, got %s", items[0].State)
	}

	// Audit entry with status pending_approval.
	entries := sink.Entries()
	if len(entries) == 0 {
		t.Fatal("no audit entries")
	}
	found := false
	for _, e := range entries {
		if e.Status == "pending_approval" {
			found = true
		}
	}
	if !found {
		t.Errorf("no audit entry with status pending_approval; entries: %+v", entries)
	}
}

func TestApprovalList(t *testing.T) {
	astore := approval.NewMemoryStore()

	// Stage a post create via --require-approval.
	executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "test post", "--require-approval"},
		testDepsOptions{approvalStore: astore})

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "list"},
		testDepsOptions{approvalStore: astore})

	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr)
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Data.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(payload.Data.Items))
	}
	if payload.Data.Items[0]["state"] != "pending" {
		t.Errorf("expected pending, got %v", payload.Data.Items[0]["state"])
	}
}

func TestApprovalGrant(t *testing.T) {
	astore := approval.NewMemoryStore()

	executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "test post", "--require-approval"},
		testDepsOptions{approvalStore: astore})

	items, _ := astore.List(t.Context())
	if len(items) == 0 {
		t.Fatal("no staged items")
	}
	cmdID := items[0].CommandID

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "grant", cmdID},
		testDepsOptions{approvalStore: astore})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["status"] != "ok" {
		t.Errorf("expected ok, got %v", payload["status"])
	}

	// Verify state transitioned to approved.
	items2, _ := astore.List(t.Context())
	if items2[0].State != approval.StateApproved {
		t.Errorf("expected approved, got %s", items2[0].State)
	}
}

func TestApprovalRun_ExecutesViaTransport(t *testing.T) {
	astore := approval.NewMemoryStore()

	// Stage.
	executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "Hello approval", "--require-approval"},
		testDepsOptions{approvalStore: astore})

	items, _ := astore.List(t.Context())
	cmdID := items[0].CommandID

	// Grant.
	executeTestCommand(t,
		[]string{"--json", "approval", "grant", cmdID},
		testDepsOptions{approvalStore: astore})

	// Run via fake transport.
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", cmdID},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
			approvalStore:    astore,
		})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["status"] != "ok" {
		t.Errorf("expected ok, got %v", payload["status"])
	}

	// Entry should now be completed.
	items2, _ := astore.List(t.Context())
	if items2[0].State != approval.StateCompleted {
		t.Errorf("expected completed, got %s", items2[0].State)
	}
}

func TestApprovalRun_WithoutGrant_Fails(t *testing.T) {
	astore := approval.NewMemoryStore()

	executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "test post", "--require-approval"},
		testDepsOptions{approvalStore: astore})

	items, _ := astore.List(t.Context())
	cmdID := items[0].CommandID

	// Run without granting — should fail with exit 2.
	code, _, _ := executeTestCommand(t,
		[]string{"--json", "approval", "run", cmdID},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
			approvalStore:    astore,
		})
	if code != 2 {
		t.Fatalf("expected exit 2 (wrong state), got %d", code)
	}
}

func TestApprovalDeny(t *testing.T) {
	astore := approval.NewMemoryStore()

	executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "test post", "--require-approval"},
		testDepsOptions{approvalStore: astore})

	items, _ := astore.List(t.Context())
	cmdID := items[0].CommandID

	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "deny", cmdID},
		testDepsOptions{approvalStore: astore})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr)
	}

	items2, _ := astore.List(t.Context())
	if items2[0].State != approval.StateDenied {
		t.Errorf("expected denied, got %s", items2[0].State)
	}
}

func TestApprovalCancel(t *testing.T) {
	astore := approval.NewMemoryStore()

	executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", "test post", "--require-approval"},
		testDepsOptions{approvalStore: astore})

	items, _ := astore.List(t.Context())
	cmdID := items[0].CommandID

	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "cancel", cmdID},
		testDepsOptions{approvalStore: astore})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr)
	}

	items2, _ := astore.List(t.Context())
	if len(items2) != 0 {
		t.Errorf("expected empty list after cancel, got %d", len(items2))
	}
}
