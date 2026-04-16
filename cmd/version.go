package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show golink build information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data := buildVersionData(a.buildInfo)
			text := fmt.Sprintf("golink %s (%s/%s, %s)", data.Version, data.OS, data.Arch, data.GoVersion)
			return a.writeSuccess(cmd, data, text)
		},
	}
}
