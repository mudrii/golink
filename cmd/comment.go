package cmd

import "github.com/spf13/cobra"

func newCommentCommand(a *app) *cobra.Command {
	commentCmd := &cobra.Command{
		Use:   "comment",
		Short: "Manage LinkedIn comments",
	}

	commentCmd.AddCommand(
		newUnsupportedCommand(a, "add <post_urn>", "Add a comment", "comment add"),
		newUnsupportedCommand(a, "list <post_urn>", "List comments", "comment list"),
	)

	return commentCmd
}
