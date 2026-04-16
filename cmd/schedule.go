package cmd

// golink schedule — client-side post queue.
//
// LinkedIn has no native scheduled-post API. This family manages a local queue
// processed on demand. Operators invoke `schedule run` via cron, launchd, or
// an agent loop; golink does NOT spawn a background daemon.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/output"
	"github.com/mudrii/golink/internal/schedule"
	"github.com/spf13/cobra"
)

func newScheduleCommand(a *app) *cobra.Command {
	schedCmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage the client-side post schedule queue",
		Long: `Client-side post scheduling queue.

LinkedIn has no native scheduled-post API. golink stores pending posts on disk
and executes them on demand. There is NO background daemon — invoke
'golink schedule run' via cron, launchd, or an agent loop.

Example crontab entry (runs every 5 minutes):
  */5 * * * * /usr/local/bin/golink schedule run --limit 20

NOTE: --require-approval cannot be combined with post schedule in this release.`,
	}

	schedCmd.AddCommand(
		newScheduleListCommand(a),
		newScheduleShowCommand(a),
		newScheduleRunCommand(a),
		newScheduleCancelCommand(a),
		newScheduleNextCommand(a),
	)

	return schedCmd
}

func newScheduleListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show pending, past-due, and recently-completed schedule entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			entries, err := a.deps.ScheduleStore.List(cmd.Context())
			if err != nil {
				return a.transportFailure(cmd, "failed to list schedule entries", err.Error())
			}

			now := a.deps.Now().UTC()
			counts := output.ScheduleListCounts{}
			items := make([]output.ScheduledPostItem, 0, len(entries))
			for i := range entries {
				e := &entries[i]
				switch e.State {
				case schedule.StatePending:
					if e.ScheduledAt.Before(now) {
						counts.PastDue++
					} else {
						counts.Pending++
					}
				case schedule.StateCompleted:
					counts.Completed++
				case schedule.StateFailed:
					counts.Failed++
				case schedule.StateCancelled:
					counts.Cancelled++
				}
				items = append(items, entryToItem(*e))
			}

			data := output.ScheduleListData{
				Items:  items,
				Counts: counts,
			}
			return a.writeSuccess(cmd, data, fmt.Sprintf("%d schedule entries", len(items)))
		},
	}
}

func newScheduleShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <command_id>",
		Short: "Print the stored request for a scheduled entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandID := strings.TrimSpace(args[0])
			if commandID == "" {
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}

			e, err := a.deps.ScheduleStore.Get(cmd.Context(), commandID)
			if err != nil {
				if errors.Is(err, schedule.ErrNotFound) {
					return a.notFoundFailure(cmd, "schedule entry not found", commandID)
				}
				return a.transportFailure(cmd, "failed to get schedule entry", err.Error())
			}

			return a.writeSuccess(cmd, entryToItem(e), fmt.Sprintf("schedule entry: %s", e.CommandID))
		},
	}
}

func newScheduleRunCommand(a *app) *cobra.Command {
	var (
		flagLimit    int
		flagFailFast bool
	)

	cmd := &cobra.Command{
		Use:   "run [<command_id>]",
		Short: "Execute past-due scheduled entries through the normal Posts API path",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			now := a.deps.Now().UTC()

			var toRun []schedule.Entry

			if len(args) == 1 {
				// Run a specific entry by ID (even if not yet past-due).
				commandID := strings.TrimSpace(args[0])
				e, err := a.deps.ScheduleStore.Get(ctx, commandID)
				if err != nil {
					if errors.Is(err, schedule.ErrNotFound) {
						return a.notFoundFailure(cmd, "schedule entry not found", commandID)
					}
					return a.transportFailure(cmd, "failed to get schedule entry", err.Error())
				}
				if e.State != schedule.StatePending && e.State != schedule.StateFailed {
					return a.validationFailure(cmd,
						fmt.Sprintf("entry %s is in state %s, only pending/failed entries can be run", commandID, e.State), "")
				}
				// Transition failed → pending in the store so MarkRunning (which
				// requires pending) will accept the entry during runOneEntry.
				if e.State == schedule.StateFailed {
					if retryErr := a.deps.ScheduleStore.MarkRetrying(ctx, commandID); retryErr != nil {
						return a.transportFailure(cmd, "failed to reset schedule entry for retry", retryErr.Error())
					}
					e.State = schedule.StatePending
					e.LastError = ""
				}
				toRun = []schedule.Entry{e}
			} else {
				// Run all past-due pending entries.
				due, err := a.deps.ScheduleStore.Due(ctx, now, flagLimit)
				if err != nil {
					return a.transportFailure(cmd, "failed to query due schedule entries", err.Error())
				}
				if len(due) == 0 {
					data := output.ScheduleRunData{
						Ran:     0,
						Results: []output.ScheduleRunResult{},
					}
					return a.writeSuccess(cmd, data, "no past-due entries to run")
				}
				toRun = due
			}

			// Resolve session + transport once for the entire run.
			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(ctx, session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}

			results := make([]output.ScheduleRunResult, 0, len(toRun))
			succeeded, failed := 0, 0

			for i := range toRun {
				result, runErr := runOneEntry(ctx, a, cmd, toRun[i], transport, session.MemberURN)
				results = append(results, result)
				if runErr != nil {
					failed++
					if flagFailFast {
						break
					}
				} else {
					succeeded++
				}
			}

			data := output.ScheduleRunData{
				Ran:       len(results),
				Succeeded: succeeded,
				Failed:    failed,
				Skipped:   0,
				Results:   results,
			}
			return a.writeSuccess(cmd, data,
				fmt.Sprintf("ran %d entries: %d succeeded, %d failed", len(results), succeeded, failed))
		},
	}

	cmd.Flags().IntVar(&flagLimit, "limit", 50, "maximum number of entries to run")
	cmd.Flags().BoolVar(&flagFailFast, "fail-fast", false, "stop at first error (default: continue)")

	return cmd
}

// runOneEntry executes a single scheduled post entry. On success it moves the
// entry to completed. On failure it marks the entry failed with last_error.
// It returns the ScheduleRunResult and an error when the entry failed.
func runOneEntry(
	_ context.Context,
	a *app,
	cmd *cobra.Command,
	e schedule.Entry,
	transport api.Transport,
	memberURN string,
) (output.ScheduleRunResult, error) {
	cobCtx := cmd.Context()

	cmdID := newCommandID(commandName(cmd), a.deps.Now().UTC())

	if err := a.deps.ScheduleStore.MarkRunning(cobCtx, e.CommandID); err != nil {
		// Entry could not transition to running (e.g. concurrent runner already
		// claimed it). Report as skipped; no audit entry since we didn't act.
		return output.ScheduleRunResult{
			CommandID: e.CommandID,
			Status:    "skipped",
			Error:     fmt.Sprintf("cannot transition to running: %v", err),
		}, err
	}

	createReq := api.CreatePostRequest{
		Text:       e.Request.Text,
		Visibility: output.Visibility(e.Request.Visibility),
		Media:      "",
	}

	// Image upload when image_path is set. MarkRunning has already succeeded,
	// so MarkFailed (which requires running) is a valid transition here.
	if e.Request.ImagePath != "" {
		if _, statErr := os.Stat(e.Request.ImagePath); statErr != nil {
			errMsg := fmt.Sprintf("image not found: %v", statErr)
			_ = a.deps.ScheduleStore.MarkFailed(cobCtx, e.CommandID, errMsg, a.deps.Now().UTC())
			a.auditMutation(cmd, cmdID, "error", "normal", "", 0, string(output.ErrorCodeValidation), nil)
			return output.ScheduleRunResult{
				CommandID: e.CommandID,
				Status:    "failed",
				Error:     errMsg,
			}, statErr
		}

		uploadURL, imageURN, initErr := transport.InitializeImageUpload(cobCtx, memberURN)
		if initErr != nil {
			_ = a.deps.ScheduleStore.MarkFailed(cobCtx, e.CommandID, initErr.Error(), a.deps.Now().UTC())
			a.auditMutation(cmd, cmdID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
			return output.ScheduleRunResult{
				CommandID: e.CommandID,
				Status:    "failed",
				Error:     initErr.Error(),
			}, initErr
		}
		if uploadErr := transport.UploadImageBinary(cobCtx, uploadURL, e.Request.ImagePath); uploadErr != nil {
			_ = a.deps.ScheduleStore.MarkFailed(cobCtx, e.CommandID, uploadErr.Error(), a.deps.Now().UTC())
			a.auditMutation(cmd, cmdID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
			return output.ScheduleRunResult{
				CommandID: e.CommandID,
				Status:    "failed",
				Error:     uploadErr.Error(),
			}, uploadErr
		}
		createReq.MediaPayload = &api.MediaPayload{
			ID:  imageURN,
			Alt: e.Request.ImageAlt,
		}
	}

	// Idempotency check before creating.
	if e.IdempotencyKey != "" {
		entry, hit, _ := a.deps.IdempotencyStore.Lookup(cobCtx, e.IdempotencyKey, "post create")
		if hit {
			// Already ran — mark completed and return cached urn.
			_ = a.deps.ScheduleStore.MarkCompleted(cobCtx, e.CommandID)
			a.auditMutation(cmd, cmdID, "ok", "normal", entry.RequestID, entry.HTTPStatus, "", nil)
			return output.ScheduleRunResult{
				CommandID: e.CommandID,
				Status:    "succeeded",
				PostURN:   entry.RequestID,
			}, nil
		}
	}

	summary, err := transport.CreatePost(cobCtx, createReq)
	if err != nil {
		errMsg := err.Error()
		_ = a.deps.ScheduleStore.MarkFailed(cobCtx, e.CommandID, errMsg, a.deps.Now().UTC())
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
		return output.ScheduleRunResult{
			CommandID: e.CommandID,
			Status:    "failed",
			Error:     errMsg,
		}, err
	}

	_ = a.deps.ScheduleStore.MarkCompleted(cobCtx, e.CommandID)
	a.auditMutation(cmd, cmdID, "ok", "normal", summary.ID, 201, "", nil)

	return output.ScheduleRunResult{
		CommandID: e.CommandID,
		Status:    "succeeded",
		PostURN:   summary.ID,
	}, nil
}

func newScheduleCancelCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <command_id>",
		Short: "Delete a pending entry without running it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandID := strings.TrimSpace(args[0])
			if commandID == "" {
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}

			e, err := a.deps.ScheduleStore.Get(cmd.Context(), commandID)
			if err != nil {
				if errors.Is(err, schedule.ErrNotFound) {
					return a.notFoundFailure(cmd, "schedule entry not found", commandID)
				}
				return a.transportFailure(cmd, "failed to get schedule entry", err.Error())
			}

			if err := a.deps.ScheduleStore.MarkCancelled(cmd.Context(), commandID); err != nil {
				if errors.Is(err, schedule.ErrInvalidState) {
					return a.validationFailure(cmd, "cannot cancel entry in current state", err.Error())
				}
				return a.transportFailure(cmd, "failed to cancel schedule entry", err.Error())
			}

			a.auditMutation(cmd, commandID, "cancelled", "normal", "", 0, "", nil)

			// Return the entry as it was before cancellation (state field updated).
			e.State = schedule.StateCancelled
			data := entryToData(e)
			return a.writeSuccess(cmd, data, fmt.Sprintf("schedule entry cancelled: %s", commandID))
		},
	}
}

func newScheduleNextCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Print the next pending entry's scheduled time (for cron/wakeup integration)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := a.deps.ScheduleStore.Next(cmd.Context())
			if err != nil {
				if errors.Is(err, schedule.ErrNotFound) {
					// No pending entries — return a helpful unsupported envelope.
					return a.writeUnsupported(cmd, output.UnsupportedPayload{
						Feature: "schedule next",
						Reason:  "no pending schedule entries",
					}, "no pending schedule entries")
				}
				return a.transportFailure(cmd, "failed to query schedule", err.Error())
			}

			return a.writeSuccess(cmd, entryToItem(e),
				fmt.Sprintf("next scheduled: %s at %s", e.CommandID, e.ScheduledAt.UTC().Format(time.RFC3339)))
		},
	}
}

// entryToItem converts a schedule.Entry to the output item type (with retry_count).
func entryToItem(e schedule.Entry) output.ScheduledPostItem {
	item := output.ScheduledPostItem{
		CommandID:      e.CommandID,
		State:          output.ScheduleState(e.State),
		ScheduledAt:    e.ScheduledAt,
		CreatedAt:      e.CreatedAt,
		LastError:      e.LastError,
		RetryCount:     e.RetryCount,
		Profile:        e.Profile,
		Transport:      e.Transport,
		IdempotencyKey: e.IdempotencyKey,
		Request: output.ScheduleRequest{
			Text:       e.Request.Text,
			Visibility: output.Visibility(e.Request.Visibility),
			ImagePath:  e.Request.ImagePath,
			ImageAlt:   e.Request.ImageAlt,
		},
	}
	if e.LastRunAt != nil {
		t := *e.LastRunAt
		item.LastRunAt = &t
	}
	return item
}

// entryToData converts a schedule.Entry to the create/cancel response type (no retry_count).
func entryToData(e schedule.Entry) output.ScheduledPostData {
	data := output.ScheduledPostData{
		CommandID:      e.CommandID,
		State:          output.ScheduleState(e.State),
		ScheduledAt:    e.ScheduledAt,
		CreatedAt:      e.CreatedAt,
		LastError:      e.LastError,
		Profile:        e.Profile,
		Transport:      e.Transport,
		IdempotencyKey: e.IdempotencyKey,
		Request: output.ScheduleRequest{
			Text:       e.Request.Text,
			Visibility: output.Visibility(e.Request.Visibility),
			ImagePath:  e.Request.ImagePath,
			ImageAlt:   e.Request.ImageAlt,
		},
	}
	if e.LastRunAt != nil {
		t := *e.LastRunAt
		data.LastRunAt = &t
	}
	return data
}
