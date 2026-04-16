package cmd

import (
	"fmt"
	"strings"

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
		newUnsupportedCommand(a, "list", "List posts", "post list"),
		newUnsupportedCommand(a, "get <post_urn>", "Get a post", "post get"),
		newUnsupportedCommand(a, "delete <post_urn>", "Delete a post", "post delete"),
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
			text := strings.TrimSpace(flags.text)
			if text == "" {
				return a.validationFailure(cmd, "missing required flag: --text", "non-interactive mode requires --text in the current implementation")
			}
			if len(text) < 5 || len(text) > 3000 {
				return a.validationFailure(cmd, "invalid --text length", "text must be between 5 and 3000 characters")
			}

			visibility, err := parseVisibility(flags.visibility)
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

			return a.writeUnsupported(cmd, output.UnsupportedPayload{
				Feature:           "post create",
				Reason:            "network transport implementation is not wired yet",
				SuggestedFallback: "retry with --dry-run to preview the request payload",
			}, "unsupported: post create network transport is not wired yet")
		},
	}

	cmd.Flags().StringVar(&flags.text, "text", "", "post text")
	cmd.Flags().StringVar(&flags.visibility, "visibility", "PUBLIC", "PUBLIC|CONNECTIONS|LOGGED_IN")
	cmd.Flags().StringVar(&flags.media, "media", "", "optional media path")

	return cmd
}

func parseVisibility(raw string) (output.Visibility, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(output.VisibilityPublic):
		return output.VisibilityPublic, nil
	case string(output.VisibilityConnections):
		return output.VisibilityConnections, nil
	case string(output.VisibilityLoggedIn):
		return output.VisibilityLoggedIn, nil
	default:
		return "", fmt.Errorf("visibility must be PUBLIC|CONNECTIONS|LOGGED_IN")
	}
}
