package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

func newCoverageApp(t *testing.T) *app {
	t.Helper()

	return &app{
		deps: normalizeDependencies(Dependencies{
			Stdout:           &bytes.Buffer{},
			Stderr:           &bytes.Buffer{},
			Now:              fixedNow,
			SessionStore:     authenticatedStore(t),
			IdempotencyStore: idempotency.NewMemoryStore(),
			ApprovalStore:    approval.NewMemoryStore(),
			AuditSink:        audit.NewMemorySink(),
		}),
		settings: config.Settings{
			Profile:   "default",
			Transport: "official",
			Output:    output.ModeJSON,
			JSON:      true,
			Timeout:   30 * time.Second,
			Audit:     true,
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestDefaultTransportFactoryBranches(t *testing.T) {
	deps := normalizeDependencies(Dependencies{HTTPClient: httpClientOrDefault(nil), Now: fixedNow})
	factory := defaultTransportFactory(deps)
	session := auth.Session{AccessToken: "token", MemberURN: "urn:li:person:abc123"}

	for _, tc := range []struct {
		name      string
		transport string
		wantName  string
	}{
		{name: "official", transport: "official", wantName: "official"},
		{name: "auto uses official", transport: "auto", wantName: "official"},
		{name: "unofficial noop", transport: "unofficial", wantName: "unofficial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport, err := factory(context.Background(), config.Settings{
				Transport:  tc.transport,
				APIVersion: "202604",
			}, session, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatalf("factory error: %v", err)
			}
			if got := transport.Name(); got != tc.wantName {
				t.Fatalf("transport name = %q, want %q", got, tc.wantName)
			}
		})
	}
}

func TestMapTransportErrorMappings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		err      error
		wantExit int
		wantCode string
	}{
		{name: "unauthorized", err: &api.Error{Status: 401, Message: "bad token"}, wantExit: 4, wantCode: string(output.ErrorCodeUnauthorized)},
		{name: "forbidden", err: &api.Error{Status: 403, Message: "forbidden"}, wantExit: 4, wantCode: string(output.ErrorCodeForbidden)},
		{name: "not found", err: &api.Error{Status: 404, Message: "missing"}, wantExit: 5, wantCode: string(output.ErrorCodeNotFound)},
		{name: "validation", err: &api.Error{Status: 422, Message: "invalid"}, wantExit: 2, wantCode: string(output.ErrorCodeValidation)},
		{name: "rate limited", err: &api.Error{Status: 429, Message: "retry later"}, wantExit: 5, wantCode: string(output.ErrorCodeRateLimited)},
		{name: "server", err: &api.Error{Status: 503, Message: "down"}, wantExit: 5, wantCode: string(output.ErrorCodeTransport)},
		{name: "unknown", err: &api.Error{Status: 418, Message: "teapot"}, wantExit: 5, wantCode: string(output.ErrorCodeTransport)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newCoverageApp(t)
			cmd := &cobra.Command{Use: "post"}
			err := a.mapTransportError(cmd, "post create", tc.err)
			var failure *commandFailure
			if !errors.As(err, &failure) {
				t.Fatalf("expected commandFailure, got %T", err)
			}
			if failure.exitCode != tc.wantExit {
				t.Fatalf("exit = %d, want %d", failure.exitCode, tc.wantExit)
			}
			if failure.errCode != tc.wantCode {
				t.Fatalf("code = %q, want %q", failure.errCode, tc.wantCode)
			}
		})
	}
}

func TestMapTransportErrorFeatureUnavailableWritesUnsupported(t *testing.T) {
	a := newCoverageApp(t)
	stdout := &bytes.Buffer{}
	a.deps.Stdout = stdout
	cmd := &cobra.Command{Use: "search"}

	err := a.mapTransportError(cmd, "search people", &api.ErrFeatureUnavailable{
		Feature:            "search people",
		Reason:             "not available",
		SuggestedTransport: "unofficial",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Feature           string `json:"feature"`
			SuggestedFallback string `json:"suggested_fallback"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal unsupported envelope: %v", err)
	}
	if payload.Status != "unsupported" {
		t.Fatalf("status = %q, want unsupported", payload.Status)
	}
	if payload.Data.SuggestedFallback != "--transport=unofficial" {
		t.Fatalf("fallback = %q", payload.Data.SuggestedFallback)
	}
}

func TestWriteSuccessFromCacheSetsFromCache(t *testing.T) {
	a := newCoverageApp(t)
	stdout := &bytes.Buffer{}
	a.deps.Stdout = stdout

	if err := a.writeSuccessFromCache(&cobra.Command{Use: "version"}, output.VersionData{Version: "1.0.0"}, "cached"); err != nil {
		t.Fatalf("writeSuccessFromCache: %v", err)
	}

	var payload struct {
		FromCache bool `json:"from_cache"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !payload.FromCache {
		t.Fatal("expected from_cache=true")
	}
}

func TestIdempotencyCheckHitAndMismatch(t *testing.T) {
	a := newCoverageApp(t)
	store := idempotency.NewMemoryStore()
	a.deps.IdempotencyStore = store
	entry := idempotency.Entry{
		TS:        time.Now().UTC(),
		Key:       "same-key",
		Command:   "post create",
		CommandID: "cmd_123",
	}
	if err := store.Record(context.Background(), entry); err != nil {
		t.Fatalf("record: %v", err)
	}

	hitEntry, hit, err := a.idempotencyCheck(&cobra.Command{Use: "post"}, "same-key", "post create")
	if err != nil {
		t.Fatalf("unexpected hit error: %v", err)
	}
	if !hit || hitEntry.CommandID != "cmd_123" {
		t.Fatalf("unexpected hit result: hit=%v entry=%+v", hit, hitEntry)
	}

	_, _, err = a.idempotencyCheck(&cobra.Command{Use: "post"}, "same-key", "comment add")
	var failure *commandFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected validation failure, got %T", err)
	}
	if failure.exitCode != 2 {
		t.Fatalf("exit = %d, want 2", failure.exitCode)
	}
}

func TestBatchRunnerPendingApprovalAndCachedResult(t *testing.T) {
	a := newCoverageApp(t)
	stdout := &bytes.Buffer{}
	a.deps.Stdout = stdout
	progressPath := filepath.Join(t.TempDir(), "progress.jsonl")

	runner := &batchRunner{
		a:            a,
		cmd:          &cobra.Command{Use: "batch"},
		transport:    &fakeTransport{name: "official"},
		istore:       a.deps.IdempotencyStore,
		out:          stdout,
		progressPath: progressPath,
	}

	if err := runner.emitPendingApproval(context.Background(), 1, batchOp{
		Command:         "post create",
		IdempotencyKey:  "ikey-1",
		RequireApproval: true,
		Args: map[string]any{
			"text":       "hello",
			"visibility": "PUBLIC",
			"author_urn": "urn:li:organization:123",
		},
	}); err != nil {
		t.Fatalf("emitPendingApproval: %v", err)
	}

	items, err := a.deps.ApprovalStore.List(context.Background())
	if err != nil {
		t.Fatalf("approval list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 approval item, got %d", len(items))
	}

	cached := idempotency.Entry{
		CommandID:  "cmd_cached",
		HTTPStatus: 201,
		Result:     json.RawMessage(`{"id":"urn:li:share:42"}`),
	}
	if err := runner.emitCachedResult(2, batchOp{Command: "post create", IdempotencyKey: "ikey-2"}, cached); err != nil {
		t.Fatalf("emitCachedResult: %v", err)
	}

	progressBytes, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if !bytes.Contains(progressBytes, []byte(`"command_id":"cmd_cached"`)) {
		t.Fatalf("progress file missing cached command id: %s", string(progressBytes))
	}
}

func httpClientOrDefault(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}
