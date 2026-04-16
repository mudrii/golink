package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

var defaultScopes = []string{"openid", "profile", "email", "w_member_social"}

func newAuthCommand(a *app) *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication state",
	}

	authCmd.AddCommand(
		newAuthLoginCommand(a),
		newAuthStatusCommand(a),
		newAuthLogoutCommand(a),
	)

	return authCmd
}

func newAuthLoginCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Start the LinkedIn native PKCE login flow",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.settings.ClientID == "" {
				return a.validationFailure(cmd, "missing required environment variable: GOLINK_CLIENT_ID", "auth login requires a LinkedIn app client ID")
			}

			request, err := auth.BuildLoginRequest(cmd.Context(), a.settings.ClientID, a.settings.RedirectPort, defaultScopes)
			if err != nil {
				return a.transportFailure(cmd, "failed to prepare auth login request", err.Error())
			}

			data := output.AuthLoginData{
				URL:       request.URL,
				Profile:   a.settings.Profile,
				Transport: a.settings.Transport,
				TimeoutMs: int(a.settings.Timeout / time.Millisecond),
			}

			return a.writeSuccess(cmd, data, request.URL)
		},
	}
}

func newAuthStatusCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication state for the current profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			session, err := a.deps.SessionStore.LoadSession(cmd.Context(), a.settings.Profile)
			if err != nil {
				if errors.Is(err, auth.ErrSessionNotFound) {
					data := output.AuthStatusData{
						IsAuthenticated: false,
						Profile:         a.settings.Profile,
						Transport:       a.settings.Transport,
					}
					return a.writeSuccess(cmd, data, fmt.Sprintf("not authenticated for profile %s", a.settings.Profile))
				}

				return a.transportFailure(cmd, "failed to read auth status", err.Error())
			}

			data := output.AuthStatusData{
				IsAuthenticated: true,
				Profile:         session.Profile,
				Transport:       session.Transport,
				Scopes:          append([]string(nil), session.Scopes...),
				AuthFlow:        session.AuthFlow,
			}
			if !session.ExpiresAt.IsZero() {
				data.ExpiresAt = session.ExpiresAt.UTC().Format(time.RFC3339)
			}

			return a.writeSuccess(cmd, data, fmt.Sprintf("authenticated for profile %s", session.Profile))
		},
	}
}

func newAuthLogoutCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear the current profile session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := a.deps.SessionStore.DeleteSession(cmd.Context(), a.settings.Profile)
			if err != nil {
				if errors.Is(err, auth.ErrSessionNotFound) {
					data := output.AuthLogoutData{
						Status:    "skipped",
						Profile:   a.settings.Profile,
						Transport: a.settings.Transport,
						Cleared:   false,
					}
					return a.writeSuccess(cmd, data, fmt.Sprintf("no session stored for profile %s", a.settings.Profile))
				}

				return a.transportFailure(cmd, "failed to clear session", err.Error())
			}

			data := output.AuthLogoutData{
				Status:    "ok",
				Profile:   a.settings.Profile,
				Transport: a.settings.Transport,
				Cleared:   true,
			}
			return a.writeSuccess(cmd, data, fmt.Sprintf("cleared session for profile %s", a.settings.Profile))
		},
	}
}
