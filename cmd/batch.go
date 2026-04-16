package cmd

// audit: per-op audit entries fire through each dispatched command; batch itself is not annotated mutating.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/mudrii/golink/internal/schedule"
	"github.com/spf13/cobra"
)

const (
	batchMaxConcurrency = 4
	rateLimitPaceThresh = 10 // pace when remaining drops to or below this
)

// batchOp is one line of the ops JSONL input file.
type batchOp struct {
	Command         string         `json:"command"`
	Args            map[string]any `json:"args"`
	IdempotencyKey  string         `json:"idempotency_key,omitempty"`
	DryRun          *bool          `json:"dry_run,omitempty"`
	RequireApproval bool           `json:"require_approval,omitempty"`
}

// progressEntry is one line of the sidecar progress file.
type progressEntry struct {
	Line           int    `json:"line"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	CommandID      string `json:"command_id,omitempty"`
}

var batchSupportedCommands = map[string]struct{}{
	"post create":   {},
	"post delete":   {},
	"comment add":   {},
	"react add":     {},
	"post schedule": {},
}

func newBatchCommand(a *app) *cobra.Command {
	var (
		failFast        bool
		continueOnError bool
		concurrency     int
		strict          bool
		resume          bool
	)

	cmd := &cobra.Command{
		Use:   "batch <ops.jsonl>",
		Short: "Run many operations from a JSONL file",
		Long: `batch reads a JSONL file (or stdin when the path is -) where each line is:
  {"command":"post create","args":{"text":"hello","visibility":"PUBLIC"},"idempotency_key":"abc"}

Supported commands: post create, post delete, comment add, react add.
Results stream to stdout as JSONL — one envelope per input line.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if concurrency < 1 {
				concurrency = 1
			}
			if concurrency > batchMaxConcurrency {
				concurrency = batchMaxConcurrency
			}

			opsPath := args[0]
			var opsReader io.Reader
			if opsPath == "-" {
				opsReader = os.Stdin
			} else {
				f, err := os.Open(opsPath)
				if err != nil {
					return a.validationFailure(cmd, "cannot open ops file", err.Error())
				}
				defer func() { _ = f.Close() }()
				opsReader = f
			}

			// Load progress sidecar if resume is enabled.
			done := map[int]bool{}
			var progressPath string
			if opsPath != "-" && resume {
				progressPath = opsPath + ".progress"
				done = loadProgress(progressPath)
			}

			// Parse all ops first so we know total count.
			ops, err := parseBatchOps(opsReader)
			if err != nil {
				return a.validationFailure(cmd, "failed to parse ops file", err.Error())
			}

			// Build session + transport once (batch runner uses a single transport).
			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}

			runner := &batchRunner{
				a:               a,
				cmd:             cmd,
				transport:       transport,
				istore:          a.deps.IdempotencyStore,
				out:             a.deps.Stdout,
				progressPath:    progressPath,
				continueOnError: continueOnError || !failFast,
				failFast:        failFast,
			}

			anyError := runner.run(cmd.Context(), ops, done, concurrency)

			if strict && anyError {
				return &commandFailure{
					outputMode: a.settings.Output,
					exitCode:   2,
					text:       "batch completed with one or more op errors (--strict)",
				}
			}
			if failFast && anyError {
				return &commandFailure{
					outputMode: a.settings.Output,
					exitCode:   5,
					text:       "batch aborted on first error (--fail-fast)",
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "stop processing on the first op error")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", true, "continue processing after op errors (default)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "parallel workers (max 4)")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 2 if any op is non-ok (useful for CI)")
	cmd.Flags().BoolVar(&resume, "resume", true, "skip already-completed ops via sidecar progress file")

	return cmd
}

type batchRunner struct {
	a               *app
	cmd             *cobra.Command
	transport       api.Transport
	istore          idempotency.Store
	out             io.Writer
	progressPath    string
	continueOnError bool
	failFast        bool

	outMu sync.Mutex
}

func (r *batchRunner) run(ctx context.Context, ops []batchOp, done map[int]bool, concurrency int) bool {
	type result struct {
		line   int
		anyErr bool
	}

	results := make(chan result, len(ops))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	aborted := false
	var abortMu sync.Mutex

	for i, op := range ops {
		lineNum := i + 1

		abortMu.Lock()
		shouldAbort := aborted
		abortMu.Unlock()
		if shouldAbort {
			break
		}

		if done[lineNum] {
			// Replay from progress: emit a cached-style result.
			r.emitSkipped(lineNum, op)
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(ln int, op batchOp) {
			defer wg.Done()
			defer func() { <-sem }()

			opErr := r.runOp(ctx, ln, op)
			hasErr := opErr != nil

			results <- result{line: ln, anyErr: hasErr}

			if hasErr && r.failFast {
				abortMu.Lock()
				aborted = true
				abortMu.Unlock()
			}

			// Rate-limit pacing.
			r.paceRateLimit(ctx)
		}(lineNum, op)
	}

	wg.Wait()
	close(results)

	anyError := false
	for res := range results {
		if res.anyErr {
			anyError = true
		}
	}
	return anyError
}

// runOp dispatches a single batch op and writes the result envelope to stdout.
func (r *batchRunner) runOp(ctx context.Context, lineNum int, op batchOp) error {
	cmdName := strings.TrimSpace(op.Command)

	if _, ok := batchSupportedCommands[cmdName]; !ok {
		return r.emitValidationError(lineNum, op, fmt.Sprintf("unsupported command %q; supported: post create, post delete, comment add, react add, post schedule", cmdName))
	}

	// Idempotency check.
	if op.IdempotencyKey != "" {
		cached, hit, err := r.istore.Lookup(ctx, op.IdempotencyKey, cmdName)
		if err != nil {
			return r.emitValidationError(lineNum, op, err.Error())
		}
		if hit {
			return r.emitCachedResult(lineNum, op, cached)
		}
	}

	dryRun := r.a.settings.DryRun
	if op.DryRun != nil {
		dryRun = *op.DryRun
	}

	requireApproval := op.RequireApproval
	if requireApproval {
		return r.emitPendingApproval(ctx, lineNum, op)
	}

	var opErr error
	var resultData any
	var cmdID string
	var httpStatus int

	switch cmdName {
	case "post create":
		resultData, cmdID, httpStatus, opErr = r.runPostCreate(ctx, op, dryRun)
	case "post delete":
		resultData, cmdID, httpStatus, opErr = r.runPostDelete(ctx, op, dryRun)
	case "comment add":
		resultData, cmdID, httpStatus, opErr = r.runCommentAdd(ctx, op, dryRun)
	case "react add":
		resultData, cmdID, httpStatus, opErr = r.runReactAdd(ctx, op, dryRun)
	case "post schedule":
		resultData, cmdID, httpStatus, opErr = r.runPostSchedule(ctx, op)
	}

	if opErr != nil {
		return r.emitOpError(lineNum, op, cmdID, opErr)
	}

	// Record idempotency entry on success.
	if op.IdempotencyKey != "" && cmdID != "" {
		resultBytes, _ := json.Marshal(resultData)
		_ = r.istore.Record(ctx, idempotency.Entry{
			TS:         r.a.deps.Now().UTC(),
			Key:        op.IdempotencyKey,
			Command:    cmdName,
			CommandID:  cmdID,
			Status:     "ok",
			HTTPStatus: httpStatus,
			Result:     resultBytes,
		})
	}

	return r.emitSuccess(lineNum, op, cmdID, httpStatus, resultData)
}

func (r *batchRunner) runPostCreate(ctx context.Context, op batchOp, dryRun bool) (any, string, int, error) {
	cmdID := newCommandID("post create", r.a.deps.Now().UTC())
	text := stringArg(op.Args, "text")
	if text == "" {
		return nil, cmdID, 0, fmt.Errorf("missing required arg: text")
	}
	visStr := stringArg(op.Args, "visibility")
	if visStr == "" {
		visStr = "PUBLIC"
	}
	visibility, err := output.ParseVisibility(visStr)
	if err != nil {
		return nil, cmdID, 0, fmt.Errorf("invalid visibility: %w", err)
	}
	if dryRun {
		data := output.PostCreateDryRunData{
			WouldPost: output.PostPayloadPreview{
				Endpoint:   "POST /rest/posts",
				Text:       text,
				Visibility: visibility,
				Media:      stringArg(op.Args, "media"),
			},
			Mode: "dry_run",
		}
		return data, cmdID, 0, nil
	}
	summary, err := r.transport.CreatePost(ctx, api.CreatePostRequest{
		Text:       text,
		Visibility: visibility,
		Media:      stringArg(op.Args, "media"),
		AuthorURN:  stringArg(op.Args, "author_urn"),
	})
	if err != nil {
		return nil, cmdID, 0, err
	}
	return output.PostCreateData{PostSummary: *summary}, cmdID, 201, nil
}

func (r *batchRunner) runPostDelete(ctx context.Context, op batchOp, dryRun bool) (any, string, int, error) {
	cmdID := newCommandID("post delete", r.a.deps.Now().UTC())
	postURN := stringArg(op.Args, "post_urn")
	if postURN == "" {
		return nil, cmdID, 0, fmt.Errorf("missing required arg: post_urn")
	}
	if dryRun {
		data := output.PostDeleteDryRunData{
			WouldDelete: output.PostDeletePreview{
				Endpoint: "DELETE /rest/posts/" + postURN,
				PostURN:  postURN,
			},
			Mode: "dry_run",
		}
		return data, cmdID, 0, nil
	}
	data, err := r.transport.DeletePost(ctx, postURN)
	if err != nil {
		return nil, cmdID, 0, err
	}
	return data, cmdID, 204, nil
}

func (r *batchRunner) runCommentAdd(ctx context.Context, op batchOp, dryRun bool) (any, string, int, error) {
	cmdID := newCommandID("comment add", r.a.deps.Now().UTC())
	postURN := stringArg(op.Args, "post_urn")
	if postURN == "" {
		return nil, cmdID, 0, fmt.Errorf("missing required arg: post_urn")
	}
	text := stringArg(op.Args, "text")
	if text == "" {
		return nil, cmdID, 0, fmt.Errorf("missing required arg: text")
	}
	if dryRun {
		data := output.CommentAddDryRunData{
			WouldComment: output.CommentAddPreview{
				Endpoint: "POST /rest/socialActions/" + postURN + "/comments",
				PostURN:  postURN,
				Text:     text,
			},
			Mode: "dry_run",
		}
		return data, cmdID, 0, nil
	}
	comment, err := r.transport.AddComment(ctx, postURN, text)
	if err != nil {
		return nil, cmdID, 0, err
	}
	return output.CommentAddData{CommentData: *comment}, cmdID, 201, nil
}

func (r *batchRunner) runReactAdd(ctx context.Context, op batchOp, dryRun bool) (any, string, int, error) {
	cmdID := newCommandID("react add", r.a.deps.Now().UTC())
	postURN := stringArg(op.Args, "post_urn")
	if postURN == "" {
		return nil, cmdID, 0, fmt.Errorf("missing required arg: post_urn")
	}
	rtStr := stringArg(op.Args, "type")
	if rtStr == "" {
		rtStr = string(output.ReactionLike)
	}
	rtype, err := output.ParseReactionType(rtStr)
	if err != nil {
		return nil, cmdID, 0, fmt.Errorf("invalid reaction type: %w", err)
	}
	if dryRun {
		data := output.ReactionAddDryRunData{
			WouldReact: output.ReactionAddPreview{
				Endpoint: "POST /rest/reactions",
				PostURN:  postURN,
				Type:     rtype,
			},
			Mode: "dry_run",
		}
		return data, cmdID, 0, nil
	}
	data, err := r.transport.AddReaction(ctx, postURN, rtype)
	if err != nil {
		return nil, cmdID, 0, err
	}
	return output.ReactionAddData{ReactionData: *data, TargetURN: postURN}, cmdID, 201, nil
}

func (r *batchRunner) runPostSchedule(ctx context.Context, op batchOp) (any, string, int, error) {
	cmdID := newCommandID("post_schedule", r.a.deps.Now().UTC())

	atStr := stringArg(op.Args, "at")
	if atStr == "" {
		return nil, cmdID, 0, fmt.Errorf("missing required arg: at")
	}
	scheduledAt, err := time.Parse(time.RFC3339, atStr)
	if err != nil {
		return nil, cmdID, 0, fmt.Errorf("invalid at value: %w", err)
	}
	scheduledAt = scheduledAt.UTC()
	if !scheduledAt.After(r.a.deps.Now().UTC().Add(30 * time.Second)) {
		return nil, cmdID, 0, fmt.Errorf("at must be at least 30 seconds in the future")
	}

	text := stringArg(op.Args, "text")
	if text == "" {
		return nil, cmdID, 0, fmt.Errorf("missing required arg: text")
	}
	visStr := stringArg(op.Args, "visibility")
	if visStr == "" {
		visStr = "PUBLIC"
	}
	visibility, err := output.ParseVisibility(visStr)
	if err != nil {
		return nil, cmdID, 0, fmt.Errorf("invalid visibility: %w", err)
	}
	imagePath := stringArg(op.Args, "image_path")
	if imagePath != "" && !strings.HasPrefix(imagePath, "/") {
		return nil, cmdID, 0, fmt.Errorf("image_path must be absolute")
	}

	entry := schedule.Entry{
		CommandID:      cmdID,
		State:          schedule.StatePending,
		ScheduledAt:    scheduledAt,
		CreatedAt:      r.a.deps.Now().UTC(),
		Profile:        r.a.settings.Profile,
		Transport:      r.a.settings.Transport,
		IdempotencyKey: op.IdempotencyKey,
		Request: schedule.Request{
			Text:       text,
			Visibility: string(visibility),
			ImagePath:  imagePath,
			ImageAlt:   stringArg(op.Args, "image_alt"),
		},
	}
	if err := r.a.deps.ScheduleStore.Add(ctx, entry); err != nil {
		return nil, cmdID, 0, err
	}

	data := output.ScheduledPostData{
		CommandID:      entry.CommandID,
		State:          output.ScheduleStatePending,
		ScheduledAt:    entry.ScheduledAt,
		CreatedAt:      entry.CreatedAt,
		Profile:        entry.Profile,
		Transport:      entry.Transport,
		IdempotencyKey: entry.IdempotencyKey,
		Request: output.ScheduleRequest{
			Text:       entry.Request.Text,
			Visibility: output.Visibility(entry.Request.Visibility),
			ImagePath:  entry.Request.ImagePath,
			ImageAlt:   entry.Request.ImageAlt,
		},
	}
	return data, cmdID, 0, nil
}

// emitPendingApproval stages the op and emits a pending_approval result.
// Batch continues with remaining ops — pending approval is not an error.
func (r *batchRunner) emitPendingApproval(ctx context.Context, lineNum int, op batchOp) error {
	cmdName := strings.TrimSpace(op.Command)
	cmdID := newCommandID(cmdName, r.a.deps.Now().UTC())
	now := r.a.deps.Now().UTC()

	// Build the preview payload reusing the same helpers as dry-run.
	var payload any
	switch cmdName {
	case "post create":
		text := stringArg(op.Args, "text")
		visStr := stringArg(op.Args, "visibility")
		if visStr == "" {
			visStr = "PUBLIC"
		}
		vis, _ := output.ParseVisibility(visStr)
		payload = output.PostPayloadPreview{
			Endpoint:   "POST /rest/posts",
			Text:       text,
			Visibility: vis,
			Media:      stringArg(op.Args, "media"),
		}
	case "post delete":
		postURN := stringArg(op.Args, "post_urn")
		payload = output.PostDeletePreview{
			Endpoint: "DELETE /rest/posts/" + postURN,
			PostURN:  postURN,
		}
	case "comment add":
		postURN := stringArg(op.Args, "post_urn")
		payload = output.CommentAddPreview{
			Endpoint: "POST /rest/socialActions/" + postURN + "/comments",
			PostURN:  postURN,
			Text:     stringArg(op.Args, "text"),
		}
	case "react add":
		postURN := stringArg(op.Args, "post_urn")
		rtStr := stringArg(op.Args, "type")
		if rtStr == "" {
			rtStr = string(output.ReactionLike)
		}
		rt, _ := output.ParseReactionType(rtStr)
		payload = output.ReactionAddPreview{
			Endpoint: "POST /rest/reactions",
			PostURN:  postURN,
			Type:     rt,
		}
	default:
		payload = op.Args
	}

	entry := approval.Entry{
		CommandID:      cmdID,
		Command:        cmdName,
		CreatedAt:      now,
		Transport:      r.a.settings.Transport,
		Profile:        r.a.settings.Profile,
		Payload:        payload,
		IdempotencyKey: op.IdempotencyKey,
	}
	stagedPath, stageErr := r.a.deps.ApprovalStore.Stage(ctx, entry)
	if stageErr != nil {
		return r.emitValidationError(lineNum, op, fmt.Sprintf("approval stage failed: %s", stageErr))
	}

	r.a.auditMutation(r.cmd, cmdID, "pending_approval", "normal", "", 0, "", nil)

	pendingData := output.ApprovalPendingData{
		CommandID:      cmdID,
		Command:        cmdName,
		StagedAt:       now,
		StagedPath:     stagedPath,
		Payload:        payload,
		IdempotencyKey: op.IdempotencyKey,
	}

	meta := r.a.metadata(r.cmd, output.StatusPendingApproval)
	meta.Command = "batch"
	meta.CommandID = cmdID

	result := output.BatchOpResultData{
		Line:           lineNum,
		Status:         output.StatusPendingApproval,
		Command:        op.Command,
		IdempotencyKey: op.IdempotencyKey,
		CommandID:      cmdID,
		Data:           pendingData,
	}
	envelope := output.SuccessEnvelope[output.BatchOpResultData]{
		BaseEnvelope: output.BaseEnvelope{
			Status:      output.StatusPendingApproval,
			CommandID:   cmdID,
			Command:     "batch",
			Transport:   r.a.settings.Transport,
			GeneratedAt: now,
		},
		Data: result,
	}
	r.writeEnvelope(envelope)
	// pending_approval is not an error; return nil so batch continues.
	return nil
}

// emitSuccess writes a batch op result envelope for a successful op.
func (r *batchRunner) emitSuccess(lineNum int, op batchOp, cmdID string, httpStatus int, data any) error {
	meta := r.a.metadata(r.cmd, output.StatusOK)
	meta.Command = "batch"
	meta.CommandID = cmdID
	if cmdID == "" {
		meta.CommandID = newCommandID("batch", r.a.deps.Now().UTC())
	}
	result := output.BatchOpResultData{
		Line:           lineNum,
		Status:         output.StatusOK,
		Command:        op.Command,
		IdempotencyKey: op.IdempotencyKey,
		CommandID:      cmdID,
		HTTPStatus:     httpStatus,
		Data:           data,
	}
	envelope := output.Success(meta, result)
	r.writeEnvelope(envelope)
	r.appendProgress(lineNum, op.IdempotencyKey, cmdID, "ok")
	return nil
}

// emitCachedResult writes a batch op result replayed from the idempotency store.
func (r *batchRunner) emitCachedResult(lineNum int, op batchOp, cached idempotency.Entry) error {
	meta := r.a.metadata(r.cmd, output.StatusOK)
	meta.Command = "batch"
	meta.CommandID = cached.CommandID

	var data any
	if len(cached.Result) > 0 {
		var raw any
		if err := json.Unmarshal(cached.Result, &raw); err == nil {
			data = raw
		}
	}

	result := output.BatchOpResultData{
		Line:           lineNum,
		Status:         output.StatusOK,
		Command:        op.Command,
		IdempotencyKey: op.IdempotencyKey,
		CommandID:      cached.CommandID,
		HTTPStatus:     cached.HTTPStatus,
		FromCache:      true,
		Data:           data,
	}
	envelope := output.Success(meta, result)
	envelope.FromCache = true
	r.writeEnvelope(envelope)
	r.appendProgress(lineNum, op.IdempotencyKey, cached.CommandID, "ok")
	return nil
}

// emitSkipped writes a skip notice for ops already in the progress file.
func (r *batchRunner) emitSkipped(lineNum int, op batchOp) {
	meta := r.a.metadata(r.cmd, output.StatusOK)
	meta.Command = "batch"
	meta.CommandID = newCommandID("batch", r.a.deps.Now().UTC())
	result := output.BatchOpResultData{
		Line:           lineNum,
		Status:         output.StatusOK,
		Command:        op.Command,
		IdempotencyKey: op.IdempotencyKey,
		FromCache:      true,
	}
	envelope := output.Success(meta, result)
	envelope.FromCache = true
	r.writeEnvelope(envelope)
}

// emitValidationError writes a validation_error result for a bad op.
func (r *batchRunner) emitValidationError(lineNum int, op batchOp, msg string) error {
	meta := r.a.metadata(r.cmd, output.StatusValidation)
	meta.Command = "batch"
	meta.CommandID = newCommandID("batch", r.a.deps.Now().UTC())
	result := output.BatchOpResultData{
		Line:           lineNum,
		Status:         output.StatusValidation,
		Command:        op.Command,
		IdempotencyKey: op.IdempotencyKey,
		Error:          msg,
		Code:           string(output.ErrorCodeValidation),
	}
	envelope := output.Success(meta, result)
	r.writeEnvelope(envelope)
	return fmt.Errorf("validation: %s", msg)
}

// emitOpError writes an error result for a failed transport op.
func (r *batchRunner) emitOpError(lineNum int, op batchOp, cmdID string, opErr error) error {
	errCode := string(output.ErrorCodeTransport)
	if ae, ok := api.AsError(opErr); ok {
		switch {
		case ae.IsUnauthorized():
			errCode = string(output.ErrorCodeUnauthorized)
		case ae.IsForbidden():
			errCode = string(output.ErrorCodeForbidden)
		case ae.IsNotFound():
			errCode = string(output.ErrorCodeNotFound)
		case ae.IsRateLimited():
			errCode = string(output.ErrorCodeRateLimited)
		case ae.IsValidation():
			errCode = string(output.ErrorCodeValidation)
		}
	}

	meta := r.a.metadata(r.cmd, output.StatusError)
	meta.Command = "batch"
	meta.CommandID = cmdID
	if cmdID == "" {
		meta.CommandID = newCommandID("batch", r.a.deps.Now().UTC())
	}
	result := output.BatchOpResultData{
		Line:           lineNum,
		Status:         output.StatusError,
		Command:        op.Command,
		IdempotencyKey: op.IdempotencyKey,
		CommandID:      cmdID,
		Error:          opErr.Error(),
		Code:           errCode,
	}
	envelope := output.Success(meta, result)
	r.writeEnvelope(envelope)
	return opErr
}

func (r *batchRunner) writeEnvelope(envelope any) {
	r.outMu.Lock()
	defer r.outMu.Unlock()
	_ = output.WriteJSON(r.out, envelope)
}

func (r *batchRunner) appendProgress(lineNum int, ikey, cmdID, status string) {
	if r.progressPath == "" {
		return
	}
	entry := progressEntry{
		Line:           lineNum,
		Status:         status,
		IdempotencyKey: ikey,
		CommandID:      cmdID,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')
	f, err := os.OpenFile(r.progressPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(line)
	_ = f.Close()
}

// paceRateLimit sleeps until the rate-limit reset window if remaining is low.
func (r *batchRunner) paceRateLimit(ctx context.Context) {
	rla, ok := r.transport.(api.RateLimitAware)
	if !ok {
		return
	}
	rl := rla.LastRateLimit()
	if rl == nil || rl.Remaining == nil {
		return
	}
	if *rl.Remaining > rateLimitPaceThresh {
		return
	}
	if rl.ResetAt == "" {
		return
	}
	resetTime, err := time.Parse(time.RFC3339, rl.ResetAt)
	if err != nil {
		return
	}
	sleepUntil := resetTime.Add(time.Second)
	wait := time.Until(sleepUntil)
	if wait <= 0 {
		return
	}
	r.a.logger.Warn("batch: rate limit low, pacing", "remaining", *rl.Remaining, "sleep_until", sleepUntil)
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
}

func parseBatchOps(r io.Reader) ([]batchOp, error) {
	var ops []batchOp
	sc := bufio.NewScanner(r)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var op batchOp
		if err := json.Unmarshal([]byte(line), &op); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		if strings.TrimSpace(op.Command) == "" {
			return nil, fmt.Errorf("line %d: missing required field: command", lineNum)
		}
		ops = append(ops, op)
	}
	return ops, sc.Err()
}

func loadProgress(path string) map[int]bool {
	done := map[int]bool{}
	f, err := os.Open(path)
	if err != nil {
		return done
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e progressEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Status == "ok" && e.Line > 0 {
			done[e.Line] = true
		}
	}
	return done
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
