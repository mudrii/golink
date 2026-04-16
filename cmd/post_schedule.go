package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mudrii/golink/internal/output"
	"github.com/mudrii/golink/internal/schedule"
	"github.com/spf13/cobra"
)

func newPostScheduleCommand(a *app) *cobra.Command {
	var (
		flagAt         string
		flagText       string
		flagVisibility string
		flagImage      string
		flagImageAlt   string
	)

	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Queue a post for later execution (no daemon — run via cron or golink schedule run)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmdID := newCommandID(commandName(cmd), a.deps.Now().UTC())
			ikey, _ := cmd.Flags().GetString("idempotency-key")

			// --require-approval is not supported in v1.
			if a.settings.RequireApproval {
				return a.validationFailure(cmd,
					"--require-approval is not supported with post schedule",
					"approval + schedule interaction is not implemented in this release")
			}

			// Validate --at.
			if flagAt == "" {
				return a.validationFailure(cmd, "missing required flag: --at", "--at <rfc3339> is required")
			}
			scheduledAt, err := time.Parse(time.RFC3339, flagAt)
			if err != nil {
				return a.validationFailure(cmd, "invalid --at value", "must be RFC3339, e.g. 2027-01-01T09:00:00Z")
			}
			scheduledAt = scheduledAt.UTC()
			// Reject times that are not at least 30s in the future.
			if !scheduledAt.After(a.deps.Now().UTC().Add(30 * time.Second)) {
				return a.validationFailure(cmd, "invalid --at value",
					"scheduled time must be at least 30 seconds in the future")
			}

			// Validate text.
			text := trimmedText(flagText)
			if text == "" {
				return a.validationFailure(cmd, "missing required flag: --text", "--text is required")
			}
			if len(text) < 5 || len(text) > 3000 {
				return a.validationFailure(cmd, "invalid --text length",
					"text must be between 5 and 3000 characters")
			}

			// Validate visibility.
			visibility, err := output.ParseVisibility(flagVisibility)
			if err != nil {
				return a.validationFailure(cmd, "invalid --visibility", err.Error())
			}

			// Validate image path: must be absolute if set.
			imagePath := strings.TrimSpace(flagImage)
			if imagePath != "" && !filepath.IsAbs(imagePath) {
				return a.validationFailure(cmd, "invalid --image path",
					"image path must be absolute (it resolves at run time regardless of CWD)")
			}

			entry := schedule.Entry{
				CommandID:      cmdID,
				State:          schedule.StatePending,
				ScheduledAt:    scheduledAt,
				CreatedAt:      a.deps.Now().UTC(),
				Profile:        a.settings.Profile,
				Transport:      a.settings.Transport,
				IdempotencyKey: ikey,
				Request: schedule.Request{
					Text:       text,
					Visibility: string(visibility),
					ImagePath:  imagePath,
					ImageAlt:   strings.TrimSpace(flagImageAlt),
				},
			}

			if err := a.deps.ScheduleStore.Add(cmd.Context(), entry); err != nil {
				return a.transportFailure(cmd, "failed to store scheduled post", err.Error())
			}

			a.auditMutation(cmd, cmdID, "scheduled", "normal", "", 0, "", nil)

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

			return a.writeSuccess(cmd, data,
				fmt.Sprintf("post scheduled: %s at %s", cmdID, scheduledAt.Format(time.RFC3339)))
		},
	}

	cmd.Flags().StringVar(&flagAt, "at", "", "scheduled time in RFC3339 format (required)")
	cmd.Flags().StringVar(&flagText, "text", "", "post text (required)")
	cmd.Flags().StringVar(&flagVisibility, "visibility", "PUBLIC", "PUBLIC|CONNECTIONS|LOGGED_IN")
	cmd.Flags().StringVar(&flagImage, "image", "", "absolute path to a local image to attach at run time")
	cmd.Flags().StringVar(&flagImageAlt, "image-alt", "", "alt text for the attached image")

	return cmd
}
