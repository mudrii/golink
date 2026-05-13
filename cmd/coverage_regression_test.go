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
	"strings"
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
			transport, err := factory(t.Context(), config.Settings{
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

func TestNewCommandIDFallbackWhenRandomFails(t *testing.T) {
	oldSeq := commandIDFallbackSeq
	commandIDFallbackSeq = 0
	t.Cleanup(func() {
		commandIDFallbackSeq = oldSeq
	})

	deps := normalizeDependencies(Dependencies{
		RandRead: func([]byte) (int, error) {
			return 0, errors.New("entropy unavailable")
		},
	})
	a := newApp(BuildInfo{}, deps, nil)

	got := a.newCommandID("post create", fixedNow())
	if got != "cmd_post_create_1776427200_000001" {
		t.Fatalf("fallback command id = %q", got)
	}
}

func TestExecuteWrapperUsesProcessArgsAndStdout(t *testing.T) {
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	defer func() {
		os.Args = oldArgs
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		_ = stdoutR.Close()
		_ = stderrR.Close()
	}()

	os.Args = []string{"golink", "--json", "version"}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	code := Execute(t.Context(), BuildInfo{
		Version:   "test",
		Commit:    "abc123",
		BuildDate: "2026-04-16T12:00:00Z",
	})
	_ = stdoutW.Close()
	_ = stderrW.Close()

	stdout, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	stderr, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if code != 0 {
		t.Fatalf("Execute code = %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(string(stdout), `"command":"version"`) {
		t.Fatalf("stdout = %s, want version envelope", stdout)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %s, want empty", stderr)
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

func TestAppWritersRenderNonJSONOutputModes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  string
		write func(*app, *cobra.Command) error
		want  string
	}{
		{
			name: "cache compact",
			mode: output.ModeCompact,
			write: func(a *app, cmd *cobra.Command) error {
				return a.writeSuccessFromCache(cmd, output.VersionData{Version: "1.0.0"}, "cached")
			},
			want: `"status":"ok"`,
		},
		{
			name: "cache jsonl",
			mode: output.ModeJSONL,
			write: func(a *app, cmd *cobra.Command) error {
				return a.writeSuccessFromCache(cmd, output.VersionData{Version: "1.0.0"}, "cached")
			},
			want: `"version":"1.0.0"`,
		},
		{
			name: "cache text",
			mode: output.ModeText,
			write: func(a *app, cmd *cobra.Command) error {
				return a.writeSuccessFromCache(cmd, output.VersionData{Version: "1.0.0"}, "cached")
			},
			want: "cached",
		},
		{
			name: "cache table",
			mode: output.ModeTable,
			write: func(a *app, cmd *cobra.Command) error {
				return a.writeSuccessFromCache(cmd, output.VersionData{Version: "1.0.0"}, "cached")
			},
			want: "cached",
		},
		{
			name: "dry run compact",
			mode: output.ModeCompact,
			write: func(a *app, cmd *cobra.Command) error {
				return a.writeDryRun(cmd, output.PostCreateDryRunData{}, "dry run")
			},
			want: `"mode":"dry_run"`,
		},
		{
			name: "unsupported compact",
			mode: output.ModeCompact,
			write: func(a *app, cmd *cobra.Command) error {
				return a.writeUnsupported(cmd, output.UnsupportedPayload{Feature: "search people"}, "unsupported")
			},
			want: `"status":"unsupported"`,
		},
		{
			name: "unsupported text",
			mode: output.ModeText,
			write: func(a *app, cmd *cobra.Command) error {
				return a.writeUnsupported(cmd, output.UnsupportedPayload{Feature: "search people"}, "unsupported")
			},
			want: "unsupported",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newCoverageApp(t)
			a.settings.Output = tc.mode
			a.settings.JSON = tc.mode == output.ModeJSON
			stdout := &bytes.Buffer{}
			a.deps.Stdout = stdout
			if err := tc.write(a, &cobra.Command{Use: "version"}); err != nil {
				t.Fatalf("write: %v", err)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("output = %q, want %q", stdout.String(), tc.want)
			}
		})
	}
}

func TestCommandFailureErrorAndRenderingBranches(t *testing.T) {
	failure := &commandFailure{text: "plain failure", exitCode: 7}
	if failure.Error() != "plain failure" {
		t.Fatalf("Error() = %q", failure.Error())
	}

	var stderr bytes.Buffer
	code := writeCommandFailure(&stderr, failure)
	if code != 7 || !strings.Contains(stderr.String(), "plain failure") {
		t.Fatalf("plain failure render: code=%d stderr=%q", code, stderr.String())
	}

	stderr.Reset()
	payload := output.ValidationError(output.EnvelopeMeta{Command: "post", Status: output.StatusValidation}, "bad input", "details")
	code = writeCommandFailure(&stderr, &commandFailure{
		outputMode: output.ModeCompact,
		payload:    payload,
		text:       "fallback detail",
		exitCode:   2,
	})
	if code != 2 || !strings.Contains(stderr.String(), "bad input") {
		t.Fatalf("compact failure render: code=%d stderr=%q", code, stderr.String())
	}

	stderr.Reset()
	code = writeCommandFailure(&stderr, &commandFailure{
		outputMode: output.ModeJSONL,
		payload:    struct{ Message string }{Message: "opaque"},
		text:       "opaque failure",
		exitCode:   5,
	})
	if code != 5 || !strings.Contains(stderr.String(), "opaque failure") {
		t.Fatalf("opaque failure render: code=%d stderr=%q", code, stderr.String())
	}
}

func TestWriteJSONErrorBrokenWriterAndRenderedEmptyFailure(t *testing.T) {
	code := writeJSONError(failingWriter{}, output.ValidationError(output.EnvelopeMeta{
		Command: "post create",
		Status:  output.StatusValidation,
	}, "bad input", ""), 2)
	if code != 1 {
		t.Fatalf("writeJSONError code = %d, want 1", code)
	}

	if err := writeRenderedFailure(&bytes.Buffer{}, output.ModeJSONL, &commandFailure{exitCode: 2}); err != nil {
		t.Fatalf("empty rendered failure error = %v", err)
	}
}

func TestGenericCommandErrorRenderingBranches(t *testing.T) {
	root := &cobra.Command{Use: "golink"}
	err := errors.New("unknown command")

	jsonApp := newCoverageApp(t)
	jsonApp.settings.Output = output.ModeJSON
	var stderr bytes.Buffer
	code := jsonApp.writeGenericCommandError(root, []string{"bad"}, &stderr, err)
	if code != 2 || !strings.Contains(stderr.String(), `"validation_error"`) {
		t.Fatalf("json generic error: code=%d stderr=%q", code, stderr.String())
	}

	compactApp := newCoverageApp(t)
	compactApp.settings.Output = output.ModeCompact
	stderr.Reset()
	code = compactApp.writeGenericCommandError(root, []string{"bad"}, &stderr, err)
	if code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("compact generic error: code=%d stderr=%q", code, stderr.String())
	}

	textApp := newCoverageApp(t)
	textApp.settings.Output = output.ModeText
	stderr.Reset()
	code = textApp.writeGenericCommandError(root, []string{"bad"}, &stderr, err)
	if code != 1 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("text generic error: code=%d stderr=%q", code, stderr.String())
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
	if err := store.Record(t.Context(), entry); err != nil {
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

func TestPostCacheHelpersReplayStoredResults(t *testing.T) {
	a := newCoverageApp(t)
	store := idempotency.NewMemoryStore()
	a.deps.IdempotencyStore = store
	stdout := &bytes.Buffer{}
	a.deps.Stdout = stdout
	cmd := &cobra.Command{Use: "post"}
	cmd.SetContext(t.Context())

	postCreateBytes, err := json.Marshal(output.PostCreateData{
		PostSummary: output.PostSummary{ID: "urn:li:share:create", URL: "https://example.com/create"},
	})
	if err != nil {
		t.Fatalf("marshal create result: %v", err)
	}
	if err := store.Record(t.Context(), idempotency.Entry{
		TS:         time.Now().UTC(),
		Key:        "create-key",
		Command:    "post create",
		HTTPStatus: 201,
		Result:     postCreateBytes,
	}); err != nil {
		t.Fatalf("record create: %v", err)
	}
	handled, err := a.writeCachedPostCreate(cmd, "cmd_create", "create-key")
	if err != nil || !handled {
		t.Fatalf("writeCachedPostCreate handled=%v err=%v", handled, err)
	}
	if !strings.Contains(stdout.String(), `"from_cache":true`) {
		t.Fatalf("create cache output missing from_cache: %s", stdout.String())
	}

	stdout.Reset()
	postEditBytes, err := json.Marshal(output.PostEditData{
		PostSummary: output.PostSummary{ID: "urn:li:share:edit", URL: "https://example.com/edit"},
	})
	if err != nil {
		t.Fatalf("marshal edit result: %v", err)
	}
	if err := store.Record(t.Context(), idempotency.Entry{
		TS:         time.Now().UTC(),
		Key:        "edit-key",
		Command:    "post edit",
		HTTPStatus: 204,
		Result:     postEditBytes,
	}); err != nil {
		t.Fatalf("record edit: %v", err)
	}
	handled, err = a.writeCachedPostEdit(cmd, "cmd_edit", "edit-key")
	if err != nil || !handled {
		t.Fatalf("writeCachedPostEdit handled=%v err=%v", handled, err)
	}
	if !strings.Contains(stdout.String(), `"from_cache":true`) {
		t.Fatalf("edit cache output missing from_cache: %s", stdout.String())
	}
}

func TestApprovalRunTransportResolutionAuditsAuthFailure(t *testing.T) {
	a := newCoverageApp(t)
	a.deps.SessionStore = auth.NewMemoryStore()
	sink := audit.NewMemorySink()
	a.deps.AuditSink = sink
	cmd := &cobra.Command{Use: "approval run"}
	cmd.SetContext(t.Context())

	_, _, err := a.resolveApprovedRunTransport(cmd, "cmd_missing_session", approval.Entry{Profile: "missing", Transport: "official"})
	var failure *commandFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected commandFailure, got %T %v", err, err)
	}
	if failure.exitCode != 4 {
		t.Fatalf("exit = %d, want 4", failure.exitCode)
	}
	entries := sink.Entries()
	if len(entries) != 1 || entries[0].Status != "error" {
		t.Fatalf("audit entries = %+v, want one error", entries)
	}
}

func TestIdempotencyRecordLogsAndContinuesOnStoreError(t *testing.T) {
	var logs bytes.Buffer
	a := newCoverageApp(t)
	a.logger = slog.New(slog.NewTextHandler(&logs, nil))
	a.deps.IdempotencyStore = failingIdempotencyRecordStore{err: errors.New("disk full")}

	a.idempotencyRecord(t.Context(), idempotency.Entry{
		TS:      time.Now().UTC(),
		Key:     "same-key",
		Command: "post create",
		Status:  "ok",
	})

	if !strings.Contains(logs.String(), "idempotency record failed") {
		t.Fatalf("expected warning log, got %q", logs.String())
	}
	code, stdout, stderr := executeTestCommand(
		t,
		[]string{"--json", "--idempotency-key=idem-write-fails", "post", "create", "--text", "records best effort"},
		testDepsOptions{
			store:            authenticatedStore(t),
			transportFactory: factoryReturning(&fakeTransport{name: "official"}),
			idempotencyStore: failingIdempotencyRecordStore{err: errors.New("disk full")},
		},
	)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr.String(), "idempotency record failed") {
		t.Fatalf("expected warning stderr, got %s", stderr)
	}
}

func TestRecordApprovedIdempotencyRejectsUnmarshalableResult(t *testing.T) {
	a := newCoverageApp(t)
	cmd := &cobra.Command{Use: "approval run"}
	cmd.SetContext(t.Context())

	err := a.recordApprovedIdempotency(cmd, "approval-bad-cache", approval.Entry{
		Command:        "post create",
		IdempotencyKey: "approval-key",
	}, approvedRunResult{data: make(chan int), httpStatus: http.StatusCreated})
	var failure *commandFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected commandFailure, got %T %v", err, err)
	}
	if failure.exitCode != 5 {
		t.Fatalf("exit = %d, want 5", failure.exitCode)
	}

	entries := a.deps.AuditSink.(*audit.MemorySink).Entries()
	if len(entries) != 1 || entries[0].Status != "error" || entries[0].ErrorCode != string(output.ErrorCodeTransport) {
		t.Fatalf("audit entries = %+v, want transport error", entries)
	}
}

type failingIdempotencyRecordStore struct {
	err error
}

func (s failingIdempotencyRecordStore) Lookup(context.Context, string, string) (idempotency.Entry, bool, error) {
	return idempotency.Entry{}, false, nil
}

func (s failingIdempotencyRecordStore) Record(context.Context, idempotency.Entry) error {
	return s.err
}

func (s failingIdempotencyRecordStore) Prune(context.Context, time.Duration, int) error {
	return nil
}

func (s failingIdempotencyRecordStore) Acquire(context.Context, string) (func() error, error) {
	return func() error { return nil }, nil
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
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

	if err := runner.emitPendingApproval(t.Context(), 1, batchOp{
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

	items, err := a.deps.ApprovalStore.List(t.Context())
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
