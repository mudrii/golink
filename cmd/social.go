package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const socialMetadataURNLimit = 100

func newSocialCommand(a *app) *cobra.Command {
	socialCmd := &cobra.Command{
		Use:   "social",
		Short: "LinkedIn social metadata and engagement",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	socialCmd.AddCommand(newSocialMetadataCommand(a))

	return socialCmd
}

func newSocialMetadataCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "metadata <urn> [<urn>...]",
		Short: "Fetch engagement metadata for one or more post URNs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return a.validationFailure(cmd, "missing required argument: urn", "social metadata requires at least one post URN")
			}
			if len(args) > socialMetadataURNLimit {
				return a.validationFailure(cmd,
					fmt.Sprintf("too many URNs: %d supplied, maximum is %d", len(args), socialMetadataURNLimit),
					"batch into multiple calls of up to 100 URNs each",
				)
			}

			urns := make([]string, 0, len(args))
			for _, arg := range args {
				u := trimmedText(arg)
				if u == "" {
					return a.validationFailure(cmd, "empty urn in argument list", "each positional argument must be a non-empty post URN")
				}
				urns = append(urns, u)
			}

			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}
			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}

			data, err := transport.SocialMetadata(cmd.Context(), urns)
			if err != nil {
				return a.mapTransportError(cmd, "social metadata", err)
			}

			return a.writeSuccess(cmd, data, fmt.Sprintf("social metadata for %d post(s)", len(urns)))
		},
	}
}
