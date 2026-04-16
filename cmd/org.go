package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newOrgCommand(a *app) *cobra.Command {
	orgCmd := &cobra.Command{
		Use:   "org",
		Short: "Manage LinkedIn organizations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	orgCmd.AddCommand(newOrgListCommand(a))
	return orgCmd
}

func newOrgListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List organizations where the authenticated member is an administrator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			session, err := a.resolveSession(cmd)
			if err != nil {
				return err
			}

			hasOrgScope := false
			for _, s := range session.Scopes {
				if s == "w_organization_social" {
					hasOrgScope = true
					break
				}
			}
			if !hasOrgScope {
				return a.validationFailure(cmd,
					"org list requires the w_organization_social scope",
					"run `golink auth login` with w_organization_social added to the LinkedIn app")
			}

			transport, err := a.resolveTransport(cmd.Context(), session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}

			data, err := transport.ListOrganizations(cmd.Context())
			if err != nil {
				return a.mapTransportError(cmd, "org list", err)
			}

			return a.writeSuccess(cmd, data, fmt.Sprintf("%d organizations", data.Count))
		},
	}
}
