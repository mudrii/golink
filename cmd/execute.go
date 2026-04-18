package cmd

// execute: reads a golink.plan/v1 document, validates it, then dispatches the
// described operation via the normal Transport path. All standard middleware
// (audit, idempotency, approval) still applies.

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/mudrii/golink/internal/plan"
	"github.com/spf13/cobra"
)

func newExecuteCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute <plan.json>",
		Short: "Execute a plan document produced by golink plan",
		Long: `execute reads a golink.plan/v1 JSON document (or stdin when path is -),
validates it, then dispatches the operation via the normal Transport path.

All standard middleware applies: audit, idempotency, approval gates.
The plan's dry_run flag is OR'd with the --dry-run CLI flag.

The plan_sha256 of the executed plan is recorded in the audit log.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planPath := args[0]
			var r io.Reader
			if planPath == "-" {
				r = os.Stdin
			} else {
				f, err := os.Open(planPath)
				if err != nil {
					return a.validationFailure(cmd, "cannot open plan file", err.Error())
				}
				defer func() { _ = f.Close() }()
				r = f
			}

			p, err := plan.Load(r)
			if err != nil {
				return a.validationFailure(cmd, "invalid plan", err.Error())
			}

			// Transport escalation warning: if plan specifies a transport and
			// the CLI flag differs, log a warning but proceed.
			cliTransport := a.settings.Transport
			if p.Transport != "" && cliTransport != "" && p.Transport != cliTransport {
				a.logger.Warn("execute: transport mismatch between plan and CLI flag",
					slog.String("plan_transport", p.Transport),
					slog.String("cli_transport", cliTransport),
				)
			}

			// Plan-level dry_run is OR'd with the CLI --dry-run flag.
			dryRunFlag, _ := cmd.Flags().GetBool("dry-run")
			effectiveDryRun := dryRunFlag || p.DryRun

			originalSettings := a.settings
			defer func() {
				a.settings = originalSettings
				a.activePlan = nil
			}()
			if p.Profile != "" {
				a.settings.Profile = p.Profile
			}
			if p.Transport != "" {
				a.settings.Transport = p.Transport
			}
			// Override dry-run in settings so downstream commands honour it.
			if effectiveDryRun {
				a.settings.DryRun = true
			}

			// Attach the plan to the app so auditMutation can record plan_sha256.
			a.activePlan = p

			// Use the plan's idempotency key if none was supplied on the CLI.
			ikey, _ := cmd.Flags().GetString("idempotency-key")
			if ikey == "" && p.IdempotencyKey != "" {
				ikey = p.IdempotencyKey
			}

			// Resolve session and transport.
			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}

			// Dispatch via the batch runner helpers, reusing all existing logic.
			runner := &batchRunner{
				a:         a,
				cmd:       cmd,
				transport: transport,
				istore:    a.deps.IdempotencyStore,
				out:       a.deps.Stdout,
			}

			op := batchOp{
				Command:         p.Command,
				Args:            p.Args,
				IdempotencyKey:  ikey,
				RequireApproval: func() bool { v, _ := cmd.Flags().GetBool("require-approval"); return v }(),
			}
			if effectiveDryRun {
				t := true
				op.DryRun = &t
			}

			auditMode := "normal"
			if effectiveDryRun {
				auditMode = "dry_run"
			}
			cmdID, runErr := runner.runOp(cmd.Context(), 1, op)
			if cmdID == "" {
				// runOp short-circuited before assigning a cmdID (e.g. validation
				// error). Synthesize one so the audit entry is still queryable.
				cmdID = newCommandID(p.Command, a.deps.Now().UTC())
			}
			if runErr != nil {
				a.auditMutation(cmd, cmdID, "error", auditMode, "", 0, "", nil)
				return fmt.Errorf("execute: %w", runErr)
			}
			a.auditMutation(cmd, cmdID, "ok", auditMode, "", 0, "", nil)
			return nil
		},
	}

	return cmd
}
