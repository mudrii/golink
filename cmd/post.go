package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/idempotency"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

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
			cmdID := newCommandID(commandName(cmd), a.deps.Now().UTC())
			ikey, _ := cmd.Flags().GetString("idempotency-key")

			text := trimmedText(flags.text)
			if text == "" {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "missing required flag: --text", "non-interactive mode requires --text")
			}
			if len(text) < 5 || len(text) > 3000 {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "invalid --text length", "text must be between 5 and 3000 characters")
			}
			visibility, err := output.ParseVisibility(flags.visibility)
			if err != nil {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "invalid --visibility", err.Error())
			}

			asOrg := strings.TrimSpace(flags.asOrg)
			if asOrg != "" && !strings.HasPrefix(asOrg, "urn:li:organization:") {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "invalid --as-org value", "--as-org must be a urn:li:organization:... URN")
			}

			imagePath := strings.TrimSpace(flags.image)

			if a.settings.DryRun {
				preview := output.PostPayloadPreview{
					Endpoint:   "POST /rest/posts",
					Text:       text,
					Visibility: visibility,
					Media:      flags.media,
					AuthorURN:  asOrg,
				}
				if imagePath != "" {
					preview.WouldUpload = &output.ImageUploadPreview{
						Path:           imagePath,
						PlaceholderURN: "urn:li:image:<to-be-uploaded>",
						Alt:            strings.TrimSpace(flags.imageAlt),
					}
				}
				data := output.PostCreateDryRunData{
					WouldPost: preview,
					Mode:      "dry_run",
				}
				previewBytes, _ := json.Marshal(data)
				writeErr := a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN POST /rest/posts text=%q visibility=%s", text, visibility))
				a.auditMutation(cmd, cmdID, "ok", "dry_run", "", 0, "", previewBytes)
				return writeErr
			}

			if a.settings.RequireApproval {
				payload := output.PostPayloadPreview{
					Endpoint:   "POST /rest/posts",
					Text:       text,
					Visibility: visibility,
					Media:      flags.media,
					AuthorURN:  asOrg,
				}
				if imagePath != "" {
					payload.WouldUpload = &output.ImageUploadPreview{
						Path:           imagePath,
						PlaceholderURN: "urn:li:image:<to-be-uploaded>",
						Alt:            strings.TrimSpace(flags.imageAlt),
					}
				}
				return a.approvalPending(cmd, cmdID, payload, ikey)
			}

			// Idempotency check — replay cached result if available.
			if cached, hit, checkErr := a.idempotencyCheck(cmd, ikey, "post create"); hit {
				var data output.PostCreateData
				if decErr := json.Unmarshal(cached.Result, &data); decErr == nil {
					a.auditMutation(cmd, cmdID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
					return a.writeSuccessFromCache(cmd, data, fmt.Sprintf("post created (cached): %s", data.URL))
				}
			} else if checkErr != nil {
				return checkErr
			}

			session, err := a.resolveSession(cmd)
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "UNAUTHORIZED", nil)
				return err
			}

			// Scope check for org posting — must happen after session is resolved.
			if asOrg != "" {
				if !sessionHasAnyScope(session, orgWriteScopes...) {
					a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
					return a.validationFailure(cmd,
						fmt.Sprintf("posting as an organization requires %s", formatScopeRequirement(orgWriteScopes...)),
						"run `golink auth login` with the scope added to the LinkedIn app")
				}
			}

			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}

			createReq := api.CreatePostRequest{
				Text:       text,
				Visibility: visibility,
				Media:      flags.media,
				AuthorURN:  asOrg,
			}

			// Image upload flow: validate file → initialize → upload binary → attach URN.
			if imagePath != "" {
				if _, statErr := os.Stat(imagePath); statErr != nil {
					a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
					return a.validationFailure(cmd, "cannot read image file", statErr.Error())
				}
				uploadURL, imageURN, initErr := transport.InitializeImageUpload(cmd.Context(), session.MemberURN)
				if initErr != nil {
					a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
					return a.mapTransportError(cmd, "post create image upload init", initErr)
				}
				if uploadErr := transport.UploadImageBinary(cmd.Context(), uploadURL, imagePath); uploadErr != nil {
					a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
					return a.mapTransportError(cmd, "post create image upload binary", uploadErr)
				}
				createReq.MediaPayload = &api.MediaPayload{
					ID:  imageURN,
					Alt: strings.TrimSpace(flags.imageAlt),
				}
			}

			summary, err := transport.CreatePost(cmd.Context(), createReq)
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
				return a.mapTransportError(cmd, "post create", err)
			}

			data := output.PostCreateData{PostSummary: *summary}
			if ikey != "" {
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
			writeErr := a.writeSuccess(cmd, data, fmt.Sprintf("post created: %s", summary.URL))
			a.auditMutationWithAuthor(cmd, cmdID, "ok", "normal", "", 201, "", nil, asOrg)
			return writeErr
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
			cmdID := newCommandID(commandName(cmd), a.deps.Now().UTC())
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
			cmdID := newCommandID(commandName(cmd), a.deps.Now().UTC())
			ikey, _ := cmd.Flags().GetString("idempotency-key")

			postURN := trimmedText(args[0])
			if postURN == "" {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "missing required argument: post_urn", "post edit requires a post URN")
			}

			textChanged := cmd.Flags().Changed("text")
			visChanged := cmd.Flags().Changed("visibility")
			if !textChanged && !visChanged {
				a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
				return a.validationFailure(cmd, "no changes specified", "provide --text and/or --visibility to edit")
			}

			var editText *string
			if textChanged {
				t := trimmedText(flagText)
				editText = &t
			}
			var editVisibility *output.Visibility
			if visChanged {
				v, err := output.ParseVisibility(flagVisibility)
				if err != nil {
					a.auditMutation(cmd, cmdID, "validation_error", "normal", "", 0, "VALIDATION_ERROR", nil)
					return a.validationFailure(cmd, "invalid --visibility", err.Error())
				}
				editVisibility = &v
			}

			// Build PATCH preview for dry-run and approval.
			patchSet := map[string]any{}
			if editText != nil {
				patchSet["commentary"] = *editText
			}
			if editVisibility != nil {
				patchSet["visibility"] = string(*editVisibility)
			}
			patchBody := map[string]any{"$set": patchSet}

			if a.settings.DryRun {
				data := output.PostEditDryRunData{
					WouldPatch: output.PostEditPreview{
						Endpoint: "PATCH /rest/posts/" + postURN,
						PostURN:  postURN,
						Patch:    map[string]any{"patch": patchBody},
					},
					Mode: "dry_run",
				}
				previewBytes, _ := json.Marshal(data)
				writeErr := a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN PATCH /rest/posts/%s", postURN))
				a.auditMutation(cmd, cmdID, "ok", "dry_run", "", 0, "", previewBytes)
				return writeErr
			}

			if a.settings.RequireApproval {
				payload := output.PostEditPreview{
					Endpoint: "PATCH /rest/posts/" + postURN,
					PostURN:  postURN,
					Patch:    map[string]any{"patch": patchBody},
				}
				return a.approvalPending(cmd, cmdID, payload, ikey)
			}

			if cached, hit, checkErr := a.idempotencyCheck(cmd, ikey, "post edit"); hit {
				var data output.PostEditData
				if decErr := json.Unmarshal(cached.Result, &data); decErr == nil {
					a.auditMutation(cmd, cmdID, "ok", "normal", cached.RequestID, cached.HTTPStatus, "", nil)
					return a.writeSuccessFromCache(cmd, data, fmt.Sprintf("post edited (cached): %s", data.ID))
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

			data, err := transport.EditPost(cmd.Context(), api.EditPostRequest{
				PostURN:    postURN,
				Text:       editText,
				Visibility: editVisibility,
			})
			if err != nil {
				a.auditMutation(cmd, cmdID, "error", "normal", "", 0, "TRANSPORT_ERROR", nil)
				return a.mapTransportError(cmd, "post edit", err)
			}

			if ikey != "" {
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
			writeErr := a.writeSuccess(cmd, data, fmt.Sprintf("post edited: %s", data.ID))
			a.auditMutation(cmd, cmdID, "ok", "normal", "", 204, "", nil)
			return writeErr
		},
	}

	cmd.Flags().StringVar(&flagText, "text", "", "new post commentary")
	cmd.Flags().StringVar(&flagVisibility, "visibility", "", "PUBLIC|CONNECTIONS|LOGGED_IN")

	return cmd
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
			cmdID := newCommandID(commandName(cmd), a.deps.Now().UTC())
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
