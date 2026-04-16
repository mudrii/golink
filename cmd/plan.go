package cmd

// plan: generates a golink.plan/v1 document describing a mutating operation
// without calling LinkedIn. The document can be reviewed and then executed
// with `golink execute`.

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/mudrii/golink/internal/output"
	"github.com/mudrii/golink/internal/plan"
	"github.com/spf13/cobra"
)

func newPlanCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <command>",
		Short: "Generate a plan document for a mutating command without executing it",
		Long: `plan produces a golink.plan/v1 JSON document that describes a mutating
operation. No LinkedIn API calls are made.

Review the plan JSON, then execute it with:
  golink execute plan.json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newPlanPostCommand(a),
		newPlanCommentCommand(a),
		newPlanReactCommand(a),
	)
	return cmd
}

// newPlanPostCommand mirrors the `post` subcommands that are plannable.
func newPlanPostCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "post",
		Short: "Plan a post operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newPlanPostCreateCommand(a),
		newPlanPostDeleteCommand(a),
		newPlanPostEditCommand(a),
		newPlanPostReshareCommand(a),
		newPlanPostScheduleCommand(a),
	)
	return cmd
}

func newPlanPostCreateCommand(a *app) *cobra.Command {
	var text, visibility, media, asOrg string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Plan a post create",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if text == "" {
				return a.validationFailure(cmd, "missing required flag: --text", "")
			}
			if asOrg != "" && !strings.HasPrefix(asOrg, "urn:li:organization:") {
				return a.validationFailure(cmd, "invalid --as-org value", "--as-org must be a urn:li:organization:... URN")
			}
			args := map[string]any{"text": text}
			if visibility != "" {
				args["visibility"] = visibility
			}
			if media != "" {
				args["media"] = media
			}
			if asOrg != "" {
				args["author_urn"] = asOrg
			}
			return emitPlan(a, cmd, "post create", args)
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "post body text")
	cmd.Flags().StringVar(&visibility, "visibility", "PUBLIC", "visibility: PUBLIC|CONNECTIONS|LOGGED_IN")
	cmd.Flags().StringVar(&media, "media", "", "media URN")
	cmd.Flags().StringVar(&asOrg, "as-org", "", "post as an organization (urn:li:organization:...)")
	return cmd
}

func newPlanPostDeleteCommand(a *app) *cobra.Command {
	var postURN string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Plan a post delete",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if postURN == "" {
				return a.validationFailure(cmd, "missing required flag: --post-urn", "")
			}
			return emitPlan(a, cmd, "post delete", map[string]any{"post_urn": postURN})
		},
	}
	cmd.Flags().StringVar(&postURN, "post-urn", "", "URN of the post to delete")
	return cmd
}

func newPlanPostEditCommand(a *app) *cobra.Command {
	var postURN, text, visibility string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Plan a post edit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if postURN == "" {
				return a.validationFailure(cmd, "missing required flag: --post-urn", "")
			}
			args := map[string]any{"post_urn": postURN}
			if text != "" {
				args["text"] = text
			}
			if visibility != "" {
				args["visibility"] = visibility
			}
			return emitPlan(a, cmd, "post edit", args)
		},
	}
	cmd.Flags().StringVar(&postURN, "post-urn", "", "URN of the post to edit")
	cmd.Flags().StringVar(&text, "text", "", "new text")
	cmd.Flags().StringVar(&visibility, "visibility", "", "new visibility")
	return cmd
}

func newPlanPostReshareCommand(a *app) *cobra.Command {
	var parentURN, commentary, visibility string
	cmd := &cobra.Command{
		Use:   "reshare",
		Short: "Plan a post reshare",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if parentURN == "" {
				return a.validationFailure(cmd, "missing required flag: --parent-urn", "")
			}
			args := map[string]any{"parent_urn": parentURN}
			if commentary != "" {
				args["commentary"] = commentary
			}
			if visibility != "" {
				args["visibility"] = visibility
			}
			return emitPlan(a, cmd, "post reshare", args)
		},
	}
	cmd.Flags().StringVar(&parentURN, "parent-urn", "", "URN of the post to reshare")
	cmd.Flags().StringVar(&commentary, "commentary", "", "optional commentary")
	cmd.Flags().StringVar(&visibility, "visibility", "PUBLIC", "visibility")
	return cmd
}

func newPlanPostScheduleCommand(a *app) *cobra.Command {
	var text, visibility, at, imagePath, imageAlt string
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Plan a post schedule",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if text == "" {
				return a.validationFailure(cmd, "missing required flag: --text", "")
			}
			if at == "" {
				return a.validationFailure(cmd, "missing required flag: --at", "")
			}
			args := map[string]any{"text": text, "at": at}
			if visibility != "" {
				args["visibility"] = visibility
			}
			if imagePath != "" {
				args["image_path"] = imagePath
			}
			if imageAlt != "" {
				args["image_alt"] = imageAlt
			}
			return emitPlan(a, cmd, "post schedule", args)
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "post body text")
	cmd.Flags().StringVar(&visibility, "visibility", "PUBLIC", "visibility")
	cmd.Flags().StringVar(&at, "at", "", "RFC3339 scheduled time")
	cmd.Flags().StringVar(&imagePath, "image", "", "absolute path to image file")
	cmd.Flags().StringVar(&imageAlt, "image-alt", "", "image alt text")
	return cmd
}

func newPlanCommentCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Plan a comment operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newPlanCommentAddCommand(a))
	return cmd
}

func newPlanCommentAddCommand(a *app) *cobra.Command {
	var postURN, text string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Plan a comment add",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if postURN == "" {
				return a.validationFailure(cmd, "missing required flag: --post-urn", "")
			}
			if text == "" {
				return a.validationFailure(cmd, "missing required flag: --text", "")
			}
			return emitPlan(a, cmd, "comment add", map[string]any{"post_urn": postURN, "text": text})
		},
	}
	cmd.Flags().StringVar(&postURN, "post-urn", "", "URN of the post to comment on")
	cmd.Flags().StringVar(&text, "text", "", "comment text")
	return cmd
}

func newPlanReactCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "react",
		Short: "Plan a react operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newPlanReactAddCommand(a))
	return cmd
}

func newPlanReactAddCommand(a *app) *cobra.Command {
	var postURN, reactionType string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Plan a react add",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if postURN == "" {
				return a.validationFailure(cmd, "missing required flag: --post-urn", "")
			}
			args := map[string]any{"post_urn": postURN}
			if reactionType != "" {
				args["type"] = reactionType
			}
			return emitPlan(a, cmd, "react add", args)
		},
	}
	cmd.Flags().StringVar(&postURN, "post-urn", "", "URN of the post to react to")
	cmd.Flags().StringVar(&reactionType, "type", "LIKE", "reaction type: LIKE|PRAISE|EMPATHY|INTEREST|APPRECIATION|ENTERTAINMENT")
	return cmd
}

// emitPlan builds and writes the plan envelope to stdout.
func emitPlan(a *app, cmd *cobra.Command, command string, args map[string]any) error {
	ikey, _ := cmd.Flags().GetString("idempotency-key")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	notes, _ := cmd.Flags().GetString("notes")

	transport := a.settings.Transport
	if transport == "" {
		transport = "official"
	}
	profile := a.settings.Profile
	if profile == "" {
		profile = "default"
	}

	p := plan.Plan{
		Schema:         plan.SchemaV1,
		CreatedAt:      a.deps.Now().UTC(),
		Command:        command,
		Args:           args,
		Transport:      transport,
		Profile:        profile,
		IdempotencyKey: ikey,
		DryRun:         dryRun,
		Notes:          notes,
	}

	// Serialize to map[string]any for the PlanData envelope args field.
	planBytes, err := json.Marshal(p)
	if err != nil {
		return a.transportFailure(cmd, "failed to marshal plan", err.Error())
	}
	var planData output.PlanData
	if err := json.Unmarshal(planBytes, &planData); err != nil {
		return a.transportFailure(cmd, "failed to build plan data", err.Error())
	}

	meta := a.metadata(cmd, output.StatusOK)
	meta.Command = "plan"
	meta.GeneratedAt = time.Now().UTC()

	envelope := output.Success(meta, planData)
	return output.WriteJSON(a.deps.Stdout, envelope)
}
