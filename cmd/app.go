package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/mudrii/golink/internal/plan"
	"github.com/mudrii/golink/internal/schedule"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const rootCommandName = "golink"

// BuildInfo contains compile-time metadata injected by the build.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// TransportFactory constructs the Transport used by networked commands.
// It receives the resolved settings and an optional session (may be empty
// for auth commands that run before a session exists).
type TransportFactory func(ctx context.Context, settings config.Settings, session auth.Session, logger *slog.Logger) (api.Transport, error)

// Dependencies controls runtime wiring for command execution.
type Dependencies struct {
	Stdout           io.Writer
	Stderr           io.Writer
	Now              func() time.Time
	HTTPClient       *http.Client
	BrowserOpener    auth.BrowserOpener
	LoginRunner      func(context.Context, *auth.LoginRequest, string, string, auth.LoginFlowOptions) (*auth.Session, error)
	SessionStore     auth.Store
	IsInteractive    func() bool
	TransportFactory TransportFactory
	AuditSink        audit.Sink
	IdempotencyStore idempotency.Store
	ApprovalStore    approval.Store
	ScheduleStore    schedule.Store
	// TokenURL overrides the LinkedIn token endpoint; defaults to auth.TokenURL.
	// Set in tests to point at a local httptest server.
	TokenURL string
	// UserinfoURL overrides the LinkedIn userinfo endpoint used by doctor probe.
	// Set in tests to point at a local httptest server.
	UserinfoURL string
}

type app struct {
	buildInfo  BuildInfo
	deps       Dependencies
	loader     *config.Loader
	settings   config.Settings
	logger     *slog.Logger
	activePlan *plan.Plan
}

type commandFailure struct {
	jsonMode   bool
	outputMode string
	exitCode   int
	payload    any
	// errMsg and errCode are used by the renderer for compact/jsonl modes.
	errMsg  string
	errCode string
	text    string
}

func (e *commandFailure) Error() string {
	return e.text
}

// Execute runs golink using the current process arguments and stdio handles.
func Execute(ctx context.Context, buildInfo BuildInfo) int {
	return ExecuteContext(ctx, os.Args[1:], Dependencies{}, buildInfo)
}

// ExecuteContext runs golink with injected dependencies for tests or embedding.
func ExecuteContext(ctx context.Context, args []string, deps Dependencies, buildInfo BuildInfo) int {
	normalized := normalizeDependencies(deps)
	jsonMode, transport, outputMode := preflightFlags(args)
	preloadedSettings := config.Settings{
		JSON:      jsonMode,
		Transport: transport,
		Output:    outputMode,
	}
	application := &app{
		buildInfo: buildInfo,
		deps:      normalized,
		loader:    config.NewLoader(),
		settings:  preloadedSettings,
		logger:    slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}

	rootCmd, err := newRootCommand(application)
	if err != nil {
		_, _ = fmt.Fprintln(normalized.Stderr, err.Error())
		return 1
	}
	rootCmd.SetArgs(args)
	err = rootCmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}

	var failure *commandFailure
	_ = errors.As(err, &failure)

	if failure == nil {
		message := err.Error()
		mode := application.settings.Output
		switch mode {
		case output.ModeJSON:
			envelope := output.ValidationError(application.metadata(bestEffortCommand(rootCmd, args), output.StatusValidation), message, "")
			if writeErr := output.WriteJSON(normalized.Stderr, envelope); writeErr != nil {
				_, _ = fmt.Fprintln(normalized.Stderr, writeErr.Error())
				return 1
			}
			return 2
		case output.ModeCompact, output.ModeJSONL:
			meta := application.metadata(bestEffortCommand(rootCmd, args), output.StatusValidation)
			base := output.BuildBase(meta)
			if writeErr := output.RenderError(normalized.Stderr, mode, base, message, string(output.ErrorCodeValidation), message); writeErr != nil {
				_, _ = fmt.Fprintln(normalized.Stderr, writeErr.Error())
				return 1
			}
			return 2
		default:
			_, _ = fmt.Fprintln(normalized.Stderr, message)
			return 1
		}
	}

	mode := failure.outputMode
	if mode == "" {
		// Fall back: use jsonMode for backward-compat with tests that set it directly.
		if failure.jsonMode {
			mode = output.ModeJSON
		} else {
			mode = output.ModeText
		}
	}

	switch mode {
	case output.ModeJSON:
		if failure.payload != nil {
			if writeErr := output.WriteJSON(normalized.Stderr, failure.payload); writeErr != nil {
				_, _ = fmt.Fprintln(normalized.Stderr, writeErr.Error())
				return 1
			}
		}
	case output.ModeCompact, output.ModeJSONL:
		if failure.payload != nil {
			// Extract base envelope from the typed payload for the renderer.
			if env, msg, code, ok := output.ExtractErrorEnvelope(failure.payload); ok {
				if writeErr := output.RenderError(normalized.Stderr, mode, env, msg, code, failure.text); writeErr != nil {
					_, _ = fmt.Fprintln(normalized.Stderr, writeErr.Error())
					return 1
				}
			} else {
				_, _ = fmt.Fprintln(normalized.Stderr, failure.text)
			}
		} else if failure.text != "" {
			_, _ = fmt.Fprintln(normalized.Stderr, failure.text)
		}
	default:
		if failure.text != "" {
			if _, writeErr := fmt.Fprintln(normalized.Stderr, failure.text); writeErr != nil {
				_, _ = fmt.Fprintln(normalized.Stderr, writeErr.Error())
				return 1
			}
		}
	}

	return failure.exitCode
}

func normalizeDependencies(deps Dependencies) Dependencies {
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = http.DefaultClient
	}
	if deps.BrowserOpener == nil {
		deps.BrowserOpener = openBrowser
	}
	if deps.LoginRunner == nil {
		deps.LoginRunner = auth.CompleteLogin
	}
	if deps.SessionStore == nil {
		deps.SessionStore = auth.NewKeyringStore("")
	}
	if deps.IsInteractive == nil {
		deps.IsInteractive = defaultIsInteractive
	}
	if deps.TransportFactory == nil {
		deps.TransportFactory = defaultTransportFactory(deps)
	}
	if deps.TokenURL == "" {
		deps.TokenURL = auth.TokenURL
	}
	if deps.IdempotencyStore == nil {
		deps.IdempotencyStore = idempotency.NewFileStore("")
	}
	if deps.ApprovalStore == nil {
		deps.ApprovalStore = approval.NewFileStore("")
	}
	if deps.ScheduleStore == nil {
		deps.ScheduleStore = schedule.NewFileStore("")
	}

	return deps
}

// defaultTransportFactory returns a TransportFactory that builds an official
// adapter for the "official" transport and a NoopTransport for every other
// value. The HTTPClient from Dependencies is reused as the underlying
// transport for retryable requests so tests can swap it freely.
func defaultTransportFactory(deps Dependencies) TransportFactory {
	return func(ctx context.Context, settings config.Settings, session auth.Session, logger *slog.Logger) (api.Transport, error) {
		switch settings.Transport {
		case "official", "auto":
			client, err := api.NewClient(api.ClientConfig{
				APIVersion: settings.APIVersion,
				HTTPClient: deps.HTTPClient,
				Logger:     logger,
				Token: func(_ context.Context) (string, error) {
					return strings.TrimSpace(session.AccessToken), nil
				},
			})
			if err != nil {
				return nil, err
			}
			return api.NewOfficial(api.OfficialConfig{
				Client:    client,
				AuthorURN: session.MemberURN,
				Now:       deps.Now,
			}), nil
		default:
			return api.NewNoopTransport(settings.Transport, "official"), nil
		}
	}
}

func openBrowser(ctx context.Context, targetURL string) error {
	var name string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		name = "open"
		args = []string{targetURL}
	case "linux":
		name = "xdg-open"
		args = []string{targetURL}
	case "windows":
		name = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", targetURL}
	default:
		return fmt.Errorf("unsupported platform for browser launch: %s", runtime.GOOS)
	}

	if err := exec.CommandContext(ctx, name, args...).Start(); err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}

	return nil
}

func defaultIsInteractive() bool {
	stdinInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	stdoutInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}

	return stdinInfo.Mode()&os.ModeCharDevice != 0 && stdoutInfo.Mode()&os.ModeCharDevice != 0
}

func newCommandID(command string, now time.Time) string {
	commandSlug := strings.ReplaceAll(strings.TrimSpace(command), " ", "_")
	if commandSlug == "" {
		commandSlug = rootCommandName
	}

	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Sprintf("cmd_%s_%d", commandSlug, now.UTC().UnixNano())
	}

	return fmt.Sprintf("cmd_%s_%d%s", commandSlug, now.UTC().Unix(), hex.EncodeToString(randomBytes))
}

func commandName(cmd *cobra.Command) string {
	path := strings.TrimSpace(cmd.CommandPath())
	path = strings.TrimPrefix(path, rootCommandName)
	path = strings.TrimSpace(path)
	if path == "" {
		return rootCommandName
	}

	return path
}

func buildVersionData(buildInfo BuildInfo) output.VersionData {
	version := buildInfo.Version
	if version == "" {
		version = "dev"
	}

	return output.VersionData{
		Version:   version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Commit:    buildInfo.Commit,
		BuildDate: buildInfo.BuildDate,
	}
}

func (a *app) metadata(cmd *cobra.Command, status output.CommandStatus) output.EnvelopeMeta {
	transport := a.settings.Transport
	if transport == "" {
		transport = "official"
	}

	return output.EnvelopeMeta{
		Status:      status,
		CommandID:   newCommandID(commandName(cmd), a.deps.Now().UTC()),
		Command:     commandName(cmd),
		Transport:   transport,
		GeneratedAt: a.deps.Now().UTC(),
	}
}

func (a *app) writeSuccess(cmd *cobra.Command, data any, text string) error {
	meta := a.metadata(cmd, output.StatusOK)
	mode := a.settings.Output
	if mode == output.ModeJSON {
		envelope := output.Success(meta, data)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	base := output.BuildBase(meta)
	if err := output.RenderSuccess(a.deps.Stdout, mode, base, data, text); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

// writeSuccessFromCache writes a success envelope with from_cache:true set on
// the base envelope. Used when idempotency replays a stored result.
func (a *app) writeSuccessFromCache(cmd *cobra.Command, data any, text string) error {
	meta := a.metadata(cmd, output.StatusOK)
	mode := a.settings.Output
	if mode == output.ModeJSON {
		envelope := output.Success(meta, data)
		envelope.FromCache = true
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	base := output.BuildBase(meta)
	base.FromCache = true
	if err := output.RenderSuccess(a.deps.Stdout, mode, base, data, text); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

func (a *app) writeDryRun(cmd *cobra.Command, data any, text string) error {
	meta := a.metadata(cmd, output.StatusOK)
	meta.Mode = "dry_run"
	mode := a.settings.Output
	if mode == output.ModeJSON {
		envelope := output.Success(meta, data)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	base := output.BuildBase(meta)
	if err := output.RenderSuccess(a.deps.Stdout, mode, base, data, text); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

// resolveSession loads the stored session for the active profile and verifies
// it is currently usable. On failure it returns a commandFailure primed with
// the right envelope and exit code so command handlers can simply return.
func (a *app) resolveSession(cmd *cobra.Command) (auth.Session, error) {
	session, err := a.deps.SessionStore.LoadSession(cmd.Context(), a.settings.Profile)
	if err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return auth.Session{}, a.authFailure(cmd, "Token expired or invalid. Re-run: golink auth login", "no active session for the selected profile")
		}
		return auth.Session{}, a.transportFailure(cmd, "failed to resolve session", err.Error())
	}
	authenticated, err := session.IsAuthenticated(a.deps.Now())
	if err != nil {
		return auth.Session{}, a.authFailure(cmd, "Token expired or invalid. Re-run: golink auth login", err.Error())
	}
	if !authenticated {
		return auth.Session{}, a.authFailure(cmd, "Token expired or invalid. Re-run: golink auth login", "session has no usable access token")
	}
	return *session, nil
}

// maybeRefreshSession silently attempts to refresh the access token when it is
// within 5 minutes of expiry and a refresh token is available. On success the
// updated session is persisted and returned. On failure the original session is
// returned unchanged and the error is logged at WARN so the caller can fall
// through to existing 401 handling.
func (a *app) maybeRefreshSession(ctx context.Context, session auth.Session) auth.Session {
	if session.RefreshToken == "" {
		return session
	}
	if session.ExpiresAt.Sub(a.deps.Now()) >= 5*time.Minute {
		return session
	}

	token, err := auth.RefreshAccessToken(ctx, a.deps.HTTPClient, a.deps.TokenURL, a.settings.ClientID, session.RefreshToken)
	if err != nil {
		a.logger.Warn("auto-refresh failed; proceeding with existing session", "error", err)
		return session
	}

	session.AccessToken = token.AccessToken
	if token.ExpiresIn > 0 {
		session.ExpiresAt = a.deps.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	if token.Scope != "" {
		session.Scopes = strings.Fields(token.Scope)
	}
	if token.RefreshToken != "" {
		session.RefreshToken = token.RefreshToken
		if token.RefreshTokenExpiresIn > 0 {
			session.RefreshExpiresAt = a.deps.Now().UTC().Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second)
		}
	}

	if err := a.deps.SessionStore.SaveSession(ctx, session); err != nil {
		a.logger.Warn("auto-refresh: failed to persist refreshed session", "error", err)
	}

	return session
}

// resolveTransport returns the Transport for the active settings and session,
// auto-refreshing the access token when near expiry.
func (a *app) resolveTransport(ctx context.Context, session auth.Session) (api.Transport, error) {
	session = a.maybeRefreshSession(ctx, session)
	return a.deps.TransportFactory(ctx, a.settings, session, a.logger)
}

// mapTransportError converts an api error into the appropriate envelope. If
// the error is an ErrFeatureUnavailable, it is surfaced as an unsupported
// success envelope. Typed api.Error values map per the PROMPT exit-code table.
func (a *app) mapTransportError(cmd *cobra.Command, feature string, err error) error {
	if fe, ok := api.AsFeatureUnavailable(err); ok {
		payload := output.UnsupportedPayload{
			Feature:           feature,
			Reason:            fe.Reason,
			SuggestedFallback: suggestedFallback(fe.SuggestedTransport),
		}
		return a.writeUnsupported(cmd, payload, fmt.Sprintf("unsupported: %s", feature))
	}
	apiErr, ok := api.AsError(err)
	if !ok {
		return a.transportFailure(cmd, "transport request failed", err.Error())
	}

	switch {
	case apiErr.IsUnauthorized():
		return a.authFailure(cmd, "Token expired or invalid. Re-run: golink auth login", apiErr.Message)
	case apiErr.IsForbidden():
		return a.forbiddenFailure(cmd, "Insufficient permission/scope for this operation", apiErr.Message)
	case apiErr.IsNotFound():
		return a.notFoundFailure(cmd, "Resource not found", apiErr.Message)
	case apiErr.IsValidation():
		return a.validationFailure(cmd, "Validation error from LinkedIn API", apiErr.Message)
	case apiErr.IsRateLimited():
		return a.rateLimitFailure(cmd, "Rate limit exceeded. Respect retry window before retrying.", apiErr.Message)
	case apiErr.IsServerError():
		return a.transportFailure(cmd, "LinkedIn API temporarily unavailable.", apiErr.Message)
	default:
		return a.transportFailure(cmd, "LinkedIn API error", apiErr.Error())
	}
}

func suggestedFallback(transport string) string {
	if strings.TrimSpace(transport) == "" {
		return ""
	}
	return "--transport=" + transport
}

func (a *app) forbiddenFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}
	return &commandFailure{
		jsonMode:   a.settings.JSON,
		outputMode: a.settings.Output,
		exitCode:   4,
		payload:    output.Error(meta, output.ErrorCodeForbidden, message, details),
		errMsg:     message,
		errCode:    string(output.ErrorCodeForbidden),
		text:       text,
	}
}

func (a *app) notFoundFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}
	return &commandFailure{
		jsonMode:   a.settings.JSON,
		outputMode: a.settings.Output,
		exitCode:   5,
		payload:    output.Error(meta, output.ErrorCodeNotFound, message, details),
		errMsg:     message,
		errCode:    string(output.ErrorCodeNotFound),
		text:       text,
	}
}

func (a *app) rateLimitFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}
	return &commandFailure{
		jsonMode:   a.settings.JSON,
		outputMode: a.settings.Output,
		exitCode:   5,
		payload:    output.Error(meta, output.ErrorCodeRateLimited, message, details),
		errMsg:     message,
		errCode:    string(output.ErrorCodeRateLimited),
		text:       text,
	}
}

func (a *app) writeUnsupported(cmd *cobra.Command, payload output.UnsupportedPayload, text string) error {
	meta := a.metadata(cmd, output.StatusUnsupported)
	mode := a.settings.Output
	if mode == output.ModeJSON {
		envelope := output.Success(meta, payload)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	base := output.BuildBase(meta)
	if err := output.RenderSuccess(a.deps.Stdout, mode, base, payload, text); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

func (a *app) validationFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusValidation)
	text := message
	if details != "" {
		text += ": " + details
	}

	return &commandFailure{
		jsonMode:   a.settings.JSON,
		outputMode: a.settings.Output,
		exitCode:   2,
		payload:    output.ValidationError(meta, message, details),
		errMsg:     message,
		errCode:    string(output.ErrorCodeValidation),
		text:       text,
	}
}

func (a *app) authFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}

	return &commandFailure{
		jsonMode:   a.settings.JSON,
		outputMode: a.settings.Output,
		exitCode:   4,
		payload:    output.Error(meta, output.ErrorCodeUnauthorized, message, details),
		errMsg:     message,
		errCode:    string(output.ErrorCodeUnauthorized),
		text:       text,
	}
}

func (a *app) transportFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}

	return &commandFailure{
		jsonMode:   a.settings.JSON,
		outputMode: a.settings.Output,
		exitCode:   5,
		payload:    output.Error(meta, output.ErrorCodeTransport, message, details),
		errMsg:     message,
		errCode:    string(output.ErrorCodeTransport),
		text:       text,
	}
}

// ErrApprovalRequired is returned by approvalPending to signal exit code 3.
var ErrApprovalRequired = errors.New("approval required")

// approvalPending stages an approval entry and returns the commandFailure that
// causes the process to exit with code 3. The caller returns this error directly.
func (a *app) approvalPending(cmd *cobra.Command, cmdID string, payload any, ikey string) error {
	now := a.deps.Now().UTC()
	entry := approval.Entry{
		CommandID:      cmdID,
		Command:        commandName(cmd),
		CreatedAt:      now,
		Transport:      a.settings.Transport,
		Profile:        a.settings.Profile,
		Payload:        payload,
		IdempotencyKey: ikey,
	}
	path, stageErr := a.deps.ApprovalStore.Stage(cmd.Context(), entry)
	if stageErr != nil {
		return a.transportFailure(cmd, "approval stage failed", stageErr.Error())
	}

	a.auditMutation(cmd, cmdID, "pending_approval", "normal", "", 0, "", nil)

	data := output.ApprovalPendingData{
		CommandID:      cmdID,
		Command:        commandName(cmd),
		StagedAt:       now,
		StagedPath:     path,
		Payload:        payload,
		IdempotencyKey: ikey,
	}

	base := output.BaseEnvelope{
		Status:      output.StatusPendingApproval,
		CommandID:   cmdID,
		Command:     commandName(cmd),
		Transport:   a.settings.Transport,
		GeneratedAt: now,
	}
	text := fmt.Sprintf("approval required: staged at %s", path)

	mode := a.settings.Output
	var writeErr error
	if mode == output.ModeJSON {
		envelope := output.ApprovalPendingOutput{BaseEnvelope: base, Data: data}
		writeErr = output.WriteJSON(a.deps.Stdout, envelope)
	} else {
		writeErr = output.RenderSuccess(a.deps.Stdout, mode, base, data, text)
	}
	if writeErr != nil {
		return fmt.Errorf("write approval pending: %w", writeErr)
	}

	return &commandFailure{
		outputMode: a.settings.Output,
		exitCode:   3,
		text:       fmt.Sprintf("approval required: staged at %s", path),
	}
}

// idempotencyCheck looks up key in the idempotency store for command.
// Returns (entry, true, nil) on a cache hit — the caller should replay
// the cached envelope and skip the transport call.
// Returns (zero, false, validationFailure) on a key-command mismatch.
// Returns (zero, false, nil) on a miss.
func (a *app) idempotencyCheck(cmd *cobra.Command, key, command string) (idempotency.Entry, bool, error) {
	if key == "" {
		return idempotency.Entry{}, false, nil
	}
	entry, hit, err := a.deps.IdempotencyStore.Lookup(cmd.Context(), key, command)
	if err != nil {
		if errors.Is(err, idempotency.ErrKeyCommandMismatch) {
			return idempotency.Entry{}, false, a.validationFailure(cmd, err.Error(), "")
		}
		a.logger.Warn("idempotency lookup failed; proceeding without cache", "error", err)
		return idempotency.Entry{}, false, nil
	}
	return entry, hit, nil
}

// idempotencyRecord persists entry to the store; lookup failures are logged
// at WARN and never propagate so they don't break the primary command.
func (a *app) idempotencyRecord(ctx context.Context, entry idempotency.Entry) {
	if err := a.deps.IdempotencyStore.Record(ctx, entry); err != nil {
		a.logger.Warn("idempotency record failed", "error", err)
	}
}

// auditMutation builds an audit Entry from the completed command state and
// appends it via the configured sink. Failures are logged at WARN and never
// propagate — audit must not break the primary command.
//
// Call this at the end of every mutating command's RunE, passing:
//   - status: "ok", "error", "validation_error", or "unsupported"
//   - mode:   "normal" or "dry_run"
//   - requestID, httpStatus, errorCode: from the transport response when known
//   - dryRunPreview: marshalled dry-run payload, or nil
func (a *app) auditMutation(cmd *cobra.Command, commandID, status, mode, requestID string, httpStatus int, errorCode string, dryRunPreview []byte) {
	a.auditMutationWithAuthor(cmd, commandID, status, mode, requestID, httpStatus, errorCode, dryRunPreview, "")
}

// auditMutationWithAuthor is like auditMutation but also records an optional
// authorURN in the audit entry (used when posting as an organization).
func (a *app) auditMutationWithAuthor(cmd *cobra.Command, commandID, status, mode, requestID string, httpStatus int, errorCode string, dryRunPreview []byte, authorURN string) {
	if a.deps.AuditSink == nil {
		return
	}
	entry := audit.Entry{
		TS:         a.deps.Now().UTC(),
		Profile:    a.settings.Profile,
		Transport:  a.settings.Transport,
		Command:    commandName(cmd),
		CommandID:  commandID,
		Mode:       mode,
		Status:     status,
		RequestID:  requestID,
		HTTPStatus: httpStatus,
		ErrorCode:  errorCode,
		AuthorURN:  authorURN,
	}
	if len(dryRunPreview) > 0 {
		entry.DryRunPreview = dryRunPreview
	}
	if a.activePlan != nil {
		entry.PlanSHA256 = a.activePlan.SHA256()
	}
	if err := a.deps.AuditSink.Append(cmd.Context(), entry); err != nil {
		a.logger.Warn("audit append failed", "err", err)
	}
}

// preflightFlags inspects just the flags we need before Cobra has parsed
// anything (so flag errors can still honor --json/--output). We reuse pflag with
// ContinueOnError so unknown flags from subcommands don't abort the preflight,
// and fall back to env vars so preflight state matches the final config.
// Precedence: --compact > --output > --json > text default.
func preflightFlags(args []string) (jsonMode bool, transport, outputMode string) {
	transport = "official"
	if envValue, ok := os.LookupEnv("GOLINK_JSON"); ok {
		if value, err := strconv.ParseBool(envValue); err == nil && value {
			jsonMode = true
		}
	}
	if envValue, ok := os.LookupEnv("GOLINK_TRANSPORT"); ok {
		if v := strings.TrimSpace(envValue); v != "" {
			transport = v
		}
	}

	var compact bool
	var outputFlag string
	fs := pflag.NewFlagSet("preflight", pflag.ContinueOnError)
	fs.ParseErrorsAllowlist.UnknownFlags = true
	fs.SetOutput(io.Discard)
	fs.BoolVar(&jsonMode, "json", jsonMode, "")
	fs.StringVar(&transport, "transport", transport, "")
	fs.StringVar(&outputFlag, "output", "", "")
	fs.BoolVar(&compact, "compact", false, "")
	_ = fs.Parse(args)

	switch transport {
	case "official", "unofficial", "auto":
	default:
		transport = "official"
	}

	// Resolve output mode using same precedence as config.Loader.
	switch {
	case compact:
		outputMode = "compact"
	case outputFlag != "":
		outputMode = outputFlag
	case jsonMode:
		outputMode = "json"
	default:
		outputMode = "text"
	}

	return jsonMode, transport, outputMode
}

func bestEffortCommand(root *cobra.Command, args []string) *cobra.Command {
	filtered := commandLookupArgs(args)
	if len(filtered) == 0 {
		return root
	}

	cmd, _, err := root.Find(filtered)
	if err != nil || cmd == nil {
		return root
	}

	return cmd
}

func commandLookupArgs(args []string) []string {
	lookup := make([]string, 0, len(args))
	flagsWithValues := map[string]struct{}{
		"--profile":   {},
		"--timeout":   {},
		"--transport": {},
		"--output":    {},
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				continue
			}
			if _, ok := flagsWithValues[arg]; ok && i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}

		lookup = append(lookup, arg)
	}

	return lookup
}
