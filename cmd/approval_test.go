package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	outputtest "github.com/mudrii/golink/internal/output"
)

// schemaPath is defined in batch_test.go; reused here.

func authenticatedStoreForProfile(t *testing.T, profile, transport, memberURN string, scopes ...string) auth.Store {
	t.Helper()

	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:     profile,
		Transport:   transport,
		AccessToken: "token-" + profile,
		Scopes:      scopes,
		MemberURN:   memberURN,
		ExpiresAt:   time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	return store
}

type approvalRecordingTransport struct {
	fakeTransport
	t              *testing.T
	wantAuthorURN  string
	wantOwnerURN   string
	wantUploadPath string
	wantUploadAlt  string
	sawCreate      bool
	sawInit        bool
	sawUpload      bool
}

func (tpt *approvalRecordingTransport) CreatePost(_ context.Context, req api.CreatePostRequest) (*outputtest.PostSummary, error) {
	tpt.sawCreate = true
	if req.AuthorURN != tpt.wantAuthorURN {
		tpt.t.Fatalf("author_urn = %q, want %q", req.AuthorURN, tpt.wantAuthorURN)
	}
	if tpt.wantUploadPath != "" {
		if req.MediaPayload == nil {
			tpt.t.Fatal("media payload missing")
		}
		if req.MediaPayload.Alt != tpt.wantUploadAlt {
			tpt.t.Fatalf("media alt = %q, want %q", req.MediaPayload.Alt, tpt.wantUploadAlt)
		}
	} else if req.MediaPayload != nil {
		tpt.t.Fatalf("unexpected media payload: %+v", req.MediaPayload)
	}

	return tpt.fakeTransport.CreatePost(context.Background(), req)
}

func (tpt *approvalRecordingTransport) InitializeImageUpload(_ context.Context, ownerURN string) (string, string, error) {
	tpt.sawInit = true
	if ownerURN != tpt.wantOwnerURN {
		tpt.t.Fatalf("image upload owner = %q, want %q", ownerURN, tpt.wantOwnerURN)
	}
	return tpt.fakeTransport.InitializeImageUpload(context.Background(), ownerURN)
}

func (tpt *approvalRecordingTransport) UploadImageBinary(_ context.Context, _, filePath string) error {
	tpt.sawUpload = true
	if filePath != tpt.wantUploadPath {
		tpt.t.Fatalf("upload path = %q, want %q", filePath, tpt.wantUploadPath)
	}
	return nil
}

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

func TestApprovalRun_UsesStoredProfileTransportAndAuthorURN(t *testing.T) {
	astore := approval.NewMemoryStore()

	code, _, stderr := executeTestCommand(t,
		[]string{
			"--json",
			"--profile", "staged-profile",
			"--transport", "unofficial",
			"--accept-unofficial-risk",
			"post", "create",
			"--text", "Hello staged org",
			"--as-org", "urn:li:organization:111",
			"--require-approval",
		},
		testDepsOptions{approvalStore: astore})
	if code != 3 {
		t.Fatalf("expected exit 3, got %d stderr=%s", code, stderr)
	}

	items, err := astore.List(t.Context())
	if err != nil {
		t.Fatalf("list approvals: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 staged entry, got %d", len(items))
	}
	cmdID := items[0].CommandID

	code, _, stderr = executeTestCommand(t,
		[]string{"--json", "approval", "grant", cmdID},
		testDepsOptions{approvalStore: astore})
	if code != 0 {
		t.Fatalf("grant exit %d stderr=%s", code, stderr)
	}

	transport := &approvalRecordingTransport{
		fakeTransport: fakeTransport{name: "unofficial"},
		t:             t,
		wantAuthorURN: "urn:li:organization:111",
	}

	var (
		gotProfile   string
		gotTransport string
		gotSession   string
	)

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", cmdID},
		testDepsOptions{
			store: authenticatedStoreForProfile(t,
				"staged-profile",
				"unofficial",
				"urn:li:person:stage123",
				"openid", "profile", "email", "w_member_social", "w_organization_social",
			),
			approvalStore: astore,
			transportFactory: func(_ context.Context, settings config.Settings, session auth.Session, _ *slog.Logger) (api.Transport, error) {
				gotProfile = settings.Profile
				gotTransport = settings.Transport
				gotSession = session.Profile
				return transport, nil
			},
		})
	if code != 0 {
		t.Fatalf("run exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if gotProfile != "staged-profile" {
		t.Fatalf("transport factory profile = %q, want staged-profile", gotProfile)
	}
	if gotTransport != "unofficial" {
		t.Fatalf("transport factory transport = %q, want unofficial", gotTransport)
	}
	if gotSession != "staged-profile" {
		t.Fatalf("transport factory session profile = %q, want staged-profile", gotSession)
	}
	if !transport.sawCreate {
		t.Fatal("expected CreatePost to be called")
	}
}

func TestApprovalRun_ReplaysStoredImageUploadPayload(t *testing.T) {
	astore := approval.NewMemoryStore()
	imagePath := t.TempDir() + "/photo.jpg"
	if err := os.WriteFile(imagePath, []byte("image-bytes"), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	entry := approval.Entry{
		CommandID: "approval-image",
		Command:   "post create",
		CreatedAt: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		Profile:   "image-profile",
		Transport: "official",
		Payload: outputtest.PostPayloadPreview{
			Endpoint:   "POST /rest/posts",
			Text:       "Replay image payload",
			Visibility: outputtest.VisibilityPublic,
			AuthorURN:  "urn:li:organization:222",
			WouldUpload: &outputtest.ImageUploadPreview{
				Path:           imagePath,
				PlaceholderURN: "urn:li:image:<to-be-uploaded>",
				Alt:            "poster alt",
			},
		},
	}
	if _, err := astore.Stage(t.Context(), entry); err != nil {
		t.Fatalf("stage approval: %v", err)
	}
	if err := astore.Grant(t.Context(), entry.CommandID); err != nil {
		t.Fatalf("grant approval: %v", err)
	}

	transport := &approvalRecordingTransport{
		fakeTransport:  fakeTransport{name: "official"},
		t:              t,
		wantAuthorURN:  "urn:li:organization:222",
		wantOwnerURN:   "urn:li:person:image123",
		wantUploadPath: imagePath,
		wantUploadAlt:  "poster alt",
	}

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", entry.CommandID},
		testDepsOptions{
			store: authenticatedStoreForProfile(t,
				"image-profile",
				"official",
				"urn:li:person:image123",
				"openid", "profile", "email", "w_member_social", "w_organization_social",
			),
			approvalStore: astore,
			transportFactory: func(_ context.Context, settings config.Settings, session auth.Session, _ *slog.Logger) (api.Transport, error) {
				if settings.Profile != "image-profile" {
					t.Fatalf("settings profile = %q, want image-profile", settings.Profile)
				}
				if settings.Transport != "official" {
					t.Fatalf("settings transport = %q, want official", settings.Transport)
				}
				if session.Profile != "image-profile" {
					t.Fatalf("session profile = %q, want image-profile", session.Profile)
				}
				return transport, nil
			},
		})
	if code != 0 {
		t.Fatalf("run exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !transport.sawInit {
		t.Fatal("expected InitializeImageUpload to be called")
	}
	if !transport.sawUpload {
		t.Fatal("expected UploadImageBinary to be called")
	}
	if !transport.sawCreate {
		t.Fatal("expected CreatePost to be called")
	}

	items, err := astore.List(t.Context())
	if err != nil {
		t.Fatalf("list approvals: %v", err)
	}
	if len(items) != 1 || items[0].State != approval.StateCompleted {
		t.Fatalf("approval state = %+v, want completed entry", items)
	}
}

func TestPostCreateRequireApproval_PreservesImagePayload(t *testing.T) {
	astore := approval.NewMemoryStore()
	imagePath := t.TempDir() + "/photo.jpg"
	if err := os.WriteFile(imagePath, []byte("image-bytes"), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	code, _, stderr := executeTestCommand(t,
		[]string{
			"--json",
			"--profile", "image-profile",
			"post", "create",
			"--text", "Replay image payload",
			"--image", imagePath,
			"--image-alt", "poster alt",
			"--as-org", "urn:li:organization:222",
			"--require-approval",
		},
		testDepsOptions{approvalStore: astore})
	if code != 3 {
		t.Fatalf("expected exit 3, got %d stderr=%s", code, stderr)
	}

	items, err := astore.List(t.Context())
	if err != nil {
		t.Fatalf("list approvals: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 staged entry, got %d", len(items))
	}
	cmdID := items[0].CommandID

	code, _, stderr = executeTestCommand(t,
		[]string{"--json", "approval", "grant", cmdID},
		testDepsOptions{approvalStore: astore})
	if code != 0 {
		t.Fatalf("grant exit %d stderr=%s", code, stderr)
	}

	transport := &approvalRecordingTransport{
		fakeTransport:  fakeTransport{name: "official"},
		t:              t,
		wantAuthorURN:  "urn:li:organization:222",
		wantOwnerURN:   "urn:li:person:image123",
		wantUploadPath: imagePath,
		wantUploadAlt:  "poster alt",
	}

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", cmdID},
		testDepsOptions{
			store: authenticatedStoreForProfile(t,
				"image-profile",
				"official",
				"urn:li:person:image123",
				"openid", "profile", "email", "w_member_social", "w_organization_social",
			),
			approvalStore: astore,
			transportFactory: func(_ context.Context, settings config.Settings, session auth.Session, _ *slog.Logger) (api.Transport, error) {
				if settings.Profile != "image-profile" {
					t.Fatalf("settings profile = %q, want image-profile", settings.Profile)
				}
				if settings.Transport != "official" {
					t.Fatalf("settings transport = %q, want official", settings.Transport)
				}
				if session.Profile != "image-profile" {
					t.Fatalf("session profile = %q, want image-profile", session.Profile)
				}
				return transport, nil
			},
		})
	if code != 0 {
		t.Fatalf("run exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !transport.sawInit {
		t.Fatal("expected InitializeImageUpload to be called")
	}
	if !transport.sawUpload {
		t.Fatal("expected UploadImageBinary to be called")
	}
	if !transport.sawCreate {
		t.Fatal("expected CreatePost to be called")
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
