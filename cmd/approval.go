package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/approval"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

func newApprovalCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approval",
		Short: "Manage staged approval requests",
		Long: `approval manages requests staged by --require-approval.

Operators review staged files, then grant or deny them:

  golink approval list
  golink approval show <command_id>
  golink approval grant <command_id>
  golink approval run   <command_id>
  golink approval deny  <command_id>
  golink approval cancel <command_id>

--require-approval always stages and exits 3, even in a TTY. This is
intentional: the flag means "never execute without explicit sign-off".`,
	}

	cmd.AddCommand(
		newApprovalListCommand(a),
		newApprovalShowCommand(a),
		newApprovalGrantCommand(a),
		newApprovalDenyCommand(a),
		newApprovalRunCommand(a),
		newApprovalCancelCommand(a),
	)

	return cmd
}

func newApprovalListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending, approved, denied, and completed approvals",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := a.deps.ApprovalStore.List(cmd.Context())
			if err != nil {
				return a.transportFailure(cmd, "approval list failed", err.Error())
			}
			if items == nil {
				items = []approval.ListItem{}
			}

			out := make([]output.ApprovalListItem, 0, len(items))
			for _, it := range items {
				out = append(out, output.ApprovalListItem{
					CommandID:      it.CommandID,
					Command:        it.Command,
					State:          string(it.State),
					StagedAt:       it.StagedAt,
					IdempotencyKey: it.IdempotencyKey,
				})
			}
			data := output.ApprovalListData{Items: out}
			return a.writeSuccess(cmd, data, fmt.Sprintf("%d approval(s)", len(out)))
		},
	}
}

func newApprovalShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <command_id>",
		Short: "Show the staged payload for an approval entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandID := trimmedText(args[0])
			if commandID == "" {
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}
			entry, state, err := a.deps.ApprovalStore.Show(cmd.Context(), commandID)
			if err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					return a.validationFailure(cmd, "approval entry not found", commandID)
				}
				return a.transportFailure(cmd, "approval show failed", err.Error())
			}
			data := output.ApprovalShowData{
				Entry: entry,
				State: string(state),
			}
			return a.writeSuccess(cmd, data, fmt.Sprintf("%s (%s)", commandID, state))
		},
	}
}

func newApprovalGrantCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "grant <command_id>",
		Short: "Approve a staged request (pending → approved)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandID := trimmedText(args[0])
			if commandID == "" {
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}
			if err := a.deps.ApprovalStore.Grant(cmd.Context(), commandID); err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					return a.validationFailure(cmd, "approval entry not found or not pending", commandID)
				}
				return a.transportFailure(cmd, "approval grant failed", err.Error())
			}
			data := output.ApprovalStateChangeData{CommandID: commandID, State: string(approval.StateApproved)}
			return a.writeSuccess(cmd, data, fmt.Sprintf("granted: %s", commandID))
		},
	}
}

func newApprovalDenyCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "deny <command_id>",
		Short: "Deny a staged request (pending → denied)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandID := trimmedText(args[0])
			if commandID == "" {
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}
			if err := a.deps.ApprovalStore.Deny(cmd.Context(), commandID); err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					return a.validationFailure(cmd, "approval entry not found or not pending", commandID)
				}
				return a.transportFailure(cmd, "approval deny failed", err.Error())
			}
			data := output.ApprovalStateChangeData{CommandID: commandID, State: string(approval.StateDenied)}
			return a.writeSuccess(cmd, data, fmt.Sprintf("denied: %s", commandID))
		},
	}
}

func newApprovalCancelCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <command_id>",
		Short: "Remove a pending or approved entry without executing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandID := trimmedText(args[0])
			if commandID == "" {
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}
			if err := a.deps.ApprovalStore.Cancel(cmd.Context(), commandID); err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					return a.validationFailure(cmd, "approval entry not found or not cancellable", commandID)
				}
				return a.transportFailure(cmd, "approval cancel failed", err.Error())
			}
			data := output.ApprovalStateChangeData{CommandID: commandID, State: "cancelled"}
			return a.writeSuccess(cmd, data, fmt.Sprintf("cancelled: %s", commandID))
		},
	}
}

func newApprovalRunCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "run <command_id>",
		Short: "Execute an approved request via Transport",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandID := trimmedText(args[0])
			if commandID == "" {
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}

			entry, err := a.deps.ApprovalStore.LoadApproved(cmd.Context(), commandID)
			if err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					return a.validationFailure(cmd, "approval entry not found", commandID)
				}
				if errors.Is(err, approval.ErrWrongState) {
					return a.validationFailure(cmd, "approval entry is not in approved state", err.Error())
				}
				return a.transportFailure(cmd, "approval run: load failed", err.Error())
			}

			ikey := entry.IdempotencyKey

			// Idempotency replay check before dispatching.
			if ikey != "" {
				cached, hit, checkErr := a.idempotencyCheck(cmd, ikey, entry.Command)
				if checkErr != nil {
					return checkErr
				}
				if hit {
					_ = a.deps.ApprovalStore.Complete(cmd.Context(), commandID)
					a.auditMutation(cmd, commandID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
					var data any
					if len(cached.Result) > 0 {
						var raw any
						if err := json.Unmarshal(cached.Result, &raw); err == nil {
							data = raw
						}
					}
					return a.writeSuccessFromCache(cmd, data, fmt.Sprintf("approval run (cached): %s", commandID))
				}
			}

			session, transport, err := a.resolveStoredSessionAndTransport(cmd.Context(), cmd, entry.Profile, entry.Transport)
			if err != nil {
				return err
			}

			// Reconstruct payload from the entry. The payload is stored as any
			// (decoded from JSON), so we re-encode and decode into a typed map.
			payloadBytes, err := json.Marshal(entry.Payload)
			if err != nil {
				return a.validationFailure(cmd, "approval run: invalid payload", err.Error())
			}
			var payloadMap map[string]any
			if err := json.Unmarshal(payloadBytes, &payloadMap); err != nil {
				return a.validationFailure(cmd, "approval run: invalid payload", err.Error())
			}

			cmdName := entry.Command
			var resultData any
			var httpStatus int
			var auditAuthorURN string

			switch cmdName {
			case "post create":
				text, _ := payloadMap["text"].(string)
				visStr := "PUBLIC"
				if visValue, ok := payloadMap["visibility"]; ok {
					var ok bool
					visStr, ok = visValue.(string)
					if !ok {
						return a.validationFailure(cmd, "approval run: invalid visibility in payload", "expected string")
					}
					if visStr == "" {
						visStr = "PUBLIC"
					}
				}
				media, _ := payloadMap["media"].(string)
				authorURN, _ := payloadMap["author_urn"].(string)
				if text == "" {
					return a.validationFailure(cmd, "approval run: missing text in payload", "")
				}
				visibility, parseErr := output.ParseVisibility(visStr)
				if parseErr != nil {
					return a.validationFailure(cmd, "approval run: invalid visibility in payload", parseErr.Error())
				}
				createReq := api.CreatePostRequest{
					Text:       text,
					Visibility: visibility,
					Media:      media,
					AuthorURN:  authorURN,
				}
				if uploadPreview, ok := payloadMap["would_upload"].(map[string]any); ok {
					imagePath, _ := uploadPreview["path"].(string)
					imageAlt, _ := uploadPreview["alt"].(string)
					if imagePath == "" {
						return a.validationFailure(cmd, "approval run: missing image path in payload", "")
					}
					if _, statErr := os.Stat(imagePath); statErr != nil {
						return a.validationFailure(cmd, "cannot read image file", statErr.Error())
					}
					uploadURL, imageURN, initErr := transport.InitializeImageUpload(cmd.Context(), session.MemberURN)
					if initErr != nil {
						a.auditMutationWithAuthor(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil, authorURN)
						return a.mapTransportError(cmd, "post create image upload init", initErr)
					}
					if uploadErr := transport.UploadImageBinary(cmd.Context(), uploadURL, imagePath); uploadErr != nil {
						a.auditMutationWithAuthor(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil, authorURN)
						return a.mapTransportError(cmd, "post create image upload binary", uploadErr)
					}
					createReq.MediaPayload = &api.MediaPayload{
						ID:  imageURN,
						Alt: imageAlt,
					}
				}
				summary, runErr := transport.CreatePost(cmd.Context(), createReq)
				if runErr != nil {
					a.auditMutationWithAuthor(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil, authorURN)
					return a.mapTransportError(cmd, "post create", runErr)
				}
				resultData = output.PostCreateData{PostSummary: *summary}
				httpStatus = 201
				auditAuthorURN = authorURN

			case "post delete":
				postURN, _ := payloadMap["post_urn"].(string)
				if postURN == "" {
					return a.validationFailure(cmd, "approval run: missing post_urn in payload", "")
				}
				data, runErr := transport.DeletePost(cmd.Context(), postURN)
				if runErr != nil {
					a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
					return a.mapTransportError(cmd, "post delete", runErr)
				}
				resultData = data
				httpStatus = 204

			case "post edit":
				postURN, _ := payloadMap["post_urn"].(string)
				if postURN == "" {
					return a.validationFailure(cmd, "approval run: missing post_urn in payload", "")
				}
				editReq := api.EditPostRequest{PostURN: postURN}
				// Stored payload: {"patch": {"$set": {"commentary": "...", "visibility": "..."}}}
				if outer, ok := payloadMap["patch"].(map[string]any); ok {
					if inner, ok := outer["patch"].(map[string]any); ok {
						if set, ok := inner["$set"].(map[string]any); ok {
							if v, ok := set["commentary"].(string); ok {
								s := v
								editReq.Text = &s
							}
							if v, ok := set["visibility"].(string); ok {
								vis, perr := output.ParseVisibility(v)
								if perr == nil {
									editReq.Visibility = &vis
								} else {
									return a.validationFailure(cmd, "approval run: invalid visibility in payload", perr.Error())
								}
							} else if set["visibility"] != nil {
								return a.validationFailure(cmd, "approval run: invalid visibility in payload", "expected string")
							}
						}
					}
				}
				data, runErr := transport.EditPost(cmd.Context(), editReq)
				if runErr != nil {
					a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
					return a.mapTransportError(cmd, "post edit", runErr)
				}
				resultData = data
				httpStatus = 204

			case "post reshare":
				parentURN, _ := payloadMap["parent_urn"].(string)
				commentary, _ := payloadMap["commentary"].(string)
				visStr := "PUBLIC"
				if visValue, ok := payloadMap["visibility"]; ok {
					var ok bool
					visStr, ok = visValue.(string)
					if !ok {
						return a.validationFailure(cmd, "approval run: invalid visibility in payload", "expected string")
					}
					if visStr == "" {
						visStr = "PUBLIC"
					}
				}
				if parentURN == "" {
					return a.validationFailure(cmd, "approval run: missing parent_urn in payload", "")
				}
				visibility, parseErr := output.ParseVisibility(visStr)
				if parseErr != nil {
					return a.validationFailure(cmd, "approval run: invalid visibility in payload", parseErr.Error())
				}
				summary, runErr := transport.ResharePost(cmd.Context(), api.ResharePostRequest{
					ParentURN:  parentURN,
					Commentary: commentary,
					Visibility: visibility,
				})
				if runErr != nil {
					a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
					return a.mapTransportError(cmd, "post reshare", runErr)
				}
				resultData = output.PostCreateData{PostSummary: *summary}
				httpStatus = 201

			case "comment add":
				postURN, _ := payloadMap["post_urn"].(string)
				text, _ := payloadMap["text"].(string)
				if postURN == "" || text == "" {
					return a.validationFailure(cmd, "approval run: missing post_urn or text in payload", "")
				}
				comment, runErr := transport.AddComment(cmd.Context(), postURN, text)
				if runErr != nil {
					a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
					return a.mapTransportError(cmd, "comment add", runErr)
				}
				resultData = output.CommentAddData{CommentData: *comment}
				httpStatus = 201

			case "react add":
				postURN, _ := payloadMap["post_urn"].(string)
				rtStr := string(output.ReactionLike)
				if rtValue, ok := payloadMap["type"]; ok {
					var ok bool
					rtStr, ok = rtValue.(string)
					if !ok {
						return a.validationFailure(cmd, "approval run: invalid reaction type in payload", "expected string")
					}
				}
				if postURN == "" {
					return a.validationFailure(cmd, "approval run: missing post_urn in payload", "")
				}
				rtype, parseErr := output.ParseReactionType(rtStr)
				if parseErr != nil {
					return a.validationFailure(cmd, "approval run: invalid reaction type in payload", parseErr.Error())
				}
				data, runErr := transport.AddReaction(cmd.Context(), postURN, rtype)
				if runErr != nil {
					a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
					return a.mapTransportError(cmd, "react add", runErr)
				}
				resultData = output.ReactionAddData{ReactionData: *data, TargetURN: postURN}
				httpStatus = 201

			default:
				return a.validationFailure(cmd, fmt.Sprintf("approval run: unsupported command %q", cmdName), "supported: post create, post delete, post edit, post reshare, comment add, react add")
			}

			// Record idempotency on success.
			if ikey != "" {
				resultBytes, _ := json.Marshal(resultData)
				a.idempotencyRecord(cmd.Context(), idempotency.Entry{
					TS:         a.deps.Now().UTC(),
					Key:        ikey,
					Command:    cmdName,
					CommandID:  commandID,
					Status:     "ok",
					HTTPStatus: httpStatus,
					Result:     resultBytes,
				})
			}

			// Rename approved → completed; leave as approved on error so retries work.
			if completeErr := a.deps.ApprovalStore.Complete(cmd.Context(), commandID); completeErr != nil {
				a.logger.Warn("approval complete rename failed", "error", completeErr)
			}

			if auditAuthorURN != "" {
				a.auditMutationWithAuthor(cmd, commandID, "ok", "normal", "", httpStatus, "", nil, auditAuthorURN)
			} else {
				a.auditMutation(cmd, commandID, "ok", "normal", "", httpStatus, "", nil)
			}
			return a.writeSuccess(cmd, resultData, fmt.Sprintf("approval run: executed %s", cmdName))
		},
	}
}

func (a *app) resolveStoredSessionAndTransport(ctx context.Context, cmd *cobra.Command, profile, transport string) (auth.Session, api.Transport, error) {
	resolvedProfile := profile
	if resolvedProfile == "" {
		resolvedProfile = a.settings.Profile
	}

	session, err := a.deps.SessionStore.LoadSession(ctx, resolvedProfile)
	if err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return auth.Session{}, nil, a.authFailure(cmd,
				"Token expired or invalid. Re-run: golink auth login",
				fmt.Sprintf("no active session for stored profile %s", resolvedProfile))
		}
		return auth.Session{}, nil, a.transportFailure(cmd, "failed to resolve session", err.Error())
	}

	authenticated, err := session.IsAuthenticated(a.deps.Now())
	if err != nil {
		return auth.Session{}, nil, a.authFailure(cmd, "Token expired or invalid. Re-run: golink auth login", err.Error())
	}
	if !authenticated {
		return auth.Session{}, nil, a.authFailure(cmd,
			"Token expired or invalid. Re-run: golink auth login",
			fmt.Sprintf("session for stored profile %s has no usable access token", resolvedProfile))
	}

	settings := a.settings
	if profile != "" {
		settings.Profile = profile
	}
	if transport != "" {
		settings.Transport = transport
	}

	resolvedSession, refreshErr := a.maybeRefreshSession(ctx, *session)
	if refreshErr != nil {
		return auth.Session{}, nil, a.authFailure(cmd,
			"Token expired or invalid. Re-run: golink auth login",
			fmt.Sprintf("failed to refresh session: %v", refreshErr))
	}
	resolvedTransport, err := a.deps.TransportFactory(ctx, settings, resolvedSession, a.logger)
	if err != nil {
		return auth.Session{}, nil, a.transportFailure(cmd, "failed to build transport", err.Error())
	}

	return resolvedSession, resolvedTransport, nil
}
