package cmd

import (
	"fmt"

	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

func newReactCommand(a *app) *cobra.Command {
	reactCmd := &cobra.Command{
		Use:   "react",
		Short: "Manage LinkedIn reactions",
	}

	reactCmd.AddCommand(
		newReactAddCommand(a),
		newReactListCommand(a),
	)

	return reactCmd
}

func newReactAddCommand(a *app) *cobra.Command {
	var reactionType string

	cmd := &cobra.Command{
		Use:   "add <post_urn>",
		Short: "Add a reaction",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			postURN := trimmedText(args[0])
			if postURN == "" {
				return a.validationFailure(cmd, "missing required argument: post_urn", "react add requires a post URN")
			}
			parsed, err := output.ParseReactionType(reactionType)
			if err != nil {
				return a.validationFailure(cmd, "invalid --type", err.Error())
			}

			if a.settings.DryRun {
				data := output.ReactionAddDryRunData{
					WouldReact: output.ReactionAddPreview{
						Endpoint: "POST /rest/reactions",
						PostURN:  postURN,
						Type:     parsed,
					},
					Mode: "dry_run",
				}
				return a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN POST /rest/reactions post=%s type=%s", postURN, parsed))
			}

			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}
			data, err := transport.AddReaction(cmd.Context(), postURN, parsed)
			if err != nil {
				return a.mapTransportError(cmd, "react add", err)
			}
			return a.writeSuccess(cmd, output.ReactionAddData{ReactionData: *data, TargetURN: postURN}, fmt.Sprintf("reaction %s added to %s", data.Type, postURN))
		},
	}
	cmd.Flags().StringVar(&reactionType, "type", string(output.ReactionLike), "reaction type")

	return cmd
}

func newReactListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list <post_urn>",
		Short: "List reactions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			postURN := trimmedText(args[0])
			if postURN == "" {
				return a.validationFailure(cmd, "missing required argument: post_urn", "react list requires a post URN")
			}
			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}
			data, err := transport.ListReactions(cmd.Context(), postURN)
			if err != nil {
				return a.mapTransportError(cmd, "react list", err)
			}
			return a.writeSuccess(cmd, data, fmt.Sprintf("%d reactions on %s", data.Count, postURN))
		},
	}
}
