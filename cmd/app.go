package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

const rootCommandName = "golink"

// BuildInfo contains compile-time metadata injected by the build.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// Dependencies controls runtime wiring for command execution.
type Dependencies struct {
	Stdout        io.Writer
	Stderr        io.Writer
	Now           func() time.Time
	SessionStore  auth.Store
	IsInteractive func() bool
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
	application := &app{
		buildInfo: buildInfo,
		deps:      normalized,
		loader:    config.NewLoader(),
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
	if deps.SessionStore == nil {
		deps.SessionStore = auth.NewKeyringStore("")
	}
	if deps.IsInteractive == nil {
		deps.IsInteractive = defaultIsInteractive
	}

	return deps
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
	return strings.TrimSpace(path)
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
	return output.EnvelopeMeta{
		Status:      status,
		CommandID:   newCommandID(commandName(cmd), a.deps.Now().UTC()),
		Command:     commandName(cmd),
		Transport:   a.settings.Transport,
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

func (a *app) writeDryRun(cmd *cobra.Command, data output.PostCreateDryRunData, text string) error {
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
