package cmd

import "github.com/spf13/cobra"

func newMCPCommand(a *app) *cobra.Command {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server",
	}

	mcpCmd.AddCommand(
		newUnsupportedCommand(a, "serve", "Serve the MCP stdio endpoint", "mcp serve"),
	)

	return mcpCmd
}
