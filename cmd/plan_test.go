package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	"github.com/mudrii/golink/internal/output"
)

func TestPlanPostCreate_emitsValidPlan(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "plan", "post", "create", "--text", "hello world", "--visibility", "PUBLIC"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}

	var env output.PlanOutput
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal plan output: %v\nraw: %s", err, stdout.String())
	}
	if env.Command != "plan" {
		t.Errorf("command = %q, want %q", env.Command, "plan")
	}
	if env.Data.Schema != "golink.plan/v1" {
		t.Errorf("schema = %q, want %q", env.Data.Schema, "golink.plan/v1")
	}
	if env.Data.Command != "post create" {
		t.Errorf("data.command = %q, want %q", env.Data.Command, "post create")
	}
	if env.Data.Args["text"] != "hello world" {
		t.Errorf("args.text = %v, want %q", env.Data.Args["text"], "hello world")
	}
}

func TestPlanPostCreate_missingText(t *testing.T) {
	code, _, _ := executeTestCommand(t,
		[]string{"--json", "plan", "post", "create"},
		testDepsOptions{})
	if code == 0 {
		t.Fatal("expected non-zero exit for missing --text")
	}
}

func TestPlanCommentAdd_emitsValidPlan(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "plan", "comment", "add", "--post-urn", "urn:li:share:123", "--text", "nice post"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}

	var env output.PlanOutput
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, stdout.String())
	}
	if env.Data.Command != "comment add" {
		t.Errorf("command = %q, want %q", env.Data.Command, "comment add")
	}
}

func TestPlanReactAdd_emitsValidPlan(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "plan", "react", "add", "--post-urn", "urn:li:share:456", "--type", "PRAISE"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}

	var env output.PlanOutput
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, stdout.String())
	}
	if env.Data.Command != "react add" {
		t.Errorf("command = %q, want %q", env.Data.Command, "react add")
	}
	if env.Data.Args["type"] != "PRAISE" {
		t.Errorf("args.type = %v, want PRAISE", env.Data.Args["type"])
	}
}

func TestPlanPostDelete_emitsValidPlan(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "plan", "post", "delete", "--post-urn", "urn:li:share:789"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}
	var env output.PlanOutput
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.Command != "post delete" {
		t.Errorf("command = %q, want %q", env.Data.Command, "post delete")
	}
}

func TestPlanPostReshare_emitsValidPlan(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "plan", "post", "reshare", "--parent-urn", "urn:li:share:999"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}
	var env output.PlanOutput
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.Command != "post reshare" {
		t.Errorf("command = %q, want %q", env.Data.Command, "post reshare")
	}
}

func TestPlanDryRunFlag_setInPlan(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "plan", "post", "create", "--text", "dry"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}
	var env output.PlanOutput
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Data.DryRun {
		t.Error("expected dry_run=true in plan data")
	}
}

func TestPlanTransportField(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "plan", "post", "create", "--text", "t"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}
	var env output.PlanOutput
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.Transport == "" {
		t.Error("transport should be set in plan data")
	}
}

func TestPlanPostSchedule_emitsValidPlan(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "plan", "post", "schedule", "--text", "scheduled post", "--at", "2026-05-01T12:00:00Z"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}
	var env output.PlanOutput
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.Command != "post schedule" {
		t.Errorf("command = %q, want %q", env.Data.Command, "post schedule")
	}
}

func TestPlanOutput_schemaValidates(t *testing.T) {
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "plan", "post", "create", "--text", "schema check"},
		testDepsOptions{})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}
	output.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
}

func TestExecute_invalidPlanFile(t *testing.T) {
	code, _, _ := executeTestCommand(t,
		[]string{"--json", "execute", "/nonexistent/plan.json"},
		testDepsOptions{})
	if code == 0 {
		t.Fatal("expected non-zero exit for missing plan file")
	}
}

func TestExecute_invalidPlanJSON(t *testing.T) {
	tmp := writeTempFile(t, []byte(`{"schema":"bad"}`))
	code, _, _ := executeTestCommand(t,
		[]string{"--json", "execute", tmp},
		testDepsOptions{})
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid plan")
	}
}

func TestExecute_nonPlannableCommand(t *testing.T) {
	payload := `{"schema":"golink.plan/v1","created_at":"2026-04-17T12:00:00Z","command":"post list","args":{},"transport":"official","profile":"default"}`
	tmp := writeTempFile(t, []byte(payload))
	code, _, _ := executeTestCommand(t,
		[]string{"--json", "execute", tmp},
		testDepsOptions{})
	if code == 0 {
		t.Fatal("expected non-zero exit for non-plannable command")
	}
}

func TestExecute_dryRun_dispatches(t *testing.T) {
	payload := `{"schema":"golink.plan/v1","created_at":"2026-04-17T12:00:00Z","command":"post create","args":{"text":"hello","visibility":"PUBLIC"},"transport":"official","profile":"default"}`
	tmp := writeTempFile(t, []byte(payload))
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "execute", tmp},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
		})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s stdout: %s", code, stderr, stdout)
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, stdout.String())
	}
	if env["status"] != "ok" {
		t.Errorf("status = %v, want ok", env["status"])
	}
}

func TestExecute_auditRecordsPlanSHA256(t *testing.T) {
	sink := audit.NewMemorySink()
	payload := `{"schema":"golink.plan/v1","created_at":"2026-04-17T12:00:00Z","command":"post create","args":{"text":"audit test","visibility":"PUBLIC"},"transport":"official","profile":"default"}`
	tmp := writeTempFile(t, []byte(payload))
	t.Setenv("GOLINK_AUDIT", "on")
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--dry-run", "execute", tmp},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
			auditSink:        sink,
		})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s stdout: %s", code, stderr, stdout)
	}
	entries := sink.Entries()
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
	for _, e := range entries {
		if e.PlanSHA256 == "" {
			t.Errorf("audit entry %q missing plan_sha256", e.Command)
		}
	}
}

func TestExecute_UsesPlanProfileAndTransport(t *testing.T) {
	store := auth.NewMemoryStore()
	for _, session := range []auth.Session{
		{
			Profile:     "default",
			Transport:   "official",
			AccessToken: "default-token",
			MemberURN:   "urn:li:person:default",
			ExpiresAt:   time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			Profile:     "plan-profile",
			Transport:   "auto",
			AccessToken: "plan-token",
			MemberURN:   "urn:li:person:plan",
			ExpiresAt:   time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	} {
		if err := store.SaveSession(t.Context(), session); err != nil {
			t.Fatalf("save session: %v", err)
		}
	}

	payload := `{"schema":"golink.plan/v1","created_at":"2026-04-17T12:00:00Z","command":"post create","args":{"text":"hello","visibility":"PUBLIC"},"transport":"auto","profile":"plan-profile"}`
	tmp := writeTempFile(t, []byte(payload))

	called := false
	code, stdout, stderr := executeTestCommand(t,
		[]string{"--json", "--profile=default", "--transport=official", "execute", tmp},
		testDepsOptions{
			store: store,
			transportFactory: func(_ context.Context, settings config.Settings, session auth.Session, _ *slog.Logger) (api.Transport, error) {
				called = true
				if settings.Profile != "plan-profile" {
					t.Fatalf("settings profile = %q, want plan-profile", settings.Profile)
				}
				if settings.Transport != "auto" {
					t.Fatalf("settings transport = %q, want auto", settings.Transport)
				}
				if session.Profile != "plan-profile" {
					t.Fatalf("session profile = %q, want plan-profile", session.Profile)
				}
				if session.MemberURN != "urn:li:person:plan" {
					t.Fatalf("session member = %q, want urn:li:person:plan", session.MemberURN)
				}
				return &fakeTransport{name: settings.Transport}, nil
			},
		})
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s stdout: %s", code, stderr, stdout)
	}
	if !called {
		t.Fatal("expected transport factory to be called")
	}
}

// writeTempFile writes data to a new temp file and returns the path.
func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}
