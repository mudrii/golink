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
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	"github.com/mudrii/golink/internal/output"
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
}

type app struct {
	buildInfo BuildInfo
	deps      Dependencies
	loader    *config.Loader
	settings  config.Settings
	logger    *slog.Logger
}

type commandFailure struct {
	jsonMode bool
	exitCode int
	payload  any
	text     string
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
	jsonMode, transport := preflightFlags(args)
	preloadedSettings := config.Settings{
		JSON:      jsonMode,
		Transport: transport,
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
		if application.settings.JSON {
			envelope := output.ValidationError(application.metadata(bestEffortCommand(rootCmd, args), output.StatusValidation), message, "")
			if writeErr := output.WriteJSON(normalized.Stderr, envelope); writeErr != nil {
				_, _ = fmt.Fprintln(normalized.Stderr, writeErr.Error())
				return 1
			}

			return 2
		}

		_, _ = fmt.Fprintln(normalized.Stderr, message)
		return 1
	}

	if failure.jsonMode && failure.payload != nil {
		if writeErr := output.WriteJSON(normalized.Stderr, failure.payload); writeErr != nil {
			_, _ = fmt.Fprintln(normalized.Stderr, writeErr.Error())
			return 1
		}
	} else if failure.text != "" {
		if _, writeErr := fmt.Fprintln(normalized.Stderr, failure.text); writeErr != nil {
			_, _ = fmt.Fprintln(normalized.Stderr, writeErr.Error())
			return 1
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
	if a.settings.JSON {
		envelope := output.Success(a.metadata(cmd, output.StatusOK), data)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}

		return nil
	}

	if _, err := fmt.Fprintln(a.deps.Stdout, text); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}

	return nil
}

func (a *app) writeDryRun(cmd *cobra.Command, data any, text string) error {
	meta := a.metadata(cmd, output.StatusOK)
	meta.Mode = "dry_run"
	if a.settings.JSON {
		envelope := output.Success(meta, data)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}

		return nil
	}

	if _, err := fmt.Fprintln(a.deps.Stdout, text); err != nil {
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

// resolveTransport returns the Transport for the active settings and session.
func (a *app) resolveTransport(ctx context.Context, session auth.Session) (api.Transport, error) {
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
		jsonMode: a.settings.JSON,
		exitCode: 4,
		payload:  output.Error(meta, output.ErrorCodeForbidden, message, details),
		text:     text,
	}
}

func (a *app) notFoundFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}
	return &commandFailure{
		jsonMode: a.settings.JSON,
		exitCode: 5,
		payload:  output.Error(meta, output.ErrorCodeNotFound, message, details),
		text:     text,
	}
}

func (a *app) rateLimitFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}
	return &commandFailure{
		jsonMode: a.settings.JSON,
		exitCode: 5,
		payload:  output.Error(meta, output.ErrorCodeRateLimited, message, details),
		text:     text,
	}
}

func (a *app) writeUnsupported(cmd *cobra.Command, payload output.UnsupportedPayload, text string) error {
	if a.settings.JSON {
		envelope := output.Success(a.metadata(cmd, output.StatusUnsupported), payload)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}

		return nil
	}

	if _, err := fmt.Fprintln(a.deps.Stdout, text); err != nil {
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
		jsonMode: a.settings.JSON,
		exitCode: 2,
		payload:  output.ValidationError(meta, message, details),
		text:     text,
	}
}

func (a *app) authFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}

	return &commandFailure{
		jsonMode: a.settings.JSON,
		exitCode: 4,
		payload:  output.Error(meta, output.ErrorCodeUnauthorized, message, details),
		text:     text,
	}
}

func (a *app) transportFailure(cmd *cobra.Command, message, details string) error {
	meta := a.metadata(cmd, output.StatusError)
	text := message
	if details != "" {
		text += ": " + details
	}

	return &commandFailure{
		jsonMode: a.settings.JSON,
		exitCode: 5,
		payload:  output.Error(meta, output.ErrorCodeTransport, message, details),
		text:     text,
	}
}

// preflightFlags inspects just the flags we need before Cobra has parsed
// anything (so flag errors can still honor --json). We reuse pflag with
// ContinueOnError so unknown flags from subcommands don't abort the preflight,
// and fall back to env vars so preflight state matches the final config.
func preflightFlags(args []string) (jsonMode bool, transport string) {
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

	fs := pflag.NewFlagSet("preflight", pflag.ContinueOnError)
	fs.ParseErrorsAllowlist.UnknownFlags = true
	fs.SetOutput(io.Discard)
	fs.BoolVar(&jsonMode, "json", jsonMode, "")
	fs.StringVar(&transport, "transport", transport, "")
	_ = fs.Parse(args)

	switch transport {
	case "official", "unofficial", "auto":
	default:
		transport = "official"
	}
	return jsonMode, transport
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
