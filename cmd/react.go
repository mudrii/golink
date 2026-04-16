package cmd

import "github.com/spf13/cobra"

func newReactCommand(a *app) *cobra.Command {
	reactCmd := &cobra.Command{
		Use:   "react",
		Short: "Manage LinkedIn reactions",
	}

	reactCmd.AddCommand(
		newUnsupportedCommand(a, "add <post_urn>", "Add a reaction", "react add"),
		newUnsupportedCommand(a, "list <post_urn>", "List reactions", "react list"),
	)

	return reactCmd
}
