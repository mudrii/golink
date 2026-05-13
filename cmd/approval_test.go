package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	"github.com/mudrii/golink/internal/idempotency"
	outputtest "github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

// schemaPath is defined in batch_test.go; reused here.

func authenticatedStoreForProfile(t *testing.T, profile, transport, memberURN string, scopes ...string) auth.Store {
	t.Helper()

	store := auth.NewMemoryStore()
	if err := store.SaveSession(t.Context(), auth.Session{
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
	gotText        string
}

type approvalImageErrorTransport struct {
	fakeTransport
	initErr   error
	uploadErr error
}

func (tpt *approvalImageErrorTransport) InitializeImageUpload(context.Context, string) (string, string, error) {
	if tpt.initErr != nil {
		return "", "", tpt.initErr
	}
	return "https://upload.example.com/image", "urn:li:image:123", nil
}

func (tpt *approvalImageErrorTransport) UploadImageBinary(context.Context, string, string) error {
	return tpt.uploadErr
}

func (tpt *approvalRecordingTransport) CreatePost(_ context.Context, req api.CreatePostRequest) (*outputtest.PostSummary, error) {
	tpt.sawCreate = true
	tpt.gotText = req.Text
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

	return tpt.fakeTransport.CreatePost(tpt.t.Context(), req)
}

func (tpt *approvalRecordingTransport) InitializeImageUpload(_ context.Context, ownerURN string) (string, string, error) {
	tpt.sawInit = true
	if ownerURN != tpt.wantOwnerURN {
		tpt.t.Fatalf("image upload owner = %q, want %q", ownerURN, tpt.wantOwnerURN)
	}
	return tpt.fakeTransport.InitializeImageUpload(tpt.t.Context(), ownerURN)
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

	code, _, stderr = executeTestCommand(t,
		[]string{"--json", "approval", "grant", cmdID},
		testDepsOptions{approvalStore: astore})
	if code != 2 {
		t.Fatalf("expected second grant exit 2, got %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr.String(), "wrong state") {
		t.Fatalf("stderr = %s, want wrong state", stderr)
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

func TestApprovalRun_ReplaysCachedIdempotencyResult(t *testing.T) {
	astore := approval.NewMemoryStore()
	istore := idempotency.NewMemoryStore()
	entry := approval.Entry{
		CommandID:      "approval-cached",
		Command:        "post create",
		CreatedAt:      time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		Profile:        "default",
		Transport:      "official",
		IdempotencyKey: "approval-key",
		Payload: map[string]any{
			"text":       "Cached approval payload",
			"visibility": "PUBLIC",
		},
	}
	if _, err := astore.Stage(t.Context(), entry); err != nil {
		t.Fatalf("stage approval: %v", err)
	}
	if err := astore.Grant(t.Context(), entry.CommandID); err != nil {
		t.Fatalf("grant approval: %v", err)
	}
	resultBytes, err := json.Marshal(outputtest.PostCreateData{
		PostSummary: outputtest.PostSummary{
			ID:  "urn:li:share:cached-approval",
			URL: "https://www.linkedin.com/feed/update/urn:li:share:cached-approval",
		},
	})
	if err != nil {
		t.Fatalf("marshal cached result: %v", err)
	}
	if err := istore.Record(t.Context(), idempotency.Entry{
		TS:         time.Now().UTC(),
		Key:        "approval-key",
		Command:    "post create",
		CommandID:  "cached-command",
		HTTPStatus: 201,
		RequestID:  "req-cached",
		Result:     resultBytes,
	}); err != nil {
		t.Fatalf("record idempotency: %v", err)
	}

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", entry.CommandID},
		testDepsOptions{
			approvalStore:    astore,
			idempotencyStore: istore,
			transportFactory: factoryReturning(&failingTransport{createErr: fmt.Errorf("transport must not be called")}),
		})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	var payload struct {
		FromCache bool `json:"from_cache"`
		Data      struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.FromCache {
		t.Fatal("expected from_cache=true")
	}
	if payload.Data.ID != "urn:li:share:cached-approval" {
		t.Fatalf("cached id = %q", payload.Data.ID)
	}
	items, err := astore.List(t.Context())
	if err != nil {
		t.Fatalf("list approval: %v", err)
	}
	if len(items) != 1 || items[0].State != approval.StateCompleted {
		t.Fatalf("approval state = %+v, want completed", items)
	}
}

func TestApprovalRun_CachedReplayFailsWhenCompleteFails(t *testing.T) {
	astore := approval.NewMemoryStore()
	istore := idempotency.NewMemoryStore()
	entry := approval.Entry{
		CommandID:      "approval-cached-complete-fails",
		Command:        "post create",
		CreatedAt:      time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		Profile:        "default",
		Transport:      "official",
		IdempotencyKey: "approval-key",
		Payload: map[string]any{
			"text":       "Cached approval payload",
			"visibility": "PUBLIC",
		},
	}
	if _, err := astore.Stage(t.Context(), entry); err != nil {
		t.Fatalf("stage approval: %v", err)
	}
	if err := astore.Grant(t.Context(), entry.CommandID); err != nil {
		t.Fatalf("grant approval: %v", err)
	}
	resultBytes, err := json.Marshal(outputtest.PostCreateData{
		PostSummary: outputtest.PostSummary{ID: "urn:li:share:cached-approval"},
	})
	if err != nil {
		t.Fatalf("marshal cached result: %v", err)
	}
	if err := istore.Record(t.Context(), idempotency.Entry{
		TS:         time.Now().UTC(),
		Key:        "approval-key",
		Command:    "post create",
		CommandID:  "cached-command",
		HTTPStatus: 201,
		Result:     resultBytes,
	}); err != nil {
		t.Fatalf("record idempotency: %v", err)
	}
	sink := audit.NewMemorySink()

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", entry.CommandID},
		testDepsOptions{
			approvalStore:    completeFailApprovalStore{Store: astore, err: errors.New("rename failed")},
			idempotencyStore: istore,
			auditSink:        sink,
			transportFactory: factoryReturning(&failingTransport{createErr: fmt.Errorf("transport must not be called")}),
		})
	if code != 5 {
		t.Fatalf("expected exit 5, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout)
	}
	items, err := astore.List(t.Context())
	if err != nil {
		t.Fatalf("list approval: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("approval state = %+v, want removed after failed complete", items)
	}
	entries := sink.Entries()
	if len(entries) != 1 || entries[0].Status != "error" || entries[0].ErrorCode != string(outputtest.ErrorCodeTransport) {
		t.Fatalf("audit entries = %+v, want one transport error", entries)
	}
}

func TestApprovalRun_FreshExecutionFailsWhenCompleteFails(t *testing.T) {
	astore := approval.NewMemoryStore()
	entry := approval.Entry{
		CommandID: "approval-complete-fails",
		Command:   "post create",
		CreatedAt: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		Profile:   "default",
		Transport: "official",
		Payload: map[string]any{
			"text":       "Approval payload",
			"visibility": "PUBLIC",
		},
	}
	if _, err := astore.Stage(t.Context(), entry); err != nil {
		t.Fatalf("stage approval: %v", err)
	}
	if err := astore.Grant(t.Context(), entry.CommandID); err != nil {
		t.Fatalf("grant approval: %v", err)
	}
	sink := audit.NewMemorySink()

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", entry.CommandID},
		testDepsOptions{
			store:            authenticatedStore(t),
			approvalStore:    completeFailApprovalStore{Store: astore, err: errors.New("rename failed")},
			auditSink:        sink,
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 5 {
		t.Fatalf("expected exit 5, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout)
	}
	items, err := astore.List(t.Context())
	if err != nil {
		t.Fatalf("list approval: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("approval state = %+v, want removed after failed complete", items)
	}
	entries := sink.Entries()
	if len(entries) != 1 || entries[0].Status != "error" || entries[0].ErrorCode != string(outputtest.ErrorCodeTransport) {
		t.Fatalf("audit entries = %+v, want one transport error", entries)
	}

	code, stdout, stderr = executeTestCommand(t,
		[]string{"--json", "approval", "run", entry.CommandID},
		testDepsOptions{
			store:            authenticatedStore(t),
			approvalStore:    astore,
			auditSink:        sink,
			transportFactory: factoryReturning(&failingTransport{createErr: fmt.Errorf("transport must not be called")}),
		})
	if code != 2 {
		t.Fatalf("expected retry exit 2, got %d stdout=%s stderr=%s", code, stdout, stderr)
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
		wantOwnerURN:   "urn:li:organization:222",
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

func TestRunApprovedCommandSupportedCommandPayloads(t *testing.T) {
	a := newCoverageApp(t)
	cmd := &cobra.Command{Use: "approval run"}
	cmd.SetContext(t.Context())
	base := approvedRunInput{
		commandID: "cmd_approved_direct",
		session: auth.Session{
			Profile:   "default",
			MemberURN: "urn:li:person:abc123",
			Scopes:    []string{"w_member_social"},
		},
		transport: &fakeTransport{name: "official"},
		validate: func(message, details string) error {
			return errors.New(message + ": " + details)
		},
	}

	cases := []struct {
		name     string
		command  string
		payload  map[string]any
		wantHTTP int
		assert   func(t *testing.T, data any)
	}{
		{
			name:     "post delete",
			command:  "post delete",
			payload:  map[string]any{"post_urn": "urn:li:share:1"},
			wantHTTP: 204,
			assert: func(t *testing.T, data any) {
				t.Helper()
				if got := data.(*outputtest.PostDeleteData).ID; got != "urn:li:share:1" {
					t.Fatalf("delete id = %q", got)
				}
			},
		},
		{
			name:    "post edit",
			command: "post edit",
			payload: map[string]any{
				"post_urn": "urn:li:share:2",
				"patch": map[string]any{
					"$set": map[string]any{
						"commentary": "updated text",
						"visibility": "CONNECTIONS",
					},
				},
			},
			wantHTTP: 204,
			assert: func(t *testing.T, data any) {
				t.Helper()
				got := data.(*outputtest.PostEditData)
				if got.Text != "updated text" || got.Visibility != outputtest.VisibilityConnections {
					t.Fatalf("edit data = %+v", got)
				}
			},
		},
		{
			name:     "post reshare",
			command:  "post reshare",
			payload:  map[string]any{"parent_urn": "urn:li:share:3", "commentary": "shared", "visibility": "CONNECTIONS"},
			wantHTTP: 201,
			assert: func(t *testing.T, data any) {
				t.Helper()
				got := data.(outputtest.PostCreateData)
				if got.Text != "shared" || got.Visibility != outputtest.VisibilityConnections {
					t.Fatalf("reshare data = %+v", got)
				}
			},
		},
		{
			name:     "comment add",
			command:  "comment add",
			payload:  map[string]any{"post_urn": "urn:li:share:4", "text": "good point"},
			wantHTTP: 201,
			assert: func(t *testing.T, data any) {
				t.Helper()
				got := data.(outputtest.CommentAddData)
				if got.Text != "good point" || got.PostURN != "urn:li:share:4" {
					t.Fatalf("comment data = %+v", got)
				}
			},
		},
		{
			name:     "react add",
			command:  "react add",
			payload:  map[string]any{"post_urn": "urn:li:share:5", "type": "PRAISE"},
			wantHTTP: 201,
			assert: func(t *testing.T, data any) {
				t.Helper()
				got := data.(outputtest.ReactionAddData)
				if got.Type != outputtest.ReactionPraise || got.TargetURN != "urn:li:share:5" {
					t.Fatalf("reaction data = %+v", got)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			input.command = tc.command
			input.payload = tc.payload
			got, err := a.runApprovedCommand(cmd, input)
			if err != nil {
				t.Fatalf("runApprovedCommand: %v", err)
			}
			if got.httpStatus != tc.wantHTTP {
				t.Fatalf("http status = %d, want %d", got.httpStatus, tc.wantHTTP)
			}
			tc.assert(t, got.data)
		})
	}
}

func TestRunApprovedCommandValidationFailures(t *testing.T) {
	a := newCoverageApp(t)
	cmd := &cobra.Command{Use: "approval run"}
	cmd.SetContext(t.Context())
	base := approvedRunInput{
		commandID: "cmd_approved_invalid",
		session:   auth.Session{MemberURN: "urn:li:person:abc123", Scopes: []string{"w_member_social"}},
		transport: &fakeTransport{name: "official"},
		validate: func(message, details string) error {
			return errors.New(message + ": " + details)
		},
	}

	for _, tc := range []struct {
		name    string
		command string
		payload map[string]any
	}{
		{name: "unsupported", command: "post pin", payload: map[string]any{}},
		{name: "bad create visibility type", command: "post create", payload: map[string]any{
			"text":       "hello from approval",
			"visibility": 42,
		}},
		{name: "bad edit visibility type", command: "post edit", payload: map[string]any{
			"post_urn": "urn:li:share:1",
			"patch":    map[string]any{"$set": map[string]any{"visibility": 42}},
		}},
		{name: "bad reaction type", command: "react add", payload: map[string]any{"post_urn": "urn:li:share:1", "type": 42}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			input.command = tc.command
			input.payload = tc.payload
			if _, err := a.runApprovedCommand(cmd, input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestAttachApprovedImageErrorBranches(t *testing.T) {
	a := newCoverageApp(t)
	cmd := &cobra.Command{Use: "approval run"}
	cmd.SetContext(t.Context())
	imagePath := t.TempDir() + "/image.jpg"
	if err := os.WriteFile(imagePath, []byte("image-bytes"), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	validate := func(message, details string) error {
		return errors.New(strings.TrimSpace(message + " " + details))
	}

	for _, tc := range []struct {
		name      string
		path      string
		transport api.Transport
		want      string
	}{
		{
			name:      "stat failure",
			path:      imagePath + ".missing",
			transport: &approvalImageErrorTransport{fakeTransport: fakeTransport{name: "official"}},
			want:      "cannot read image file",
		},
		{
			name:      "upload init failure",
			path:      imagePath,
			transport: &approvalImageErrorTransport{fakeTransport: fakeTransport{name: "official"}, initErr: errors.New("init failed")},
			want:      "init failed",
		},
		{
			name:      "upload binary failure",
			path:      imagePath,
			transport: &approvalImageErrorTransport{fakeTransport: fakeTransport{name: "official"}, uploadErr: errors.New("upload failed")},
			want:      "upload failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &api.CreatePostRequest{}
			err := a.attachApprovedImage(cmd, approvedRunInput{
				commandID: "cmd_image_error",
				session:   auth.Session{MemberURN: "urn:li:person:abc123"},
				transport: tc.transport,
				validate:  validate,
				payload: map[string]any{
					"would_upload": map[string]any{"path": tc.path, "alt": "alt text"},
				},
			}, "", req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want %q", err.Error(), tc.want)
			}
		})
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
		wantOwnerURN:   "urn:li:organization:222",
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

func TestPostCreateImageAsOrgUsesOrgUploadOwner(t *testing.T) {
	imagePath := t.TempDir() + "/photo.png"
	pngHeader := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if err := os.WriteFile(imagePath, pngHeader, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	transport := &approvalRecordingTransport{
		fakeTransport:  fakeTransport{name: "official"},
		t:              t,
		wantAuthorURN:  "urn:li:organization:222",
		wantOwnerURN:   "urn:li:organization:222",
		wantUploadPath: imagePath,
		wantUploadAlt:  "poster alt",
	}

	code, stdout, stderr := executeTestCommand(t,
		[]string{
			"--json",
			"post", "create",
			"--text", "Direct org image payload",
			"--image", imagePath,
			"--image-alt", "poster alt",
			"--as-org", "urn:li:organization:222",
		},
		testDepsOptions{
			store: authenticatedStoreForProfile(t,
				"default",
				"official",
				"urn:li:person:image123",
				"openid", "profile", "email", "w_member_social", "w_organization_social",
			),
			transportFactory: factoryReturning(transport),
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

func TestApprovalRun_ValidationFailureIsAudited(t *testing.T) {
	astore := approval.NewMemoryStore()
	sink := audit.NewMemorySink()
	// The stored approval intentionally asks to post as an organization while the
	// session only has member-write scope; approval run must audit that validation
	// failure before returning it.
	entry := approval.Entry{
		CommandID: "approval-validation",
		Command:   "post create",
		CreatedAt: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		Profile:   "validation-profile",
		Transport: "official",
		Payload: outputtest.PostPayloadPreview{
			Endpoint:   "POST /rest/posts",
			Text:       "Validation payload",
			Visibility: outputtest.VisibilityPublic,
			AuthorURN:  "urn:li:organization:222",
		},
	}
	if _, err := astore.Stage(t.Context(), entry); err != nil {
		t.Fatalf("stage approval: %v", err)
	}
	if err := astore.Grant(t.Context(), entry.CommandID); err != nil {
		t.Fatalf("grant approval: %v", err)
	}

	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", entry.CommandID},
		testDepsOptions{
			store: authenticatedStoreForProfile(t,
				"validation-profile",
				"official",
				"urn:li:person:validation123",
				"openid", "profile", "email", "w_member_social",
			),
			approvalStore:    astore,
			auditSink:        sink,
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 2 {
		t.Fatalf("expected exit 2, got %d stderr=%s", code, stderr)
	}

	entries := sink.Entries()
	if len(entries) == 0 {
		t.Fatal("expected validation audit entry")
	}
	got := entries[len(entries)-1]
	if got.CommandID != entry.CommandID {
		t.Fatalf("audit command_id = %q, want %q", got.CommandID, entry.CommandID)
	}
	if got.Status != "validation_error" {
		t.Fatalf("audit status = %q, want validation_error", got.Status)
	}
	if got.ErrorCode != string(outputtest.ErrorCodeValidation) {
		t.Fatalf("audit error_code = %q, want %q", got.ErrorCode, outputtest.ErrorCodeValidation)
	}
}

type completeFailApprovalStore struct {
	approval.Store
	err error
}

func (s completeFailApprovalStore) Complete(context.Context, string) error {
	return s.err
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

	code, _, stderr = executeTestCommand(t,
		[]string{"--json", "approval", "deny", cmdID},
		testDepsOptions{approvalStore: astore})
	if code != 2 {
		t.Fatalf("expected second deny exit 2, got %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr.String(), "wrong state") {
		t.Fatalf("stderr = %s, want wrong state", stderr)
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

// TestApprovalRun_FileStoreRoundTripsPayloadVerbatim is the end-to-end guard
// for H1: stage a --require-approval post through a real on-disk FileStore,
// grant it, run it, and assert the transport receives the operator's literal
// text. A prior version of FileStore.Stage redacted "text" before writing,
// which caused the literal string "REDACTED" to be posted to LinkedIn on run.
func TestApprovalRun_FileStoreRoundTripsPayloadVerbatim(t *testing.T) {
	dir := t.TempDir()
	astore := approval.NewFileStore(dir)

	const wantText = "Real announcement: we are launching tomorrow."

	code, _, stderr := executeTestCommand(t,
		[]string{"--json", "post", "create", "--text", wantText, "--require-approval"},
		testDepsOptions{approvalStore: astore})
	if code != 3 {
		t.Fatalf("stage exit %d stderr=%s", code, stderr)
	}

	items, err := astore.List(t.Context())
	if err != nil {
		t.Fatalf("list approvals: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 staged entry, got %d", len(items))
	}
	cmdID := items[0].CommandID

	// Independently verify the on-disk file does not contain "REDACTED".
	pending := filepath.Join(dir, cmdID+".pending.json")
	raw, err := os.ReadFile(pending)
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if strings.Contains(string(raw), "REDACTED") {
		t.Fatalf("pending file contains REDACTED: %s", raw)
	}
	if !strings.Contains(string(raw), wantText) {
		t.Fatalf("pending file missing original text: %s", raw)
	}

	code, _, stderr = executeTestCommand(t,
		[]string{"--json", "approval", "grant", cmdID},
		testDepsOptions{approvalStore: astore})
	if code != 0 {
		t.Fatalf("grant exit %d stderr=%s", code, stderr)
	}

	transport := &approvalRecordingTransport{
		fakeTransport: fakeTransport{name: "official"},
		t:             t,
	}

	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "approval", "run", cmdID},
		testDepsOptions{
			store:            authenticatedStore(t),
			approvalStore:    astore,
			transportFactory: factoryReturning(transport),
		})
	if code != 0 {
		t.Fatalf("run exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !transport.sawCreate {
		t.Fatal("expected CreatePost to be called")
	}
	if transport.gotText != wantText {
		t.Fatalf("dispatched text = %q, want %q (H1 regression: payload was redacted on disk)", transport.gotText, wantText)
	}
}
