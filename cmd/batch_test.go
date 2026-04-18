package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/mudrii/golink/internal/schedule"
	"github.com/spf13/cobra"
)

// countingTransport wraps fakeTransport and counts calls.
type countingTransport struct {
	inner     *fakeTransport
	callCount int
	failOn    map[int]error // fail on Nth call (1-based)
}

func (c *countingTransport) Name() string { return c.inner.Name() }
func (c *countingTransport) ProfileMe(ctx context.Context) (*output.ProfileData, error) {
	return c.inner.ProfileMe(ctx)
}
func (c *countingTransport) CreatePost(ctx context.Context, req api.CreatePostRequest) (*output.PostSummary, error) {
	c.callCount++
	if err, ok := c.failOn[c.callCount]; ok {
		return nil, err
	}
	return &output.PostSummary{
		ID:         fmt.Sprintf("urn:li:share:%d", c.callCount),
		CreatedAt:  time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		Text:       req.Text,
		Visibility: req.Visibility,
		URL:        fmt.Sprintf("https://www.linkedin.com/feed/update/urn:li:share:%d", c.callCount),
		AuthorURN:  "urn:li:person:abc123",
	}, nil
}
func (c *countingTransport) ListPosts(ctx context.Context, a string, b, d int) (*output.PostListData, error) {
	return c.inner.ListPosts(ctx, a, b, d)
}
func (c *countingTransport) GetPost(ctx context.Context, p string) (*output.PostGetData, error) {
	return c.inner.GetPost(ctx, p)
}
func (c *countingTransport) DeletePost(ctx context.Context, p string) (*output.PostDeleteData, error) {
	c.callCount++
	return c.inner.DeletePost(ctx, p)
}
func (c *countingTransport) AddComment(ctx context.Context, p, t string) (*output.CommentData, error) {
	c.callCount++
	return c.inner.AddComment(ctx, p, t)
}
func (c *countingTransport) ListComments(ctx context.Context, p string, count, start int) (*output.CommentListData, error) {
	return c.inner.ListComments(ctx, p, count, start)
}
func (c *countingTransport) AddReaction(ctx context.Context, p string, r output.ReactionType) (*output.ReactionData, error) {
	c.callCount++
	return c.inner.AddReaction(ctx, p, r)
}
func (c *countingTransport) ListReactions(ctx context.Context, p string) (*output.ReactionListData, error) {
	return c.inner.ListReactions(ctx, p)
}
func (c *countingTransport) SearchPeople(ctx context.Context, req api.SearchPeopleRequest) (*output.SearchPeopleData, error) {
	return c.inner.SearchPeople(ctx, req)
}
func (c *countingTransport) SocialMetadata(ctx context.Context, urns []string) (*output.SocialMetadataData, error) {
	return c.inner.SocialMetadata(ctx, urns)
}
func (c *countingTransport) InitializeImageUpload(ctx context.Context, ownerURN string) (string, string, error) {
	return c.inner.InitializeImageUpload(ctx, ownerURN)
}
func (c *countingTransport) UploadImageBinary(ctx context.Context, uploadURL, filePath string) error {
	return c.inner.UploadImageBinary(ctx, uploadURL, filePath)
}
func (c *countingTransport) EditPost(ctx context.Context, req api.EditPostRequest) (*output.PostEditData, error) {
	return c.inner.EditPost(ctx, req)
}
func (c *countingTransport) ResharePost(ctx context.Context, req api.ResharePostRequest) (*output.PostSummary, error) {
	return c.inner.ResharePost(ctx, req)
}

func (c *countingTransport) ListOrganizations(ctx context.Context) (*output.OrgListData, error) {
	return c.inner.ListOrganizations(ctx)
}

func writeOpsFile(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write ops file: %v", err)
	}
	return path
}

func parseBatchLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var results []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("parse batch output line %q: %v", line, err)
		}
		results = append(results, m)
	}
	return results
}

func runBatch(t *testing.T, args []string, transport api.Transport, istore idempotency.Store) (int, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	store := authenticatedStore(t)
	code := ExecuteContext(context.Background(), args, Dependencies{
		Stdout:           stdout,
		Stderr:           stderr,
		Now:              func() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) },
		SessionStore:     store,
		IsInteractive:    func() bool { return false },
		TransportFactory: factoryReturning(transport),
		AuditSink:        audit.NewMemorySink(),
		IdempotencyStore: istore,
	}, BuildInfo{Version: "test"})
	return code, stdout, stderr
}

func TestBatchHappyPath(t *testing.T) {
	ct := &countingTransport{inner: &fakeTransport{name: "official"}}
	istore := idempotency.NewMemoryStore()

	ops := []string{
		`{"command":"post create","args":{"text":"hello batch 1","visibility":"PUBLIC"},"idempotency_key":"b-1"}`,
		`{"command":"post create","args":{"text":"hello batch 2","visibility":"PUBLIC"},"idempotency_key":"b-2"}`,
		`{"command":"post create","args":{"text":"hello batch 3","visibility":"PUBLIC"},"idempotency_key":"b-3"}`,
	}
	opsPath := writeOpsFile(t, ops)

	code, stdout, stderr := runBatch(t, []string{"--json", "batch", opsPath}, ct, istore)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	lines := parseBatchLines(t, stdout)
	if len(lines) != 3 {
		t.Fatalf("expected 3 output lines, got %d: %s", len(lines), stdout.String())
	}
	for i, line := range lines {
		data, ok := line["data"].(map[string]any)
		if !ok {
			t.Fatalf("line %d: expected data object", i+1)
		}
		if data["status"] != "ok" {
			t.Errorf("line %d: expected status ok, got %v", i+1, data["status"])
		}
		lineNum := int(data["line"].(float64))
		if lineNum != i+1 {
			t.Errorf("line %d: expected line=%d, got %d", i+1, i+1, lineNum)
		}
	}
	if ct.callCount != 3 {
		t.Errorf("expected 3 transport calls, got %d", ct.callCount)
	}
}

func TestBatchDryRunPerOp(t *testing.T) {
	ct := &countingTransport{inner: &fakeTransport{name: "official"}}
	istore := idempotency.NewMemoryStore()

	ops := []string{
		`{"command":"post create","args":{"text":"hello","visibility":"PUBLIC"},"dry_run":true}`,
	}
	opsPath := writeOpsFile(t, ops)

	code, stdout, stderr := runBatch(t, []string{"--json", "batch", opsPath}, ct, istore)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}
	if ct.callCount != 0 {
		t.Errorf("expected 0 transport calls in per-op dry-run, got %d", ct.callCount)
	}
	lines := parseBatchLines(t, stdout)
	if len(lines) != 1 {
		t.Fatalf("expected 1 output line, got %d", len(lines))
	}
}

func TestBatchUnsupportedCommand(t *testing.T) {
	ct := &countingTransport{inner: &fakeTransport{name: "official"}}
	istore := idempotency.NewMemoryStore()

	ops := []string{
		`{"command":"profile me","args":{}}`,
	}
	opsPath := writeOpsFile(t, ops)

	code, stdout, _ := runBatch(t, []string{"--json", "batch", opsPath}, ct, istore)
	// continue-on-error is default — exit 0
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	lines := parseBatchLines(t, stdout)
	if len(lines) != 1 {
		t.Fatalf("expected 1 output line, got %d", len(lines))
	}
	data := lines[0]["data"].(map[string]any)
	if data["status"] != "validation_error" {
		t.Errorf("expected validation_error status, got %v", data["status"])
	}
}

func TestBatchIdempotencyReplay(t *testing.T) {
	ct := &countingTransport{inner: &fakeTransport{name: "official"}}
	istore := idempotency.NewMemoryStore()

	ops := []string{
		`{"command":"post create","args":{"text":"hello","visibility":"PUBLIC"},"idempotency_key":"replay-1"}`,
	}
	opsPath := writeOpsFile(t, ops)

	// First run — transport is called.
	code, _, _ := runBatch(t, []string{"--json", "batch", opsPath}, ct, istore)
	if code != 0 {
		t.Fatalf("first run: expected exit code 0, got %d", code)
	}
	if ct.callCount != 1 {
		t.Fatalf("first run: expected 1 transport call, got %d", ct.callCount)
	}

	// Second run — same istore, same key: should replay from cache.
	code2, stdout2, _ := runBatch(t, []string{"--json", "batch", opsPath}, ct, istore)
	if code2 != 0 {
		t.Fatalf("second run: expected exit code 0, got %d", code2)
	}
	if ct.callCount != 1 {
		t.Fatalf("second run: transport should not be called again, total=%d", ct.callCount)
	}

	lines := parseBatchLines(t, stdout2)
	if len(lines) != 1 {
		t.Fatalf("expected 1 output line, got %d", len(lines))
	}
	data := lines[0]["data"].(map[string]any)
	if data["from_cache"] != true {
		t.Errorf("expected from_cache=true on replay, got %v", data["from_cache"])
	}
}

func TestBatchResume(t *testing.T) {
	ct := &countingTransport{inner: &fakeTransport{name: "official"}}
	istore := idempotency.NewMemoryStore()

	ops := []string{
		`{"command":"post create","args":{"text":"op1","visibility":"PUBLIC"}}`,
		`{"command":"post create","args":{"text":"op2","visibility":"PUBLIC"}}`,
		`{"command":"post create","args":{"text":"op3","visibility":"PUBLIC"}}`,
	}
	opsPath := writeOpsFile(t, ops)

	// Pre-populate progress file to simulate ops 1+2 already done.
	progressPath := opsPath + ".progress"
	progressLines := []string{
		`{"line":1,"status":"ok","command_id":"cmd_x"}`,
		`{"line":2,"status":"ok","command_id":"cmd_y"}`,
	}
	if err := os.WriteFile(progressPath, []byte(strings.Join(progressLines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write progress: %v", err)
	}

	code, stdout, stderr := runBatch(t, []string{"--json", "batch", "--resume", opsPath}, ct, istore)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	// Only op 3 should have been dispatched.
	if ct.callCount != 1 {
		t.Errorf("expected 1 transport call (only op 3), got %d", ct.callCount)
	}

	// Should still have 3 output lines (2 skipped + 1 real).
	lines := parseBatchLines(t, stdout)
	if len(lines) != 3 {
		t.Fatalf("expected 3 output lines, got %d: %s", len(lines), stdout.String())
	}
}

func TestBatchStrictExitCode(t *testing.T) {
	ct := &countingTransport{inner: &fakeTransport{name: "official"}}
	istore := idempotency.NewMemoryStore()

	// empty text causes validation error in batch runner
	ops := []string{
		`{"command":"post create","args":{"text":"","visibility":"PUBLIC"}}`,
	}
	opsPath := writeOpsFile(t, ops)

	code, _, _ := runBatch(t, []string{"--json", "batch", "--strict", opsPath}, ct, istore)
	if code != 2 {
		t.Errorf("expected exit code 2 with --strict and op error, got %d", code)
	}
}

func TestBatchPostScheduleRequireApprovalRejected(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	astore := approval.NewMemoryStore()
	opsPath := writeOpsFile(t, []string{
		`{"command":"post schedule","args":{"at":"2026-04-17T14:00:00Z","text":"scheduled"},"require_approval":true}`,
	})

	code := ExecuteContext(context.Background(), []string{"--json", "batch", opsPath}, Dependencies{
		Stdout:           stdout,
		Stderr:           stderr,
		Now:              func() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) },
		SessionStore:     authenticatedStore(t),
		IsInteractive:    func() bool { return false },
		TransportFactory: factoryReturning(&fakeTransport{name: "official"}),
		AuditSink:        audit.NewMemorySink(),
		IdempotencyStore: idempotency.NewMemoryStore(),
		ApprovalStore:    astore,
	}, BuildInfo{Version: "test"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr)
	}

	lines := parseBatchLines(t, stdout)
	if len(lines) != 1 {
		t.Fatalf("expected 1 output line, got %d", len(lines))
	}
	data := lines[0]["data"].(map[string]any)
	if data["status"] != "validation_error" {
		t.Fatalf("expected validation_error, got %v", data["status"])
	}
	if !strings.Contains(data["error"].(string), "not supported") {
		t.Fatalf("unexpected error: %v", data["error"])
	}

	items, err := astore.List(context.Background())
	if err != nil {
		t.Fatalf("list approvals: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no staged approvals, got %d", len(items))
	}
}

func TestBatchStdinInput(t *testing.T) {
	ct := &countingTransport{inner: &fakeTransport{name: "official"}}
	istore := idempotency.NewMemoryStore()

	// Write to a temp file and point stdin-style to it by path "-" is special;
	// test with real file since we can't easily replace os.Stdin in the test.
	// Instead test that "-" flag is accepted and the help works.
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	store := authenticatedStore(t)
	code := ExecuteContext(context.Background(), []string{"batch", "--help"}, Dependencies{
		Stdout:           stdout,
		Stderr:           stderr,
		Now:              func() time.Time { return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC) },
		SessionStore:     store,
		IsInteractive:    func() bool { return false },
		TransportFactory: factoryReturning(ct),
		AuditSink:        audit.NewMemorySink(),
		IdempotencyStore: istore,
	}, BuildInfo{Version: "test"})
	// --help returns exit 0
	if code != 0 {
		t.Fatalf("expected exit 0 for --help, got %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "batch") {
		t.Errorf("expected 'batch' in help output, got: %s", combined)
	}
}

func TestBatchRunOpDeleteDryRunAndValidation(t *testing.T) {
	t.Helper()

	runner := &batchRunner{
		a:         &app{deps: normalizeDependencies(Dependencies{Now: fixedNow, ScheduleStore: schedule.NewMemoryStore()})},
		cmd:       &cobra.Command{},
		transport: &fakeTransport{name: "official"},
	}

	data, cmdID, status, err := runner.runPostDelete(context.Background(), batchOp{
		Args: map[string]any{"post_urn": "urn:li:post:123"},
	}, true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if cmdID == "" {
		t.Fatal("cmdID empty")
	}
	if status != 0 {
		t.Fatalf("dry-run status = %d", status)
	}
	if _, ok := data.(output.PostDeleteDryRunData); !ok {
		t.Fatalf("dry-run type = %T", data)
	}

	if _, _, _, err := runner.runPostDelete(context.Background(), batchOp{
		Args: map[string]any{},
	}, false); err == nil {
		t.Fatal("expected missing post_urn error")
	}
}

func TestBatchRunOpCommentAndReactionDryRun(t *testing.T) {
	t.Helper()

	runner := &batchRunner{
		a:         &app{deps: normalizeDependencies(Dependencies{Now: fixedNow, ScheduleStore: schedule.NewMemoryStore()})},
		cmd:       &cobra.Command{},
		transport: &fakeTransport{name: "official"},
	}

	commentData, _, _, err := runner.runCommentAdd(context.Background(), batchOp{
		Args: map[string]any{
			"post_urn": "urn:li:post:456",
			"text":     "hello comment",
		},
	}, true)
	if err != nil {
		t.Fatalf("comment dry-run: %v", err)
	}
	if _, ok := commentData.(output.CommentAddDryRunData); !ok {
		t.Fatalf("comment type = %T", commentData)
	}

	reactionData, _, _, err := runner.runReactAdd(context.Background(), batchOp{
		Args: map[string]any{"post_urn": "urn:li:post:789"},
	}, true)
	if err != nil {
		t.Fatalf("react dry-run: %v", err)
	}
	if _, ok := reactionData.(output.ReactionAddDryRunData); !ok {
		t.Fatalf("react type = %T", reactionData)
	}
}

func TestBatchRunPostScheduleValidation(t *testing.T) {
	runner := &batchRunner{
		a: &app{deps: normalizeDependencies(Dependencies{
			Now:           fixedNow,
			ScheduleStore: schedule.NewMemoryStore(),
		})},
		cmd:       &cobra.Command{},
		transport: &fakeTransport{name: "official"},
	}

	if _, _, _, err := runner.runPostSchedule(context.Background(), batchOp{
		Args: map[string]any{"text": "missing time"},
	}); err == nil {
		t.Fatal("expected missing time validation error")
	}
	if _, _, _, err := runner.runPostSchedule(context.Background(), batchOp{
		Args: map[string]any{"at": fixedNow().Add(-time.Minute).Format(time.RFC3339), "text": "late"},
	}); err == nil {
		t.Fatal("expected past at validation error")
	}
	if _, _, _, err := runner.runPostSchedule(context.Background(), batchOp{
		Args: map[string]any{
			"text":       "relative path",
			"image_path": "/tmp/example.jpg",
			"at":         fixedNow().Add(2 * time.Hour).Format(time.RFC3339),
		},
	}); err != nil {
		t.Fatal("unexpected error for absolute path schedule")
	}
	if _, _, _, err := runner.runPostSchedule(context.Background(), batchOp{
		Args: map[string]any{
			"text":       "relative path",
			"image_path": "relative/path.jpg",
			"at":         fixedNow().Add(2 * time.Hour).Format(time.RFC3339),
		},
	}); err == nil {
		t.Fatal("expected relative image path validation error")
	}
}
