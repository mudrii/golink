package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

// allowedImageTypes is the set of MIME types accepted for image upload.
// LinkedIn supports JPEG/PNG/GIF/WebP for member posts.
var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// detectImageType reads up to 512 bytes from path and returns the sniffed
// MIME type. Returns an error if the file cannot be opened.
func detectImageType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return http.DetectContentType(buf[:n]), nil
}

type postCreateFlags struct {
	text       string
	visibility string
	media      string
	image      string
	imageAlt   string
	asOrg      string
}

func newPostCommand(a *app) *cobra.Command {
	postCmd := &cobra.Command{
		Use:   "post",
		Short: "Manage LinkedIn posts",
	}

	postCmd.AddCommand(
		newPostCreateCommand(a),
		newPostListCommand(a),
		newPostGetCommand(a),
		newPostDeleteCommand(a),
		newPostEditCommand(a),
		newPostReshareCommand(a),
		newPostScheduleCommand(a),
	)

	return postCmd
}

func newPostCreateCommand(a *app) *cobra.Command {
	var flags postCreateFlags

	cmd := &cobra.Command{
		Use:         "create",
		Short:       "Create a LinkedIn post",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"audit": "mutating"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runPostCreateCommand(cmd, flags)
		},
	}

	cmd.Flags().StringVar(&flags.text, "text", "", "post text")
	cmd.Flags().StringVar(&flags.visibility, "visibility", "PUBLIC", "PUBLIC|CONNECTIONS|LOGGED_IN")
	cmd.Flags().StringVar(&flags.media, "media", "", "optional media path")
	cmd.Flags().StringVar(&flags.image, "image", "", "path to a local image to attach (single image)")
	cmd.Flags().StringVar(&flags.imageAlt, "image-alt", "", "alt text for the attached image")
	cmd.Flags().StringVar(&flags.asOrg, "as-org", "", "post as an organization (urn:li:organization:...); requires organization social write scope")

	return cmd
}

type preparedPostCreate struct {
	text      string
	asOrg     string
	imagePath string
	request   api.CreatePostRequest
	preview   output.PostPayloadPreview
}

func (a *app) runPostCreateCommand(cmd *cobra.Command, flags postCreateFlags) error {
	cmdID := a.newCommandID(commandName(cmd), a.deps.Now().UTC())
	ikey, _ := cmd.Flags().GetString("idempotency-key")

	prepared, err := a.preparePostCreate(cmd, cmdID, flags)
	if err != nil {
		return err
	}
	if a.settings.DryRun {
		return a.writePostCreateDryRun(cmd, cmdID, prepared)
	}
	if a.settings.RequireApproval {
		return a.approvalPending(cmd, cmdID, prepared.preview, ikey)
	}
	// Hold the per-key cross-process lock across Lookup→dispatch→Record so a
	// concurrent golink invocation with the same idempotency key cannot
	// also miss the cache and double-post.
	defer a.idempotencyAcquire(cmd.Context(), ikey)()
	if handled, err := a.writeCachedPostCreate(cmd, cmdID, ikey); handled || err != nil {
		return err
	}

	session, transport, err := a.resolvePostCreateTransport(cmd, cmdID, prepared.asOrg)
	if err != nil {
		return err
	}
	if err := a.attachPostCreateImage(cmd, cmdID, prepared.imagePath, flags.imageAlt, prepared.asOrg, session, transport, &prepared.request); err != nil {
		return err
	}

	summary, err := transport.CreatePost(cmd.Context(), prepared.request)
	if err != nil {
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
		return a.mapTransportError(cmd, "post create", err)
	}
	data := output.PostCreateData{PostSummary: *summary}
	a.recordPostCreateIdempotency(cmd, cmdID, ikey, data)
	writeErr := a.writeSuccess(cmd, data, fmt.Sprintf("post created: %s", summary.URL))
	a.auditMutationWithAuthor(cmd, cmdID, "ok", "normal", "", 201, "", nil, prepared.asOrg)
	return writeErr
}

func (a *app) preparePostCreate(cmd *cobra.Command, cmdID string, flags postCreateFlags) (preparedPostCreate, error) {
	text := trimmedText(flags.text)
	if text == "" {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return preparedPostCreate{}, a.validationFailure(cmd, "missing required flag: --text", "non-interactive mode requires --text")
	}
	if len(text) < 5 || len(text) > 3000 {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return preparedPostCreate{}, a.validationFailure(cmd, "invalid --text length", "text must be between 5 and 3000 characters")
	}
	visibility, err := output.ParseVisibility(flags.visibility)
	if err != nil {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return preparedPostCreate{}, a.validationFailure(cmd, "invalid --visibility", err.Error())
	}
	asOrg := strings.TrimSpace(flags.asOrg)
	if asOrg != "" && !strings.HasPrefix(asOrg, "urn:li:organization:") {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return preparedPostCreate{}, a.validationFailure(cmd, "invalid --as-org value", "--as-org must be a urn:li:organization:... URN")
	}

	request := api.CreatePostRequest{Text: text, Visibility: visibility, Media: flags.media, AuthorURN: asOrg}
	preview := output.PostPayloadPreview{
		Endpoint:   "POST /rest/posts",
		Text:       text,
		Visibility: visibility,
		Media:      flags.media,
		AuthorURN:  asOrg,
	}
	imagePath := strings.TrimSpace(flags.image)
	if imagePath != "" {
		preview.WouldUpload = &output.ImageUploadPreview{
			Path:           imagePath,
			PlaceholderURN: "urn:li:image:<to-be-uploaded>",
			Alt:            strings.TrimSpace(flags.imageAlt),
		}
	}
	return preparedPostCreate{text: text, asOrg: asOrg, imagePath: imagePath, request: request, preview: preview}, nil
}

func (a *app) writePostCreateDryRun(cmd *cobra.Command, cmdID string, prepared preparedPostCreate) error {
	data := output.PostCreateDryRunData{WouldPost: prepared.preview, Mode: "dry_run"}
	previewBytes, _ := json.Marshal(data)
	writeErr := a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN POST /rest/posts text=%q visibility=%s", prepared.text, prepared.request.Visibility))
	a.auditMutation(cmd, cmdID, "ok", "dry_run", "", 0, "", previewBytes)
	return writeErr
}

func (a *app) writeCachedPostCreate(cmd *cobra.Command, cmdID, ikey string) (bool, error) {
	cached, hit, err := a.idempotencyCheck(cmd, ikey, "post create")
	if err != nil {
		return true, err
	}
	if !hit {
		return false, nil
	}
	var data output.PostCreateData
	if decErr := json.Unmarshal(cached.Result, &data); decErr != nil {
		return false, nil
	}
	a.auditMutation(cmd, cmdID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
	return true, a.writeSuccessFromCache(cmd, data, fmt.Sprintf("post created (cached): %s", data.URL))
}

func (a *app) resolvePostCreateTransport(cmd *cobra.Command, cmdID, asOrg string) (auth.Session, api.Transport, error) {
	session, err := a.resolveSession(cmd)
	if err != nil {
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "UNAUTHORIZED", nil)
		return auth.Session{}, nil, err
	}
	if asOrg != "" && !sessionHasAnyScope(session, orgWriteScopes...) {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return auth.Session{}, nil, a.validationFailure(cmd,
			fmt.Sprintf("posting as an organization requires %s", formatScopeRequirement(orgWriteScopes...)),
			"run `golink auth login` with the scope added to the LinkedIn app")
	}
	transport, err := a.resolveTransport(cmd.Context(), session)
	if err != nil {
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
		return auth.Session{}, nil, a.transportFailure(cmd, "failed to build transport", err.Error())
	}
	return session, transport, nil
}

func (a *app) attachPostCreateImage(cmd *cobra.Command, cmdID, imagePath, imageAlt, asOrg string, session auth.Session, transport api.Transport, req *api.CreatePostRequest) error {
	if imagePath == "" {
		return nil
	}
	if _, err := os.Stat(imagePath); err != nil {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return a.validationFailure(cmd, "cannot read image file", err.Error())
	}
	mimeType, err := detectImageType(imagePath)
	if err != nil {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return a.validationFailure(cmd, "cannot read image file", err.Error())
	}
	if !allowedImageTypes[mimeType] {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return a.validationFailure(cmd, "unsupported image type",
			fmt.Sprintf("detected %q; allowed: image/jpeg, image/png, image/gif, image/webp", mimeType))
	}
	uploadOwner := session.MemberURN
	if asOrg != "" {
		uploadOwner = asOrg
	}
	uploadURL, imageURN, err := transport.InitializeImageUpload(cmd.Context(), uploadOwner)
	if err != nil {
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
		return a.mapTransportError(cmd, "post create image upload init", err)
	}
	if err := transport.UploadImageBinary(cmd.Context(), uploadURL, imagePath); err != nil {
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
		return a.mapTransportError(cmd, "post create image upload binary", err)
	}
	req.MediaPayload = &api.MediaPayload{ID: imageURN, Alt: strings.TrimSpace(imageAlt)}
	return nil
}

func (a *app) recordPostCreateIdempotency(cmd *cobra.Command, cmdID, ikey string, data output.PostCreateData) {
	if ikey == "" {
		return
	}
	resultBytes, _ := json.Marshal(data)
	a.idempotencyRecord(cmd.Context(), idempotency.Entry{
		TS:         a.deps.Now().UTC(),
		Key:        ikey,
		Command:    "post create",
		CommandID:  cmdID,
		Status:     "ok",
		HTTPStatus: 201,
		Result:     resultBytes,
	})
}

func newPostListCommand(a *app) *cobra.Command {
	var count int
	var start int
	var authorURN string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List posts for the authenticated member",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			list, err := transport.ListPosts(cmd.Context(), authorURN, count, start)
			if err != nil {
				return a.mapTransportError(cmd, "post list", err)
			}

			return a.writeSuccess(cmd, list, fmt.Sprintf("%d posts for %s", len(list.Items), list.OwnerURN))
		},
	}
	cmd.Flags().IntVar(&count, "count", 10, "page size")
	cmd.Flags().IntVar(&start, "start", 0, "offset")
	cmd.Flags().StringVar(&authorURN, "author-urn", "", "author URN (defaults to the authenticated member)")

	return cmd
}

func newPostGetCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "get <post_urn>",
		Short: "Get a single post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			postURN := trimmedText(args[0])
			if postURN == "" {
				return a.validationFailure(cmd, "missing required argument: post_urn", "post get requires a post URN")
			}
			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}
			data, err := transport.GetPost(cmd.Context(), postURN)
			if err != nil {
				return a.mapTransportError(cmd, "post get", err)
			}
			return a.writeSuccess(cmd, data, fmt.Sprintf("post %s", data.ID))
		},
	}
}

func newPostDeleteCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "delete <post_urn>",
		Short:       "Delete a post",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{"audit": "mutating"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdID := a.newCommandID(commandName(cmd), a.deps.Now().UTC())
			ikey, _ := cmd.Flags().GetString("idempotency-key")

			postURN := trimmedText(args[0])
			if postURN == "" {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "missing required argument: post_urn", "post delete requires a post URN")
			}
			if a.settings.DryRun {
				data := output.PostDeleteDryRunData{
					WouldDelete: output.PostDeletePreview{
						Endpoint: "DELETE /rest/posts/" + postURN,
						PostURN:  postURN,
					},
					Mode: "dry_run",
				}
				preview, _ := json.Marshal(data)
				writeErr := a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN DELETE /rest/posts/%s", postURN))
				a.auditMutation(cmd, cmdID, "ok", "dry_run", "", 0, "", preview)
				return writeErr
			}

			if a.settings.RequireApproval {
				payload := output.PostDeletePreview{
					Endpoint: "DELETE /rest/posts/" + postURN,
					PostURN:  postURN,
				}
				return a.approvalPending(cmd, cmdID, payload, ikey)
			}

			// Cross-process per-key lock across Lookup→dispatch→Record so a
			// concurrent process with the same idempotency key cannot also
			// observe a miss and re-issue the DELETE.
			defer a.idempotencyAcquire(cmd.Context(), ikey)()
			if cached, hit, checkErr := a.idempotencyCheck(cmd, ikey, "post delete"); hit {
				var data output.PostDeleteData
				if decErr := json.Unmarshal(cached.Result, &data); decErr == nil {
					a.auditMutation(cmd, cmdID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
					return a.writeSuccessFromCache(cmd, data, fmt.Sprintf("post deleted (cached): %s", data.ID))
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
			data, err := transport.DeletePost(cmd.Context(), postURN)
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
				return a.mapTransportError(cmd, "post delete", err)
			}
			if ikey != "" {
				resultBytes, _ := json.Marshal(data)
				a.idempotencyRecord(cmd.Context(), idempotency.Entry{
					TS:         a.deps.Now().UTC(),
					Key:        ikey,
					Command:    "post delete",
					CommandID:  cmdID,
					Status:     "ok",
					HTTPStatus: 204,
					Result:     resultBytes,
				})
			}
			writeErr := a.writeSuccess(cmd, data, fmt.Sprintf("post deleted: %s", data.ID))
			a.auditMutation(cmd, cmdID, "ok", "normal", "", 204, "", nil)
			return writeErr
		},
	}
	return cmd
}

func newPostEditCommand(a *app) *cobra.Command {
	var flagText string
	var flagVisibility string

	cmd := &cobra.Command{
		Use:         "edit <post_urn>",
		Short:       "Edit an existing post's commentary and/or visibility",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{"audit": "mutating"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runPostEditCommand(cmd, postEditInput{
				postURN:           args[0],
				text:              flagText,
				visibility:        flagVisibility,
				textChanged:       cmd.Flags().Changed("text"),
				visibilityChanged: cmd.Flags().Changed("visibility"),
			})
		},
	}

	cmd.Flags().StringVar(&flagText, "text", "", "new post commentary")
	cmd.Flags().StringVar(&flagVisibility, "visibility", "", "PUBLIC|CONNECTIONS|LOGGED_IN")

	return cmd
}

type postEditInput struct {
	postURN           string
	text              string
	visibility        string
	textChanged       bool
	visibilityChanged bool
}

type postEditChange struct {
	postURN    string
	text       *string
	visibility *output.Visibility
	patchBody  map[string]any
}

func (a *app) runPostEditCommand(cmd *cobra.Command, input postEditInput) error {
	cmdID := a.newCommandID(commandName(cmd), a.deps.Now().UTC())
	ikey, _ := cmd.Flags().GetString("idempotency-key")

	change, err := a.buildPostEditChange(cmd, cmdID, input)
	if err != nil {
		return err
	}
	preview := output.PostEditPreview{
		Endpoint: "PATCH /rest/posts/" + change.postURN,
		PostURN:  change.postURN,
		Patch:    map[string]any{"patch": change.patchBody},
	}
	if a.settings.DryRun {
		return a.writePostEditDryRun(cmd, cmdID, change.postURN, preview)
	}
	if a.settings.RequireApproval {
		return a.approvalPending(cmd, cmdID, preview, ikey)
	}
	defer a.idempotencyAcquire(cmd.Context(), ikey)()
	if handled, err := a.writeCachedPostEdit(cmd, cmdID, ikey); handled || err != nil {
		return err
	}

	transport, err := a.resolveMutatingTransport(cmd, cmdID)
	if err != nil {
		return err
	}
	data, err := transport.EditPost(cmd.Context(), api.EditPostRequest{
		PostURN:    change.postURN,
		Text:       change.text,
		Visibility: change.visibility,
	})
	if err != nil {
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
		return a.mapTransportError(cmd, "post edit", err)
	}

	a.recordPostEditIdempotency(cmd, cmdID, ikey, data)
	writeErr := a.writeSuccess(cmd, data, fmt.Sprintf("post edited: %s", data.ID))
	a.auditMutation(cmd, cmdID, "ok", "normal", "", 204, "", nil)
	return writeErr
}

func (a *app) buildPostEditChange(cmd *cobra.Command, cmdID string, input postEditInput) (postEditChange, error) {
	postURN := trimmedText(input.postURN)
	if postURN == "" {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return postEditChange{}, a.validationFailure(cmd, "missing required argument: post_urn", "post edit requires a post URN")
	}
	if !input.textChanged && !input.visibilityChanged {
		a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
		return postEditChange{}, a.validationFailure(cmd, "no changes specified", "provide --text and/or --visibility to edit")
	}

	var editText *string
	if input.textChanged {
		t := trimmedText(input.text)
		editText = &t
	}
	var editVisibility *output.Visibility
	if input.visibilityChanged {
		v, err := output.ParseVisibility(input.visibility)
		if err != nil {
			a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
			return postEditChange{}, a.validationFailure(cmd, "invalid --visibility", err.Error())
		}
		editVisibility = &v
	}
	return postEditChange{
		postURN:    postURN,
		text:       editText,
		visibility: editVisibility,
		patchBody:  postEditPatchBody(editText, editVisibility),
	}, nil
}

func postEditPatchBody(text *string, visibility *output.Visibility) map[string]any {
	patchSet := map[string]any{}
	if text != nil {
		patchSet["commentary"] = *text
	}
	if visibility != nil {
		patchSet["visibility"] = string(*visibility)
	}
	return map[string]any{"$set": patchSet}
}

func (a *app) writePostEditDryRun(cmd *cobra.Command, cmdID, postURN string, preview output.PostEditPreview) error {
	data := output.PostEditDryRunData{WouldPatch: preview, Mode: "dry_run"}
	previewBytes, _ := json.Marshal(data)
	writeErr := a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN PATCH /rest/posts/%s", postURN))
	a.auditMutation(cmd, cmdID, "ok", "dry_run", "", 0, "", previewBytes)
	return writeErr
}

func (a *app) writeCachedPostEdit(cmd *cobra.Command, cmdID, ikey string) (bool, error) {
	cached, hit, err := a.idempotencyCheck(cmd, ikey, "post edit")
	if err != nil {
		return true, err
	}
	if !hit {
		return false, nil
	}
	var data output.PostEditData
	if decErr := json.Unmarshal(cached.Result, &data); decErr != nil {
		return false, nil
	}
	a.auditMutation(cmd, cmdID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
	return true, a.writeSuccessFromCache(cmd, data, fmt.Sprintf("post edited (cached): %s", data.ID))
}

func (a *app) resolveMutatingTransport(cmd *cobra.Command, cmdID string) (api.Transport, error) {
	session, err := a.resolveSession(cmd)
	if err != nil {
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "UNAUTHORIZED", nil)
		return nil, err
	}
	transport, err := a.resolveTransport(cmd.Context(), session)
	if err != nil {
		a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
		return nil, a.transportFailure(cmd, "failed to build transport", err.Error())
	}
	return transport, nil
}

func (a *app) recordPostEditIdempotency(cmd *cobra.Command, cmdID, ikey string, data *output.PostEditData) {
	if ikey == "" {
		return
	}
	resultBytes, _ := json.Marshal(data)
	a.idempotencyRecord(cmd.Context(), idempotency.Entry{
		TS:         a.deps.Now().UTC(),
		Key:        ikey,
		Command:    "post edit",
		CommandID:  cmdID,
		Status:     "ok",
		HTTPStatus: 204,
		Result:     resultBytes,
	})
}

func newPostReshareCommand(a *app) *cobra.Command {
	var flagText string
	var flagVisibility string

	cmd := &cobra.Command{
		Use:         "reshare <share_urn>",
		Short:       "Reshare an existing post with optional commentary",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{"audit": "mutating"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdID := a.newCommandID(commandName(cmd), a.deps.Now().UTC())
			ikey, _ := cmd.Flags().GetString("idempotency-key")

			parentURN := trimmedText(args[0])
			if parentURN == "" {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "missing required argument: share_urn", "post reshare requires a share URN")
			}

			visibility := output.VisibilityPublic
			if cmd.Flags().Changed("visibility") {
				v, err := output.ParseVisibility(flagVisibility)
				if err != nil {
					a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
					return a.validationFailure(cmd, "invalid --visibility", err.Error())
				}
				visibility = v
			}

			commentary := strings.TrimSpace(flagText)

			if a.settings.DryRun {
				data := output.PostReshareDryRunData{
					WouldReshare: output.PostResharePreview{
						Endpoint:   "POST /rest/posts",
						ParentURN:  parentURN,
						Commentary: commentary,
						Visibility: visibility,
					},
					Mode: "dry_run",
				}
				previewBytes, _ := json.Marshal(data)
				writeErr := a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN POST /rest/posts reshare parent=%s", parentURN))
				a.auditMutation(cmd, cmdID, "ok", "dry_run", "", 0, "", previewBytes)
				return writeErr
			}

			if a.settings.RequireApproval {
				payload := output.PostResharePreview{
					Endpoint:   "POST /rest/posts",
					ParentURN:  parentURN,
					Commentary: commentary,
					Visibility: visibility,
				}
				return a.approvalPending(cmd, cmdID, payload, ikey)
			}

			defer a.idempotencyAcquire(cmd.Context(), ikey)()
			if cached, hit, checkErr := a.idempotencyCheck(cmd, ikey, "post reshare"); hit {
				var data output.PostCreateData
				if decErr := json.Unmarshal(cached.Result, &data); decErr == nil {
					a.auditMutation(cmd, cmdID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
					return a.writeSuccessFromCache(cmd, data, fmt.Sprintf("post reshared (cached): %s", data.URL))
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

			summary, err := transport.ResharePost(cmd.Context(), api.ResharePostRequest{
				ParentURN:  parentURN,
				Commentary: commentary,
				Visibility: visibility,
			})
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
				return a.mapTransportError(cmd, "post reshare", err)
			}

			data := output.PostCreateData{PostSummary: *summary}
			if ikey != "" {
				resultBytes, _ := json.Marshal(data)
				a.idempotencyRecord(cmd.Context(), idempotency.Entry{
					TS:         a.deps.Now().UTC(),
					Key:        ikey,
					Command:    "post reshare",
					CommandID:  cmdID,
					Status:     "ok",
					HTTPStatus: 201,
					Result:     resultBytes,
				})
			}
			writeErr := a.writeSuccess(cmd, data, fmt.Sprintf("post reshared: %s", summary.URL))
			a.auditMutation(cmd, cmdID, "ok", "normal", "", 201, "", nil)
			return writeErr
		},
	}

	cmd.Flags().StringVar(&flagText, "text", "", "commentary to add to the reshare")
	cmd.Flags().StringVar(&flagVisibility, "visibility", "PUBLIC", "PUBLIC|CONNECTIONS|LOGGED_IN")

	return cmd
}
