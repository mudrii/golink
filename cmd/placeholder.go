package cmd

import (
	"fmt"

	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

func newUnsupportedCommand(a *app, use, short, feature string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.writeUnsupported(cmd, output.UnsupportedPayload{
				Feature:           feature,
				Reason:            "implementation is not complete yet",
				SuggestedFallback: "retry after the transport and command handler are implemented",
			}, fmt.Sprintf("unsupported: %s is not implemented yet", feature))
		},
	}
}

func fallback(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}

	return value
}
