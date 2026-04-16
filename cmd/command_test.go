package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	outputtest "github.com/mudrii/golink/internal/output"
)

func TestVersionJSON(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "version"}, testDepsOptions{})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %s", stderr)
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %#v", payload["status"])
	}
	if payload["command"] != "version" {
		t.Fatalf("expected command version, got %#v", payload["command"])
	}
}

func TestAuthStatusJSONWithoutSession(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "auth", "status"}, testDepsOptions{})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Data struct {
			IsAuthenticated bool   `json:"is_authenticated"`
			Profile         string `json:"profile"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if payload.Data.IsAuthenticated {
		t.Fatal("expected unauthenticated status")
	}
	if payload.Data.Profile != "default" {
		t.Fatalf("expected default profile, got %q", payload.Data.Profile)
	}
}

func TestAuthLoginRequiresClientID(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "auth", "login"}, testDepsOptions{})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestAuthLoginJSON(t *testing.T) {
	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	code, stdout, stderr := executeTestCommand(t, []string{"--json", "auth", "login"}, testDepsOptions{
		loginRunner: func(_ context.Context, _ *auth.LoginRequest, profile string, transport string, _ auth.LoginFlowOptions) (*auth.Session, error) {
			return &auth.Session{
				Profile:        profile,
				Transport:      transport,
				AccessToken:    "token",
				ConnectedAt:    time.Date(2026, 4, 16, 12, 0, 1, 0, time.UTC),
				Scopes:         []string{"openid", "profile", "email", "w_member_social"},
				MemberURN:      "urn:li:person:abc123",
				ProfileID:      "abc123",
				Name:           "Ion Mudreac",
				Email:          "ion@example.com",
				LocaleCountry:  "MY",
				LocaleLanguage: "en",
			}, nil
		},
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	lines := bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 json lines, got %d: %s", len(lines), stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), lines[0])
	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), lines[1])
}

func TestAuthLoginTimeoutReturnsAuthError(t *testing.T) {
	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	code, stdout, stderr := executeTestCommand(t, []string{"--json", "--timeout=1ms", "auth", "login"}, testDepsOptions{
		loginRunner: func(ctx context.Context, _ *auth.LoginRequest, _ string, _ string, _ auth.LoginFlowOptions) (*auth.Session, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected login start payload before timeout")
	}

	lines := bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 stdout json line, got %d: %s", len(lines), stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), lines[0])
	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestUnknownFlagUsesJSONValidationEnvelope(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "--bogus"}, testDepsOptions{})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestProfileMeReturnsAuthErrorWithoutSession(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "profile", "me"}, testDepsOptions{})
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestProfileMeRejectsMalformedSession(t *testing.T) {
	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:   "default",
		Transport: "official",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	code, stdout, stderr := executeTestCommand(t, []string{"--json", "profile", "me"}, testDepsOptions{store: store})
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestPostCreateDryRunJSON(t *testing.T) {
	code, stdout, stderr := executeTestCommand(
		t,
		[]string{"--json", "--dry-run", "post", "create", "--text", "Hello from golink", "--visibility", "PUBLIC"},
		testDepsOptions{},
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Mode string `json:"mode"`
		Data struct {
			Mode string `json:"mode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if payload.Mode != "dry_run" || payload.Data.Mode != "dry_run" {
		t.Fatalf("expected dry_run mode, got envelope=%q data=%q", payload.Mode, payload.Data.Mode)
	}
}

func TestPostDeleteRequiresURN(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "post", "delete"}, testDepsOptions{})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())

	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal stderr: %v", err)
	}
	if payload.Command != "post delete" {
		t.Fatalf("expected command post delete, got %q", payload.Command)
	}
}

func TestCommentAddRequiresURNAndText(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "comment", "add"}, testDepsOptions{})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestSearchPeopleRequiresKeywords(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "search", "people"}, testDepsOptions{})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestUnofficialTransportRequiresAcknowledgement(t *testing.T) {
	code, stdout, stderr := executeTestCommand(
		t,
		[]string{"--json", "--transport=unofficial", "version"},
		testDepsOptions{},
	)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestProfileMeUsesStoredSession(t *testing.T) {
	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:        "default",
		Transport:      "official",
		MemberURN:      "urn:li:person:abc123",
		ProfileID:      "abc123",
		Name:           "Ion Mudreac",
		Email:          "ion@example.com",
		LocaleCountry:  "MY",
		LocaleLanguage: "en",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	code, stdout, stderr := executeTestCommand(t, []string{"--json", "profile", "me"}, testDepsOptions{store: store})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
}

func TestAuthStatusRejectsMalformedSession(t *testing.T) {
	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:   "default",
		Transport: "",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	code, stdout, stderr := executeTestCommand(t, []string{"--json", "auth", "status"}, testDepsOptions{store: store})
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestAuthRefreshNoSession(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t, []string{"--json", "auth", "refresh"}, testDepsOptions{})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}
	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestAuthRefreshNoRefreshToken(t *testing.T) {
	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:     "default",
		Transport:   "official",
		AccessToken: "token",
		ExpiresAt:   time.Date(2026, 4, 16, 13, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	code, stdout, stderr := executeTestCommand(t, []string{"--json", "auth", "refresh"}, testDepsOptions{store: store})
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}
	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stderr.Bytes())
}

func TestAuthRefreshSuccess(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"new-token","expires_in":5184000,"scope":"openid profile email w_member_social"}`)
	}))
	defer tokenServer.Close()

	store := auth.NewMemoryStore()
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:      "default",
		Transport:    "official",
		AccessToken:  "old-token",
		ExpiresAt:    time.Date(2026, 4, 16, 12, 4, 0, 0, time.UTC),
		RefreshToken: "refresh-token",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	code, stdout, stderr := executeTestCommandWithHTTP(t, []string{"--json", "auth", "refresh"}, testDepsOptions{
		store: store,
	}, tokenServer)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %s", stderr)
	}
	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Command string `json:"command"`
		Data    struct {
			Profile   string `json:"profile"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Command != "auth refresh" {
		t.Errorf("expected command auth refresh, got %q", payload.Command)
	}
	if payload.Data.Profile != "default" {
		t.Errorf("expected default profile, got %q", payload.Data.Profile)
	}
}

func TestResolveTransportAutoRefreshNearExpiry(t *testing.T) {
	refreshed := false
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshed = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"new-token","expires_in":5184000,"scope":"openid profile"}`)
	}))
	defer tokenServer.Close()

	store := auth.NewMemoryStore()
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	// Session expires in 2 minutes (< 5 min threshold) — auto-refresh should fire.
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:      "default",
		Transport:    "official",
		AccessToken:  "old-token",
		ExpiresAt:    now.Add(2 * time.Minute),
		RefreshToken: "refresh-token",
		MemberURN:    "urn:li:person:abc123",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	// post list triggers resolveSession + resolveTransport; noop transport returns unsupported (exit 0).
	executeTestCommandWithHTTP(t, []string{"--json", "post", "list"}, testDepsOptions{
		store: store,
	}, tokenServer)

	if !refreshed {
		t.Error("expected auto-refresh to be attempted when token near expiry")
	}
}

func TestResolveTransportNoRefreshWhenTokenFresh(t *testing.T) {
	refreshCalled := false
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalled = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"new-token","expires_in":5184000}`)
	}))
	defer tokenServer.Close()

	store := auth.NewMemoryStore()
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	// Session expires in 60 minutes (> 5 min threshold) — no refresh expected.
	if err := store.SaveSession(context.Background(), auth.Session{
		Profile:      "default",
		Transport:    "official",
		AccessToken:  "valid-token",
		ExpiresAt:    now.Add(60 * time.Minute),
		RefreshToken: "refresh-token",
		MemberURN:    "urn:li:person:abc123",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	// post list triggers resolveSession + resolveTransport; noop transport returns unsupported (exit 0).
	executeTestCommandWithHTTP(t, []string{"--json", "post", "list"}, testDepsOptions{
		store: store,
	}, tokenServer)

	if refreshCalled {
		t.Error("expected no auto-refresh when token is fresh")
	}
}

type testDepsOptions struct {
	store            auth.Store
	loginRunner      func(context.Context, *auth.LoginRequest, string, string, auth.LoginFlowOptions) (*auth.Session, error)
	transportFactory TransportFactory
	auditSink        audit.Sink
	approvalStore    approval.Store
}

func executeTestCommand(t *testing.T, args []string, opts testDepsOptions) (int, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	store := opts.store
	if store == nil {
		store = auth.NewMemoryStore()
	}
	astore := opts.approvalStore
	if astore == nil {
		astore = approval.NewMemoryStore()
	}

	code := ExecuteContext(context.Background(), args, Dependencies{
		Stdout:           stdout,
		Stderr:           stderr,
		LoginRunner:      opts.loginRunner,
		Now:              func() time.Time { return time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC) },
		SessionStore:     store,
		IsInteractive:    func() bool { return false },
		TransportFactory: opts.transportFactory,
		AuditSink:        opts.auditSink,
		ApprovalStore:    astore,
	}, BuildInfo{
		Version:   "test",
		Commit:    "abc123",
		BuildDate: "2026-04-16T12:00:00Z",
	})

	return code, stdout, stderr
}

// executeTestCommandWithHTTP runs the command wiring the provided httptest.Server
// as both the HTTP client and token URL so that auth.RefreshAccessToken calls hit it.
func executeTestCommandWithHTTP(t *testing.T, args []string, opts testDepsOptions, tokenServer *httptest.Server) (int, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	store := opts.store
	if store == nil {
		store = auth.NewMemoryStore()
	}
	astore := opts.approvalStore
	if astore == nil {
		astore = approval.NewMemoryStore()
	}

	code := ExecuteContext(context.Background(), args, Dependencies{
		Stdout:           stdout,
		Stderr:           stderr,
		LoginRunner:      opts.loginRunner,
		HTTPClient:       tokenServer.Client(),
		TokenURL:         tokenServer.URL,
		Now:              func() time.Time { return time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC) },
		SessionStore:     store,
		IsInteractive:    func() bool { return false },
		TransportFactory: opts.transportFactory,
		AuditSink:        opts.auditSink,
		ApprovalStore:    astore,
	}, BuildInfo{
		Version:   "test",
		Commit:    "abc123",
		BuildDate: "2026-04-16T12:00:00Z",
	})

	return code, stdout, stderr
}

func TestAuditPostCreateDryRun(t *testing.T) {
	sink := audit.NewMemorySink()
	t.Setenv("GOLINK_AUDIT", "on")

	code, _, stderr := executeTestCommand(
		t,
		[]string{"--json", "--dry-run", "post", "create", "--text", "Hello from golink", "--visibility", "PUBLIC"},
		testDepsOptions{auditSink: sink},
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	entries := sink.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Status != "ok" {
		t.Errorf("status: want ok, got %q", e.Status)
	}
	if e.Mode != "dry_run" {
		t.Errorf("mode: want dry_run, got %q", e.Mode)
	}
	if e.Command != "post create" {
		t.Errorf("command: want post create, got %q", e.Command)
	}
	if len(e.DryRunPreview) == 0 {
		t.Error("expected dry_run_preview to be populated")
	}
}

func TestAuditPostCreateValidationError(t *testing.T) {
	sink := audit.NewMemorySink()

	code, _, _ := executeTestCommand(
		t,
		[]string{"--json", "post", "create"},
		testDepsOptions{auditSink: sink},
	)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}

	entries := sink.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Status != "validation_error" {
		t.Errorf("status: want validation_error, got %q", e.Status)
	}
	if e.Mode != "normal" {
		t.Errorf("mode: want normal, got %q", e.Mode)
	}
}

func TestAuditReadCommandNotAudited(t *testing.T) {
	sink := audit.NewMemorySink()

	executeTestCommand(t, []string{"--json", "version"}, testDepsOptions{auditSink: sink})

	if len(sink.Entries()) != 0 {
		t.Fatalf("expected no audit entries for read command, got %d", len(sink.Entries()))
	}
}

func TestAuditOffSkipsMutating(t *testing.T) {
	t.Setenv("GOLINK_AUDIT", "off")

	sink := audit.NewMemorySink()

	executeTestCommand(
		t,
		[]string{"--json", "--dry-run", "post", "create", "--text", "Hello from golink"},
		testDepsOptions{auditSink: sink},
	)

	if len(sink.Entries()) != 0 {
		t.Fatalf("expected no audit entries when GOLINK_AUDIT=off, got %d", len(sink.Entries()))
	}
}

func schemaPath(t *testing.T) string {
	t.Helper()

	return filepath.Clean(filepath.Join("..", "schemas", "golink-output.schema.json"))
}
