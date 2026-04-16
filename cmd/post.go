package cmd

import (
	"fmt"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

type postCreateFlags struct {
	text       string
	visibility string
	media      string
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
	)

	return postCmd
}

func newPostCreateCommand(a *app) *cobra.Command {
	var flags postCreateFlags

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a LinkedIn post",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			text := trimmedText(flags.text)
			if text == "" {
				return a.validationFailure(cmd, "missing required flag: --text", "non-interactive mode requires --text")
			}
			if len(text) < 5 || len(text) > 3000 {
				return a.validationFailure(cmd, "invalid --text length", "text must be between 5 and 3000 characters")
			}
			visibility, err := output.ParseVisibility(flags.visibility)
			if err != nil {
				return a.validationFailure(cmd, "invalid --visibility", err.Error())
			}

			if a.settings.DryRun {
				data := output.PostCreateDryRunData{
					WouldPost: output.PostPayloadPreview{
						Endpoint:   "POST /rest/posts",
						Text:       text,
						Visibility: visibility,
						Media:      flags.media,
					},
					Mode: "dry_run",
				}
				return a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN POST /rest/posts text=%q visibility=%s", text, visibility))
			}

			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}

			summary, err := transport.CreatePost(cmd.Context(), api.CreatePostRequest{
				Text:       text,
				Visibility: visibility,
				Media:      flags.media,
			})
			if err != nil {
				return a.mapTransportError(cmd, "post create", err)
			}

			data := output.PostCreateData{PostSummary: *summary}
			return a.writeSuccess(cmd, data, fmt.Sprintf("post created: %s", summary.URL))
		},
	}

	cmd.Flags().StringVar(&flags.text, "text", "", "post text")
	cmd.Flags().StringVar(&flags.visibility, "visibility", "PUBLIC", "PUBLIC|CONNECTIONS|LOGGED_IN")
	cmd.Flags().StringVar(&flags.media, "media", "", "optional media path")

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
		Use:   "delete <post_urn>",
		Short: "Delete a post",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			postURN := trimmedText(args[0])
			if postURN == "" {
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
				return a.writeDryRun(cmd, data, fmt.Sprintf("DRY RUN DELETE /rest/posts/%s", postURN))
			}

			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}
			data, err := transport.DeletePost(cmd.Context(), postURN)
			if err != nil {
				return a.mapTransportError(cmd, "post delete", err)
			}
			return a.writeSuccess(cmd, data, fmt.Sprintf("post deleted: %s", data.ID))
		},
	}
	return cmd
}
