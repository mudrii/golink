package cmd

import (
	"fmt"

	"github.com/mudrii/golink/internal/api"
	"github.com/mudrii/golink/internal/auth"
	"github.com/spf13/cobra"
)

func newSearchCommand(a *app) *cobra.Command {
	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search LinkedIn resources",
	}

	searchCmd.AddCommand(newSearchPeopleCommand(a))
	return searchCmd
}

func newSearchPeopleCommand(a *app) *cobra.Command {
	var keywords string
	var count int

	cmd := &cobra.Command{
		Use:   "people",
		Short: "Search people",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if trimmedText(keywords) == "" {
				return a.validationFailure(cmd, "missing required flag: --keywords", "search people requires --keywords")
			}
			if count < 1 || count > 50 {
				return a.validationFailure(cmd, "invalid --count", "count must be between 1 and 50")
			}

			// search people on the official transport is entitlement-gated and
			// returns ErrFeatureUnavailable without hitting the network, so
			// we don't require a session up-front here. Load it best-effort.
			session, _ := a.deps.SessionStore.LoadSession(cmd.Context(), a.settings.Profile)
			if session == nil {
				session = &auth.Session{Profile: a.settings.Profile, Transport: a.settings.Transport}
			}
			transport, err := a.resolveTransport(cmd.Context(), *session)
			if err != nil {
				return a.transportFailure(cmd, "failed to build transport", err.Error())
			}
			data, err := transport.SearchPeople(cmd.Context(), api.SearchPeopleRequest{
				Keywords: trimmedText(keywords),
				Count:    count,
			})
			if err != nil {
				return a.mapTransportError(cmd, "search people", err)
			}
			return a.writeSuccess(cmd, data, fmt.Sprintf("%d results for %q", data.Count, data.Query))
		},
	}
	cmd.Flags().StringVar(&keywords, "keywords", "", "search keywords")
	cmd.Flags().IntVar(&count, "count", 10, "result count")

	return cmd
}
