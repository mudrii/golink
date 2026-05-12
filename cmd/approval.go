package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

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
			auditID := commandID
			if auditID == "" {
				auditID = newCommandID(commandName(cmd), a.deps.Now().UTC())
			}
			if commandID == "" {
				a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeValidation), nil)
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}
			if err := a.deps.ApprovalStore.Grant(cmd.Context(), commandID); err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeNotFound), nil)
					return a.validationFailure(cmd, "approval entry not found or not pending", commandID)
				}
				if errors.Is(err, approval.ErrWrongState) {
					a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeValidation), nil)
					return a.validationFailure(cmd, "approval entry is in wrong state", err.Error())
				}
				a.auditMutation(cmd, auditID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
				return a.transportFailure(cmd, "approval grant failed", err.Error())
			}
			data := output.ApprovalStateChangeData{CommandID: commandID, State: string(approval.StateApproved)}
			a.auditMutation(cmd, auditID, "ok", "normal", "", 0, "", nil)
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
			auditID := commandID
			if auditID == "" {
				auditID = newCommandID(commandName(cmd), a.deps.Now().UTC())
			}
			if commandID == "" {
				a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeValidation), nil)
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}
			if err := a.deps.ApprovalStore.Deny(cmd.Context(), commandID); err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeNotFound), nil)
					return a.validationFailure(cmd, "approval entry not found or not pending", commandID)
				}
				if errors.Is(err, approval.ErrWrongState) {
					a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeValidation), nil)
					return a.validationFailure(cmd, "approval entry is in wrong state", err.Error())
				}
				a.auditMutation(cmd, auditID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
				return a.transportFailure(cmd, "approval deny failed", err.Error())
			}
			data := output.ApprovalStateChangeData{CommandID: commandID, State: string(approval.StateDenied)}
			a.auditMutation(cmd, auditID, "ok", "normal", "", 0, "", nil)
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
			auditID := commandID
			if auditID == "" {
				auditID = newCommandID(commandName(cmd), a.deps.Now().UTC())
			}
			if commandID == "" {
				a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeValidation), nil)
				return a.validationFailure(cmd, "missing required argument: command_id", "")
			}
			if err := a.deps.ApprovalStore.Cancel(cmd.Context(), commandID); err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeNotFound), nil)
					return a.validationFailure(cmd, "approval entry not found or not cancellable", commandID)
				}
				a.auditMutation(cmd, auditID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
				return a.transportFailure(cmd, "approval cancel failed", err.Error())
			}
			data := output.ApprovalStateChangeData{CommandID: commandID, State: "cancelled"}
			a.auditMutation(cmd, auditID, "ok", "normal", "", 0, "", nil)
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
			return a.runApprovalRunCommand(cmd, args[0])
		},
	}
}

func (a *app) runApprovalRunCommand(cmd *cobra.Command, rawCommandID string) error {
	commandID := trimmedText(rawCommandID)
	validate := a.approvalRunValidation(cmd, commandID)
	if commandID == "" {
		return validate("missing required argument: command_id", "")
	}

	entry, err := a.loadApprovedEntryForRun(cmd, commandID, validate)
	if err != nil {
		return err
	}
	if handled, err := a.replayApprovedIdempotency(cmd, commandID, entry); handled || err != nil {
		return err
	}

	session, transport, err := a.resolveApprovedRunTransport(cmd, commandID, entry)
	if err != nil {
		return err
	}
	payloadMap, err := approvalPayloadMap(entry.Payload)
	if err != nil {
		return validate("approval run: invalid payload", err.Error())
	}

	result, err := a.runApprovedCommand(cmd, approvedRunInput{
		commandID: commandID,
		command:   entry.Command,
		payload:   payloadMap,
		session:   session,
		transport: transport,
		validate:  validate,
	})
	if err != nil {
		return err
	}

	if err := a.recordApprovedIdempotency(cmd, commandID, entry, result); err != nil {
		return err
	}
	if err := a.completeApprovedRun(cmd, commandID); err != nil {
		return err
	}
	a.auditApprovedRunSuccess(cmd, commandID, result)
	return a.writeSuccess(cmd, result.data, fmt.Sprintf("approval run: executed %s", entry.Command))
}

func (a *app) approvalRunValidation(cmd *cobra.Command, commandID string) approvalValidationFunc {
	return func(message, details string) error {
		auditID := commandID
		if auditID == "" {
			auditID = newCommandID(commandName(cmd), a.deps.Now().UTC())
		}
		a.auditMutation(cmd, auditID, "validation_error", "normal", "", 0, string(output.ErrorCodeValidation), nil)
		return a.validationFailure(cmd, message, details)
	}
}

func (a *app) loadApprovedEntryForRun(cmd *cobra.Command, commandID string, validate approvalValidationFunc) (approval.Entry, error) {
	entry, err := a.deps.ApprovalStore.LoadApproved(cmd.Context(), commandID)
	if err == nil {
		return entry, nil
	}
	if errors.Is(err, approval.ErrNotFound) {
		return approval.Entry{}, validate("approval entry not found", commandID)
	}
	if errors.Is(err, approval.ErrWrongState) {
		return approval.Entry{}, validate("approval entry is not in approved state", err.Error())
	}
	return approval.Entry{}, a.transportFailure(cmd, "approval run: load failed", err.Error())
}

func (a *app) replayApprovedIdempotency(cmd *cobra.Command, commandID string, entry approval.Entry) (bool, error) {
	if entry.IdempotencyKey == "" {
		return false, nil
	}
	cached, hit, err := a.idempotencyCheck(cmd, entry.IdempotencyKey, entry.Command)
	if err != nil {
		a.auditMutation(cmd, commandID, "validation_error", "normal", "", 0, string(output.ErrorCodeValidation), nil)
		return true, err
	}
	if !hit {
		return false, nil
	}

	if err := a.completeApprovedRun(cmd, commandID); err != nil {
		return true, err
	}
	a.auditMutation(cmd, commandID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
	return true, a.writeSuccessFromCache(cmd, cachedApprovalData(cached), fmt.Sprintf("approval run (cached): %s", commandID))
}

func cachedApprovalData(cached idempotency.Entry) any {
	if len(cached.Result) == 0 {
		return nil
	}
	var data any
	if err := json.Unmarshal(cached.Result, &data); err != nil {
		return nil
	}
	return data
}

func (a *app) resolveApprovedRunTransport(cmd *cobra.Command, commandID string, entry approval.Entry) (auth.Session, api.Transport, error) {
	session, transport, err := a.resolveStoredSessionAndTransport(cmd.Context(), cmd, entry.Profile, entry.Transport)
	if err == nil {
		return session, transport, nil
	}
	var failure *commandFailure
	if errors.As(err, &failure) {
		status := "error"
		if failure.exitCode == 2 {
			status = "validation_error"
		}
		a.auditMutation(cmd, commandID, status, "normal", "", 0, failure.errCode, nil)
		return auth.Session{}, nil, err
	}
	a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
	return auth.Session{}, nil, err
}

func approvalPayloadMap(payload any) (map[string]any, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(payloadBytes, &payloadMap); err != nil {
		return nil, err
	}
	return payloadMap, nil
}

func (a *app) recordApprovedIdempotency(cmd *cobra.Command, commandID string, entry approval.Entry, result approvedRunResult) error {
	if entry.IdempotencyKey == "" {
		return nil
	}
	resultBytes, err := json.Marshal(result.data)
	if err != nil {
		a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
		return a.transportFailure(cmd, "approval run: cache result encode failed", err.Error())
	}
	a.idempotencyRecord(cmd.Context(), idempotency.Entry{
		TS:         a.deps.Now().UTC(),
		Key:        entry.IdempotencyKey,
		Command:    entry.Command,
		CommandID:  commandID,
		Status:     "ok",
		HTTPStatus: result.httpStatus,
		Result:     resultBytes,
	})
	return nil
}

func (a *app) completeApprovedRun(cmd *cobra.Command, commandID string) error {
	if err := a.deps.ApprovalStore.Complete(cmd.Context(), commandID); err != nil {
		a.logger.Warn("approval complete rename failed", "error", err)
		if cleanupErr := a.deps.ApprovalStore.Cancel(cmd.Context(), commandID); cleanupErr != nil {
			a.logger.Warn("approval cleanup after complete failure failed", "error", cleanupErr)
			a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
			return a.transportFailure(cmd, "approval run: complete failed", errors.Join(err, cleanupErr).Error())
		}
		a.auditMutation(cmd, commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
		return a.transportFailure(cmd, "approval run: complete failed", err.Error())
	}
	return nil
}

func (a *app) auditApprovedRunSuccess(cmd *cobra.Command, commandID string, result approvedRunResult) {
	if result.auditAuthorURN != "" {
		a.auditMutationWithAuthor(cmd, commandID, "ok", "normal", "", result.httpStatus, "", nil, result.auditAuthorURN)
		return
	}
	a.auditMutation(cmd, commandID, "ok", "normal", "", result.httpStatus, "", nil)
}

type approvalValidationFunc func(message, details string) error

type approvedRunInput struct {
	commandID string
	command   string
	payload   map[string]any
	session   auth.Session
	transport api.Transport
	validate  approvalValidationFunc
}

type approvedRunResult struct {
	data           any
	httpStatus     int
	auditAuthorURN string
}

func (a *app) runApprovedCommand(cmd *cobra.Command, in approvedRunInput) (approvedRunResult, error) {
	switch in.command {
	case "post create":
		return a.runApprovedPostCreate(cmd, in)
	case "post delete":
		return a.runApprovedPostDelete(cmd, in)
	case "post edit":
		return a.runApprovedPostEdit(cmd, in)
	case "post reshare":
		return a.runApprovedPostReshare(cmd, in)
	case "comment add":
		return a.runApprovedCommentAdd(cmd, in)
	case "react add":
		return a.runApprovedReactAdd(cmd, in)
	default:
		return approvedRunResult{}, in.validate(
			fmt.Sprintf("approval run: unsupported command %q", in.command),
			"supported: post create, post delete, post edit, post reshare, comment add, react add")
	}
}

func (a *app) runApprovedPostCreate(cmd *cobra.Command, in approvedRunInput) (approvedRunResult, error) {
	text, _ := in.payload["text"].(string)
	if text == "" {
		return approvedRunResult{}, in.validate("approval run: missing text in payload", "")
	}
	if len(text) < 5 || len(text) > 3000 {
		return approvedRunResult{}, in.validate("approval run: invalid text length", "text must be between 5 and 3000 characters")
	}

	visibility, err := approvedVisibility(in.payload, in.validate)
	if err != nil {
		return approvedRunResult{}, err
	}
	media, _ := in.payload["media"].(string)
	authorURN, _ := in.payload["author_urn"].(string)
	if err := validateApprovedAuthorURN(in.session, authorURN, in.validate); err != nil {
		return approvedRunResult{}, err
	}

	createReq := api.CreatePostRequest{
		Text:       text,
		Visibility: visibility,
		Media:      media,
		AuthorURN:  authorURN,
	}
	if err := a.attachApprovedImage(cmd, in, authorURN, &createReq); err != nil {
		return approvedRunResult{}, err
	}

	summary, err := in.transport.CreatePost(cmd.Context(), createReq)
	if err != nil {
		a.auditMutationWithAuthor(cmd, in.commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil, authorURN)
		return approvedRunResult{}, a.mapTransportError(cmd, "post create", err)
	}
	return approvedRunResult{
		data:           output.PostCreateData{PostSummary: *summary},
		httpStatus:     201,
		auditAuthorURN: authorURN,
	}, nil
}

func approvedVisibility(payload map[string]any, validate approvalValidationFunc) (output.Visibility, error) {
	visStr := "PUBLIC"
	if visValue, ok := payload["visibility"]; ok {
		var ok bool
		visStr, ok = visValue.(string)
		if !ok {
			return "", validate("approval run: invalid visibility in payload", "expected string")
		}
		if visStr == "" {
			visStr = "PUBLIC"
		}
	}
	visibility, err := output.ParseVisibility(visStr)
	if err != nil {
		return "", validate("approval run: invalid visibility in payload", err.Error())
	}
	return visibility, nil
}

func validateApprovedAuthorURN(session auth.Session, authorURN string, validate approvalValidationFunc) error {
	if authorURN == "" {
		return nil
	}
	if !strings.HasPrefix(authorURN, "urn:li:organization:") {
		return validate("approval run: invalid author_urn in payload", "author_urn must be a urn:li:organization:... URN")
	}
	if !sessionHasAnyScope(session, orgWriteScopes...) {
		return validate(
			fmt.Sprintf("posting as an organization requires %s", formatScopeRequirement(orgWriteScopes...)),
			"run `golink auth login` with the scope added to the LinkedIn app")
	}
	return nil
}

func (a *app) attachApprovedImage(cmd *cobra.Command, in approvedRunInput, authorURN string, req *api.CreatePostRequest) error {
	uploadPreview, ok := in.payload["would_upload"].(map[string]any)
	if !ok {
		return nil
	}
	imagePath, _ := uploadPreview["path"].(string)
	imageAlt, _ := uploadPreview["alt"].(string)
	if imagePath == "" {
		return in.validate("approval run: missing image path in payload", "")
	}
	if _, err := os.Stat(imagePath); err != nil {
		return in.validate("cannot read image file", err.Error())
	}
	uploadOwner := in.session.MemberURN
	if authorURN != "" {
		uploadOwner = authorURN
	}
	uploadURL, imageURN, err := in.transport.InitializeImageUpload(cmd.Context(), uploadOwner)
	if err != nil {
		a.auditMutationWithAuthor(cmd, in.commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil, authorURN)
		return a.mapTransportError(cmd, "post create image upload init", err)
	}
	if err := in.transport.UploadImageBinary(cmd.Context(), uploadURL, imagePath); err != nil {
		a.auditMutationWithAuthor(cmd, in.commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil, authorURN)
		return a.mapTransportError(cmd, "post create image upload binary", err)
	}
	req.MediaPayload = &api.MediaPayload{ID: imageURN, Alt: imageAlt}
	return nil
}

func (a *app) runApprovedPostDelete(cmd *cobra.Command, in approvedRunInput) (approvedRunResult, error) {
	postURN, _ := in.payload["post_urn"].(string)
	if postURN == "" {
		return approvedRunResult{}, in.validate("approval run: missing post_urn in payload", "")
	}
	data, err := in.transport.DeletePost(cmd.Context(), postURN)
	if err != nil {
		a.auditMutation(cmd, in.commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
		return approvedRunResult{}, a.mapTransportError(cmd, "post delete", err)
	}
	return approvedRunResult{data: data, httpStatus: 204}, nil
}

func (a *app) runApprovedPostEdit(cmd *cobra.Command, in approvedRunInput) (approvedRunResult, error) {
	postURN, _ := in.payload["post_urn"].(string)
	if postURN == "" {
		return approvedRunResult{}, in.validate("approval run: missing post_urn in payload", "")
	}
	editReq := api.EditPostRequest{PostURN: postURN}
	if err := applyApprovedEditPatch(&editReq, in.payload, in.validate); err != nil {
		return approvedRunResult{}, err
	}
	data, err := in.transport.EditPost(cmd.Context(), editReq)
	if err != nil {
		a.auditMutation(cmd, in.commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
		return approvedRunResult{}, a.mapTransportError(cmd, "post edit", err)
	}
	return approvedRunResult{data: data, httpStatus: 204}, nil
}

func applyApprovedEditPatch(req *api.EditPostRequest, payload map[string]any, validate approvalValidationFunc) error {
	outer, ok := payload["patch"].(map[string]any)
	if !ok {
		return nil
	}
	set, ok := outer["$set"].(map[string]any)
	if !ok {
		return nil
	}
	if v, ok := set["commentary"].(string); ok {
		req.Text = &v
	}
	if v, ok := set["visibility"].(string); ok {
		vis, err := output.ParseVisibility(v)
		if err != nil {
			return validate("approval run: invalid visibility in payload", err.Error())
		}
		req.Visibility = &vis
	} else if set["visibility"] != nil {
		return validate("approval run: invalid visibility in payload", "expected string")
	}
	return nil
}

func (a *app) runApprovedPostReshare(cmd *cobra.Command, in approvedRunInput) (approvedRunResult, error) {
	parentURN, _ := in.payload["parent_urn"].(string)
	if parentURN == "" {
		return approvedRunResult{}, in.validate("approval run: missing parent_urn in payload", "")
	}
	visibility, err := approvedVisibility(in.payload, in.validate)
	if err != nil {
		return approvedRunResult{}, err
	}
	commentary, _ := in.payload["commentary"].(string)
	summary, err := in.transport.ResharePost(cmd.Context(), api.ResharePostRequest{
		ParentURN:  parentURN,
		Commentary: commentary,
		Visibility: visibility,
	})
	if err != nil {
		a.auditMutation(cmd, in.commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
		return approvedRunResult{}, a.mapTransportError(cmd, "post reshare", err)
	}
	return approvedRunResult{data: output.PostCreateData{PostSummary: *summary}, httpStatus: 201}, nil
}

func (a *app) runApprovedCommentAdd(cmd *cobra.Command, in approvedRunInput) (approvedRunResult, error) {
	postURN, _ := in.payload["post_urn"].(string)
	text, _ := in.payload["text"].(string)
	if postURN == "" || text == "" {
		return approvedRunResult{}, in.validate("approval run: missing post_urn or text in payload", "")
	}
	comment, err := in.transport.AddComment(cmd.Context(), postURN, text)
	if err != nil {
		a.auditMutation(cmd, in.commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
		return approvedRunResult{}, a.mapTransportError(cmd, "comment add", err)
	}
	return approvedRunResult{data: output.CommentAddData{CommentData: *comment}, httpStatus: 201}, nil
}

func (a *app) runApprovedReactAdd(cmd *cobra.Command, in approvedRunInput) (approvedRunResult, error) {
	postURN, _ := in.payload["post_urn"].(string)
	if postURN == "" {
		return approvedRunResult{}, in.validate("approval run: missing post_urn in payload", "")
	}
	rtStr := string(output.ReactionLike)
	if rtValue, ok := in.payload["type"]; ok {
		var ok bool
		rtStr, ok = rtValue.(string)
		if !ok {
			return approvedRunResult{}, in.validate("approval run: invalid reaction type in payload", "expected string")
		}
	}
	rtype, err := output.ParseReactionType(rtStr)
	if err != nil {
		return approvedRunResult{}, in.validate("approval run: invalid reaction type in payload", err.Error())
	}
	data, err := in.transport.AddReaction(cmd.Context(), postURN, rtype)
	if err != nil {
		a.auditMutation(cmd, in.commandID, "error", "normal", "", 0, string(output.ErrorCodeTransport), nil)
		return approvedRunResult{}, a.mapTransportError(cmd, "react add", err)
	}
	return approvedRunResult{data: output.ReactionAddData{ReactionData: *data, TargetURN: postURN}, httpStatus: 201}, nil
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
