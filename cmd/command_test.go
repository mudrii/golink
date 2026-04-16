package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

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

	code, stdout, stderr := executeTestCommand(t, []string{"--json", "auth", "login"}, testDepsOptions{})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
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

type testDepsOptions struct {
	store auth.Store
}

func executeTestCommand(t *testing.T, args []string, opts testDepsOptions) (int, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	store := opts.store
	if store == nil {
		store = auth.NewMemoryStore()
	}

	code := ExecuteContext(context.Background(), args, Dependencies{
		Stdout:        stdout,
		Stderr:        stderr,
		Now:           func() time.Time { return time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC) },
		SessionStore:  store,
		IsInteractive: func() bool { return false },
	}, BuildInfo{
		Version:   "test",
		Commit:    "abc123",
		BuildDate: "2026-04-16T12:00:00Z",
	})

	return code, stdout, stderr
}

func schemaPath(t *testing.T) string {
	t.Helper()

	return filepath.Clean(filepath.Join("..", "schemas", "golink-output.schema.json"))
}
