package cmd

import "github.com/spf13/cobra"

func newSearchCommand(a *app) *cobra.Command {
	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search LinkedIn resources",
	}

	searchCmd.AddCommand(
		newUnsupportedCommand(a, "people", "Search people", "search people"),
	)

	return searchCmd
}
