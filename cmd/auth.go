package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		newAuthRefreshCommand(a),
	)

	return authCmd
}

func newAuthLoginCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Start the LinkedIn native PKCE login flow",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loginCtx, cancel := context.WithTimeout(cmd.Context(), a.settings.Timeout)
			defer cancel()

			if a.settings.ClientID == "" {
				return a.validationFailure(cmd, "missing required environment variable: GOLINK_CLIENT_ID", "auth login requires a LinkedIn app client ID")
			}

			request, err := auth.BuildLoginRequest(loginCtx, a.settings.ClientID, a.settings.RedirectPort, defaultScopes)
			if err != nil {
				return a.transportFailure(cmd, "failed to prepare auth login request", err.Error())
			}

			data := output.AuthLoginData{
				URL:       request.URL,
				Profile:   a.settings.Profile,
				Transport: a.settings.Transport,
				TimeoutMs: int(a.settings.Timeout / time.Millisecond),
			}

			if err := a.writeAuthLoginStart(cmd, data); err != nil {
				return err
			}

			session, err := a.deps.LoginRunner(loginCtx, request, a.settings.Profile, a.settings.Transport, auth.LoginFlowOptions{
				HTTPClient:    a.deps.HTTPClient,
				BrowserOpener: a.deps.BrowserOpener,
				Interactive:   a.deps.IsInteractive(),
				Now:           a.deps.Now,
			})
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					return a.authFailure(cmd, "authentication timed out", err.Error())
				}

				return a.authFailure(cmd, "authentication failed", err.Error())
			}

			if err := a.deps.SessionStore.SaveSession(loginCtx, *session); err != nil {
				return a.authFailure(cmd, "authentication failed", fmt.Sprintf("persist session: %v", err))
			}

			return a.writeAuthLoginResult(cmd, output.AuthLoginResultData{
				Status:        "success",
				Profile:       session.Profile,
				Transport:     session.Transport,
				ConnectedAt:   session.ConnectedAt.UTC().Format(time.RFC3339),
				ScopesGranted: append([]string(nil), session.Scopes...),
			})
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

			authenticated, err := session.IsAuthenticated(a.deps.Now())
			if err != nil {
				return a.authFailure(cmd, "Token expired or invalid. Re-run: golink auth login", err.Error())
			}

			data := output.AuthStatusData{
				IsAuthenticated: authenticated,
				Profile:         session.Profile,
				Transport:       session.Transport,
				Scopes:          append([]string(nil), session.Scopes...),
				AuthFlow:        session.AuthFlow,
			}
			if !session.ExpiresAt.IsZero() {
				data.ExpiresAt = session.ExpiresAt.UTC().Format(time.RFC3339)
			}
			if !session.RefreshExpiresAt.IsZero() {
				data.RefreshExpiresAt = session.RefreshExpiresAt.UTC().Format(time.RFC3339)
			}

			if !authenticated {
				return a.writeSuccess(cmd, data, fmt.Sprintf("not authenticated for profile %s", session.Profile))
			}

			return a.writeSuccess(cmd, data, fmt.Sprintf("authenticated for profile %s", session.Profile))
		},
	}
}

func (a *app) writeAuthLoginStart(cmd *cobra.Command, data output.AuthLoginData) error {
	if a.settings.JSON {
		envelope := output.Success(a.metadata(cmd, output.StatusOK), data)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}

		return nil
	}

	if _, err := fmt.Fprintf(a.deps.Stdout, "Open this URL to continue authentication:\n%s\n", data.URL); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}

	return nil
}

func (a *app) writeAuthLoginResult(cmd *cobra.Command, data output.AuthLoginResultData) error {
	if a.settings.JSON {
		envelope := output.Success(a.metadata(cmd, output.StatusOK), data)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}

		return nil
	}

	if _, err := fmt.Fprintf(a.deps.Stdout, "Authenticated profile %s with scopes: %s\n", data.Profile, strings.Join(data.ScopesGranted, ", ")); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}

	return nil
}

func newAuthRefreshCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Refresh the access token without re-authorizing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refreshCtx, cancel := context.WithTimeout(cmd.Context(), a.settings.Timeout)
			defer cancel()

			session, err := a.deps.SessionStore.LoadSession(refreshCtx, a.settings.Profile)
			if err != nil {
				if errors.Is(err, auth.ErrSessionNotFound) {
					return a.validationFailure(cmd,
						fmt.Sprintf("no session for profile %s; run: golink auth login", a.settings.Profile),
						"session not found")
				}
				return a.transportFailure(cmd, "failed to load session", err.Error())
			}

			if session.RefreshToken == "" {
				return a.authFailure(cmd,
					"refresh tokens unavailable for this app (apply for LinkedIn programmatic refresh-token enablement or re-run auth login)",
					"no refresh token stored")
			}

			if a.settings.ClientID == "" {
				return a.validationFailure(cmd, "missing required environment variable: GOLINK_CLIENT_ID", "auth refresh requires a LinkedIn app client ID")
			}

			token, err := auth.RefreshAccessToken(refreshCtx, a.deps.HTTPClient, a.deps.TokenURL, a.settings.ClientID, session.RefreshToken)
			if err != nil {
				return a.authFailure(cmd, "refresh token expired; re-run: golink auth login", err.Error())
			}

			now := a.deps.Now().UTC()
			session.AccessToken = token.AccessToken
			if token.ExpiresIn > 0 {
				session.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second)
			}
			if token.Scope != "" {
				session.Scopes = strings.Fields(token.Scope)
			}
			if token.RefreshToken != "" {
				session.RefreshToken = token.RefreshToken
				if token.RefreshTokenExpiresIn > 0 {
					session.RefreshExpiresAt = now.Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second)
				}
			}

			if err := a.deps.SessionStore.SaveSession(refreshCtx, *session); err != nil {
				return a.transportFailure(cmd, "failed to persist refreshed session", err.Error())
			}

			data := output.AuthRefreshData{
				Profile:       session.Profile,
				Transport:     session.Transport,
				RefreshedAt:   now.Format(time.RFC3339),
				ExpiresAt:     session.ExpiresAt.UTC().Format(time.RFC3339),
				ScopesGranted: append([]string(nil), session.Scopes...),
			}
			if !session.RefreshExpiresAt.IsZero() {
				data.RefreshExpiresAt = session.RefreshExpiresAt.UTC().Format(time.RFC3339)
			}

			return a.writeAuthRefresh(cmd, data)
		},
	}
}

func (a *app) writeAuthRefresh(cmd *cobra.Command, data output.AuthRefreshData) error {
	if a.settings.JSON {
		envelope := output.Success(a.metadata(cmd, output.StatusOK), data)
		if err := output.WriteJSON(a.deps.Stdout, envelope); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}

	if _, err := fmt.Fprintf(a.deps.Stdout, "Refreshed token for profile %s; expires %s\n", data.Profile, data.ExpiresAt); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
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
