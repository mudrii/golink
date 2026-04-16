package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/mudrii/golink/internal/idempotency"
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
		Use:         "add <post_urn>",
		Short:       "Add a reaction",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{"audit": "mutating"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdID := newCommandID(commandName(cmd), a.deps.Now().UTC())
			ikey, _ := cmd.Flags().GetString("idempotency-key")

			postURN := trimmedText(args[0])
			if postURN == "" {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "missing required argument: post_urn", "react add requires a post URN")
			}
			parsed, err := output.ParseReactionType(reactionType)
			if err != nil {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
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
				preview, _ := json.Marshal(data)
				writeErr := a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN POST /rest/reactions post=%s type=%s", postURN, parsed))
				a.auditMutation(cmd, cmdID, "ok", "dry_run", "", 0, "", preview)
				return writeErr
			}

			if cached, hit, checkErr := a.idempotencyCheck(cmd, ikey, "react add"); hit {
				var data output.ReactionAddData
				if decErr := json.Unmarshal(cached.Result, &data); decErr == nil {
					a.auditMutation(cmd, cmdID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
					return a.writeSuccessFromCache(cmd, data, fmt.Sprintf("reaction %s added (cached) to %s", data.Type, postURN))
				}
			} else if checkErr != nil {
				return checkErr
			}

			session, err := a.resolveSession(cmd)
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "UNAUTHORIZED", nil)
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}
			data, err := transport.AddReaction(cmd.Context(), postURN, parsed)
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
				return a.mapTransportError(cmd, "react add", err)
			}
			result := output.ReactionAddData{ReactionData: *data, TargetURN: postURN}
			if ikey != "" {
				resultBytes, _ := json.Marshal(result)
				a.idempotencyRecord(cmd.Context(), idempotency.Entry{
					TS:         a.deps.Now().UTC(),
					Key:        ikey,
					Command:    "react add",
					CommandID:  cmdID,
					Status:     "ok",
					HTTPStatus: 201,
					Result:     resultBytes,
				})
			}
			writeErr := a.writeSuccess(cmd, result, fmt.Sprintf("reaction %s added to %s", data.Type, postURN))
			a.auditMutation(cmd, cmdID, "ok", "normal", "", 201, "", nil)
			return writeErr
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
