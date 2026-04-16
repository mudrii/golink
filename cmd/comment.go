package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

func newCommentCommand(a *app) *cobra.Command {
	commentCmd := &cobra.Command{
		Use:   "comment",
		Short: "Manage LinkedIn comments",
	}

	commentCmd.AddCommand(
		newCommentAddCommand(a),
		newCommentListCommand(a),
	)

	return commentCmd
}

func newCommentAddCommand(a *app) *cobra.Command {
	var text string

	cmd := &cobra.Command{
		Use:         "add <post_urn>",
		Short:       "Add a comment",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{"audit": "mutating"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdID := newCommandID(commandName(cmd), a.deps.Now().UTC())
			ikey, _ := cmd.Flags().GetString("idempotency-key")

			postURN := trimmedText(args[0])
			if postURN == "" {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "missing required argument: post_urn", "comment add requires a post URN")
			}
			textValue := trimmedText(text)
			if textValue == "" {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "missing required flag: --text", "comment add requires --text")
			}
			if len(textValue) > 1250 {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "invalid --text length", "text must be between 1 and 1250 characters")
			}

			if a.settings.DryRun {
				data := output.CommentAddDryRunData{
					WouldComment: output.CommentAddPreview{
						Endpoint: "POST /rest/socialActions/" + postURN + "/comments",
						PostURN:  postURN,
						Text:     textValue,
					},
					Mode: "dry_run",
				}
				preview, _ := json.Marshal(data)
				writeErr := a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN POST /rest/socialActions/%s/comments", postURN))
				a.auditMutation(cmd, cmdID, "ok", "dry_run", "", 0, "", preview)
				return writeErr
			}

			if cached, hit, checkErr := a.idempotencyCheck(cmd, ikey, "comment add"); hit {
				var data output.CommentAddData
				if decErr := json.Unmarshal(cached.Result, &data); decErr == nil {
					a.auditMutation(cmd, cmdID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
					return a.writeSuccessFromCache(cmd, data, fmt.Sprintf("comment added (cached): %s", data.ID))
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
			comment, err := transport.AddComment(cmd.Context(), postURN, textValue)
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
				return a.mapTransportError(cmd, "comment add", err)
			}

			data := output.CommentAddData{CommentData: *comment}
			if ikey != "" {
				resultBytes, _ := json.Marshal(data)
				a.idempotencyRecord(cmd.Context(), idempotency.Entry{
					TS:         a.deps.Now().UTC(),
					Key:        ikey,
					Command:    "comment add",
					CommandID:  cmdID,
					Status:     "ok",
					HTTPStatus: 201,
					Result:     resultBytes,
				})
			}
			writeErr := a.writeSuccess(cmd, data, fmt.Sprintf("comment added: %s", comment.ID))
			a.auditMutation(cmd, cmdID, "ok", "normal", "", 201, "", nil)
			return writeErr
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "comment text")

	return cmd
}

func newCommentListCommand(a *app) *cobra.Command {
	var count int
	var start int

	cmd := &cobra.Command{
		Use:   "list <post_urn>",
		Short: "List comments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			postURN := trimmedText(args[0])
			if postURN == "" {
				return a.validationFailure(cmd, "missing required argument: post_urn", "comment list requires a post URN")
			}
			if count < 1 || count > 50 {
				return a.validationFailure(cmd, "invalid --count", "count must be between 1 and 50")
			}
			if start < 0 {
				return a.validationFailure(cmd, "invalid --start", "start must be greater than or equal to zero")
			}

			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}
			data, err := transport.ListComments(cmd.Context(), postURN, count, start)
			if err != nil {
				return a.mapTransportError(cmd, "comment list", err)
			}
			return a.writeSuccess(cmd, data, fmt.Sprintf("%d comments on %s", len(data.Items), postURN))
		},
	}
	cmd.Flags().IntVar(&count, "count", 10, "page size")
	cmd.Flags().IntVar(&start, "start", 0, "offset")

	return cmd
}
