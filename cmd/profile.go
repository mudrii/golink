package cmd

import (
	"errors"
	"fmt"

	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

func newProfileCommand(a *app) *cobra.Command {
	profileCmd := &cobra.Command{
		Use:   "profile",
		Short: "Read profile data",
	}

	profileCmd.AddCommand(&cobra.Command{
		Use:   "me",
		Short: "Show the authenticated profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			session, err := a.deps.SessionStore.LoadSession(cmd.Context(), a.settings.Profile)
			if err != nil {
				if errors.Is(err, auth.ErrSessionNotFound) {
					return a.authFailure(cmd, "Token expired or invalid. Re-run: golink auth login", "no active session for the selected profile")
				}

				return a.transportFailure(cmd, "failed to resolve profile session", err.Error())
			}

			data := output.ProfileData{
				Sub:       session.MemberURN,
				Name:      session.Name,
				Email:     session.Email,
				Picture:   session.Picture,
				ProfileID: session.ProfileID,
				Locale: output.Locale{
					Country:  fallback(session.LocaleCountry, "MY"),
					Language: fallback(session.LocaleLanguage, "en"),
				},
			}

			return a.writeSuccess(cmd, data, fmt.Sprintf("%s <%s>", fallback(data.Name, data.Sub), data.Email))
		},
	})

	return profileCmd
}
