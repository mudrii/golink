package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	outputtest "github.com/mudrii/golink/internal/output"
)

// validDoctorSession returns a well-formed authenticated session for use in doctor tests.
func validDoctorSession(now time.Time) auth.Session {
	return auth.Session{
		Profile:          "default",
		Transport:        "official",
		AccessToken:      "tok",
		ExpiresAt:        now.Add(30 * 24 * time.Hour),
		RefreshToken:     "rtok",
		RefreshExpiresAt: now.Add(365 * 24 * time.Hour),
		Scopes:           []string{"openid", "profile", "email", "w_member_social_feed"},
		AuthFlow:         "pkce",
		ConnectedAt:      now.Add(-7 * 24 * time.Hour),
		MemberURN:        "urn:li:person:abc123",
	}
}

// executeDoctorCommand wires a fake userinfo server URL via UserinfoURL in Dependencies.
func executeDoctorCommand(t *testing.T, args []string, opts testDepsOptions, userinfoSrv *httptest.Server) (int, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	store := opts.store
	if store == nil {
		store = auth.NewMemoryStore()
	}

	var httpClient *http.Client
	var userinfoURL string
	if userinfoSrv != nil {
		httpClient = userinfoSrv.Client()
		userinfoURL = userinfoSrv.URL
	}

	code := ExecuteContext(context.Background(), args, Dependencies{
		Stdout:           stdout,
		Stderr:           stderr,
		LoginRunner:      opts.loginRunner,
		HTTPClient:       httpClient,
		Now:              func() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) },
		SessionStore:     store,
		IsInteractive:    func() bool { return false },
		TransportFactory: opts.transportFactory,
		AuditSink:        opts.auditSink,
		UserinfoURL:      userinfoURL,
	}, BuildInfo{
		Version:   "test",
		Commit:    "abc123",
		BuildDate: "2026-04-17T12:00:00Z",
	})

	return code, stdout, stderr
}

func TestDoctorHealthOK(t *testing.T) {
	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := auth.NewMemoryStore()
	sess := validDoctorSession(now)
	if err := store.SaveSession(context.Background(), sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"sub":"urn:li:person:abc123"}`)
	}))
	defer srv.Close()

	code, stdout, stderr := executeDoctorCommand(t, []string{"--json", "doctor"}, testDepsOptions{store: store}, srv)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr)
	}

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())

	var payload struct {
		Data struct {
			Health  string   `json:"health"`
			Errors  []string `json:"errors"`
			Session struct {
				Authenticated bool `json:"authenticated"`
			} `json:"session"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Data.Health != "ok" {
		t.Errorf("health: want ok, got %q (errors: %v)", payload.Data.Health, payload.Data.Errors)
	}
	if !payload.Data.Session.Authenticated {
		t.Error("expected authenticated=true")
	}
}

func TestDoctorExpiredTokenWarning(t *testing.T) {
	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := auth.NewMemoryStore()
	sess := validDoctorSession(now)
	// Token expires in 48 hours — less than 168h threshold
	sess.ExpiresAt = now.Add(48 * time.Hour)
	if err := store.SaveSession(context.Background(), sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"sub":"urn:li:person:abc123"}`)
	}))
	defer srv.Close()

	code, stdout, _ := executeDoctorCommand(t, []string{"--json", "doctor"}, testDepsOptions{store: store}, srv)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	var payload struct {
		Data struct {
			Health   string   `json:"health"`
			Warnings []string `json:"warnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Data.Health != "warnings" {
		t.Errorf("health: want warnings, got %q", payload.Data.Health)
	}
	found := false
	for _, w := range payload.Data.Warnings {
		if strings.Contains(w, "access token expires") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected token-expiry warning in %v", payload.Data.Warnings)
	}
}

func TestDoctorNoSessionWarning(t *testing.T) {
	// No GOLINK_CLIENT_ID, no session — expect warnings, probe not attempted.
	code, stdout, _ := executeTestCommand(t, []string{"--json", "doctor"}, testDepsOptions{})
	if code != 0 {
		t.Fatalf("expected exit 0 (non-fatal), got %d", code)
	}

	var payload struct {
		Data struct {
			Health string `json:"health"`
			Probe  struct {
				Attempted bool `json:"attempted"`
			} `json:"probe"`
			Session struct {
				Authenticated bool `json:"authenticated"`
			} `json:"session"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Data.Health != "warnings" {
		t.Errorf("health: want warnings, got %q", payload.Data.Health)
	}
	if payload.Data.Probe.Attempted {
		t.Error("probe should not be attempted when unauthenticated")
	}
	if payload.Data.Session.Authenticated {
		t.Error("expected authenticated=false when no session")
	}
}

func TestDoctorProbe401Error(t *testing.T) {
	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := auth.NewMemoryStore()
	sess := validDoctorSession(now)
	if err := store.SaveSession(context.Background(), sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	// doctor is non-fatal even on probe error — exit 0
	code, stdout, _ := executeDoctorCommand(t, []string{"--json", "doctor"}, testDepsOptions{store: store}, srv)
	if code != 0 {
		t.Fatalf("expected exit 0 (non-fatal), got %d", code)
	}

	var payload struct {
		Data struct {
			Health string   `json:"health"`
			Errors []string `json:"errors"`
			Probe  struct {
				Attempted bool   `json:"attempted"`
				Status    int    `json:"status"`
				Error     string `json:"error"`
			} `json:"probe"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Data.Health != "error" {
		t.Errorf("health: want error, got %q", payload.Data.Health)
	}
	if !payload.Data.Probe.Attempted {
		t.Error("expected probe.attempted=true")
	}
	if payload.Data.Probe.Status != http.StatusUnauthorized {
		t.Errorf("probe status: want 401, got %d", payload.Data.Probe.Status)
	}
	if payload.Data.Probe.Error == "" {
		t.Error("expected probe.error to be set")
	}
	if len(payload.Data.Errors) == 0 {
		t.Error("expected errors slice to be non-empty")
	}
}

func TestDoctorStrictWarningsExit2(t *testing.T) {
	// No session → warnings → --strict should exit 2.
	code, _, _ := executeTestCommand(t, []string{"--json", "doctor", "--strict"}, testDepsOptions{})
	if code != 2 {
		t.Errorf("--strict with warnings: want exit 2, got %d", code)
	}
}

func TestDoctorStrictOKExit0(t *testing.T) {
	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := auth.NewMemoryStore()
	sess := validDoctorSession(now)
	if err := store.SaveSession(context.Background(), sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"sub":"urn:li:person:abc123"}`)
	}))
	defer srv.Close()

	code, _, stderr := executeDoctorCommand(t, []string{"--json", "doctor", "--strict"}, testDepsOptions{store: store}, srv)
	if code != 0 {
		t.Errorf("--strict with health=ok: want exit 0, got %d stderr=%s", code, stderr)
	}
}

func TestDoctorFeatureMapScopes(t *testing.T) {
	// Scopes: openid + member social write scope → post create supported, post list always unsupported.
	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := auth.NewMemoryStore()
	sess := validDoctorSession(now)
	sess.Scopes = []string{"openid", "w_member_social_feed"}
	if err := store.SaveSession(context.Background(), sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"sub":"urn:li:person:abc123"}`)
	}))
	defer srv.Close()

	code, stdout, stderr := executeDoctorCommand(t, []string{"--json", "doctor"}, testDepsOptions{store: store}, srv)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr)
	}

	var payload struct {
		Data struct {
			Features []struct {
				Command string `json:"command"`
				Status  string `json:"status"`
			} `json:"features"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	featureStatus := make(map[string]string)
	for _, f := range payload.Data.Features {
		featureStatus[f.Command] = f.Status
	}

	if featureStatus["post create"] != "supported" {
		t.Errorf("post create: want supported, got %q", featureStatus["post create"])
	}
	if featureStatus["post list"] != "unsupported" {
		t.Errorf("post list: want unsupported, got %q", featureStatus["post list"])
	}
	if featureStatus["profile me"] != "supported" {
		t.Errorf("profile me: want supported (openid granted), got %q", featureStatus["profile me"])
	}
	if featureStatus["auth refresh"] != "supported" {
		t.Errorf("auth refresh: want supported (refresh token stored), got %q", featureStatus["auth refresh"])
	}
}

func TestDoctorSchemaValidation(t *testing.T) {
	t.Setenv("GOLINK_CLIENT_ID", "client-123")

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := auth.NewMemoryStore()
	sess := validDoctorSession(now)
	if err := store.SaveSession(context.Background(), sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"sub":"urn:li:person:abc123"}`)
	}))
	defer srv.Close()

	_, stdout, _ := executeDoctorCommand(t, []string{"--json", "doctor"}, testDepsOptions{store: store}, srv)

	outputtest.ValidateEnvelopeRoundTrip(t, schemaPath(t), stdout.Bytes())
}

func TestDoctorNotAudited(t *testing.T) {
	// doctor must not produce audit entries.
	sink := audit.NewMemorySink()
	t.Setenv("GOLINK_AUDIT", "on")

	executeTestCommand(t, []string{"--json", "doctor"}, testDepsOptions{auditSink: sink})

	if len(sink.Entries()) != 0 {
		t.Fatalf("doctor must not be audited, got %d entries", len(sink.Entries()))
	}
}

func TestWriteDoctorText(t *testing.T) {
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	data := outputtest.DoctorData{
		APIVersion: "2026-04-17",
		Environment: outputtest.DoctorEnvironment{
			GOLINKClientID:   true,
			GOLINKAPIVersion: "2026-04-17",
			ConfigPath:       "/tmp/config.yaml",
		},
		Session: outputtest.DoctorSession{
			Profile:          "default",
			Authenticated:    true,
			ExpiresAt:        now.Add(time.Hour).Format(time.RFC3339),
			ExpiresInHours:   1,
			RefreshAvailable: true,
			RefreshExpiresAt: now.Add(48 * time.Hour).Format(time.RFC3339),
			RefreshInDays:    2,
			ConnectedAt:      now.Add(-24 * time.Hour).Format(time.RFC3339),
			Scopes:           []string{"openid", "profile"},
			AuthFlow:         "pkce",
		},
		Probe: outputtest.DoctorProbe{
			Attempted: true,
			Status:    200,
			Member:    "urn:li:person:abc123",
		},
		Features: []outputtest.DoctorFeature{
			{Command: "post create", Status: "supported"},
			{Command: "post delete", Status: "unsupported", Reason: "missing scope"},
		},
		Audit: outputtest.DoctorAudit{
			Path:    "/tmp/audit.log",
			Enabled: true,
		},
		Warnings: []string{"access token expires soon"},
		Errors:   []string{"probe timeout"},
		Health:   "error",
	}

	if err := writeDoctorText(&out, data); err != nil {
		t.Fatalf("writeDoctorText: %v", err)
	}
	got := out.String()
	if got == "" {
		t.Fatal("expected rendered doctor text output")
	}
	for _, needle := range []string{
		"golink doctor — diagnostics",
		"Session (profile: default)",
		"LinkedIn probe",
		"Feature support",
		"Warnings",
		"Errors",
		"Health: error",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing section %q", needle)
		}
	}
}
