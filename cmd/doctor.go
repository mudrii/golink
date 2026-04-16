package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mudrii/golink/internal/audit"
	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/output"
	"github.com/spf13/cobra"
)

// userinfoURL is the LinkedIn userinfo endpoint used by the probe.
const userinfoURL = "https://api.linkedin.com/v2/userinfo"

// featureRule maps a command name to the scopes required for "supported" status,
// or a fixed reason when the feature is always unavailable on official transport.
type featureRule struct {
	command       string
	requiredScope string // empty means entitlement-gated or always-unsupported
	fixedReason   string // non-empty means always unsupported regardless of scopes
}

var featureRules = []featureRule{
	{command: "profile me", requiredScope: "openid"},
	{command: "post create", requiredScope: "w_member_social"},
	{command: "post list", fixedReason: "r_member_social is closed by LinkedIn (entitlement-gated)"},
	{command: "post get", fixedReason: "r_member_social is closed by LinkedIn (entitlement-gated)"},
	{command: "post delete", requiredScope: "w_member_social"},
	{command: "comment add", requiredScope: "w_member_social"},
	{command: "comment list", fixedReason: "r_member_social is closed by LinkedIn (entitlement-gated)"},
	{command: "react add", requiredScope: "w_member_social"},
	{command: "react list", fixedReason: "r_member_social is closed by LinkedIn (entitlement-gated)"},
	{command: "search people", fixedReason: "not available on official transport (use --transport=unofficial)"},
}

func newDoctorCommand(a *app) *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose golink configuration, session, and feature support",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			now := a.deps.Now()

			// --- Environment ---
			env := buildDoctorEnvironment()

			// --- Session ---
			var session *auth.Session
			sessionErr := error(nil)
			session, sessionErr = a.deps.SessionStore.LoadSession(cmd.Context(), a.settings.Profile)
			if sessionErr != nil && !errors.Is(sessionErr, auth.ErrSessionNotFound) {
				sessionErr = fmt.Errorf("load session: %w", sessionErr)
			}

			docSession := buildDoctorSession(session, a.settings.Profile, now)

			// --- Probe ---
			probeTarget := a.deps.UserinfoURL
			if probeTarget == "" {
				probeTarget = userinfoURL
			}
			probe := output.DoctorProbe{URL: probeTarget, Attempted: false}
			if session != nil && session.AccessToken != "" {
				authenticated, _ := session.IsAuthenticated(now)
				if authenticated {
					probe = runUserinfoProbe(cmd.Context(), a.deps.HTTPClient, a.settings.Timeout, session.AccessToken, probeTarget)
				}
			}

			// --- Features ---
			var scopes []string
			if session != nil {
				scopes = session.Scopes
			}
			features := buildFeatureMap(scopes, session)

			// --- Audit ---
			auditPath := a.settings.AuditPath
			if auditPath == "" {
				auditPath = audit.ResolvePath()
			}
			docAudit := buildDoctorAudit(auditPath, a.settings.Audit)

			// --- Warnings + Errors ---
			var warnings, errs []string

			// Session warnings
			if session == nil || errors.Is(sessionErr, auth.ErrSessionNotFound) {
				warnings = append(warnings, "no active session — run: golink auth login")
			} else if sessionErr != nil {
				errs = append(errs, fmt.Sprintf("session load error: %s", sessionErr))
			} else {
				authenticated, _ := session.IsAuthenticated(now)
				if !authenticated {
					errs = append(errs, "session exists but access token is expired — run: golink auth login")
				} else if !session.ExpiresAt.IsZero() {
					hoursLeft := session.ExpiresAt.Sub(now).Hours()
					if hoursLeft < 168 {
						warnings = append(warnings, fmt.Sprintf("access token expires in %.0f hours (< 7 days)", hoursLeft))
					}
				}
				if session.RefreshToken != "" && !session.RefreshExpiresAt.IsZero() {
					daysLeft := session.RefreshExpiresAt.Sub(now).Hours() / 24
					if daysLeft < 30 {
						warnings = append(warnings, fmt.Sprintf("refresh token expires in %.0f days (< 30 days)", daysLeft))
					}
				}
			}

			// Client ID warning
			if !env.GOLINKClientID {
				warnings = append(warnings, "GOLINK_CLIENT_ID is not set — required for auth login and auth refresh")
			}

			// Probe errors
			if probe.Attempted && probe.Error != "" {
				errs = append(errs, fmt.Sprintf("LinkedIn probe failed: %s", probe.Error))
			}
			if probe.Attempted && probe.Status == http.StatusUnauthorized {
				errs = append(errs, "LinkedIn probe returned 401 — token may be invalid")
			}

			// Audit warning
			if !docAudit.Enabled {
				warnings = append(warnings, "audit log is disabled")
			}

			// Health
			health := "ok"
			switch {
			case len(errs) > 0:
				health = "error"
			case len(warnings) > 0:
				health = "warnings"
			}

			data := output.DoctorData{
				APIVersion:  a.settings.APIVersion,
				Environment: env,
				Session:     docSession,
				Probe:       probe,
				Features:    features,
				Audit:       docAudit,
				Warnings:    warnings,
				Errors:      errs,
				Health:      health,
			}

			// For text mode, use our custom renderer; all other modes go through writeSuccess.
			if a.settings.Output == output.ModeText {
				if err := writeDoctorText(a.deps.Stdout, data); err != nil {
					return err
				}
			} else {
				if err := a.writeSuccess(cmd, data, ""); err != nil {
					return err
				}
			}

			// --strict exit handling (applies to all output modes)
			if strict {
				switch health {
				case "error":
					return &commandFailure{
						outputMode: a.settings.Output,
						exitCode:   5,
						text:       "doctor: one or more errors detected",
					}
				case "warnings":
					return &commandFailure{
						outputMode: a.settings.Output,
						exitCode:   2,
						text:       "doctor: one or more warnings detected",
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "exit 2 on warnings, exit 5 on errors")
	return cmd
}

// buildDoctorEnvironment reads the relevant environment variables.
func buildDoctorEnvironment() output.DoctorEnvironment {
	env := output.DoctorEnvironment{}
	_, env.GOLINKClientID = os.LookupEnv("GOLINK_CLIENT_ID")
	env.GOLINKAPIVersion, _ = os.LookupEnv("GOLINK_API_VERSION")
	env.GOLINKRedirect, _ = os.LookupEnv("GOLINK_REDIRECT_PORT")
	env.GOLINKJSON, _ = os.LookupEnv("GOLINK_JSON")
	env.GOLINKTransport, _ = os.LookupEnv("GOLINK_TRANSPORT")
	env.GOLINKOutput, _ = os.LookupEnv("GOLINK_OUTPUT")
	env.GOLINKAudit, _ = os.LookupEnv("GOLINK_AUDIT")
	env.GOLINKAuditPath, _ = os.LookupEnv("GOLINK_AUDIT_PATH")
	return env
}

// buildDoctorSession builds the session section of the doctor report.
func buildDoctorSession(session *auth.Session, profile string, now time.Time) output.DoctorSession {
	if session == nil {
		return output.DoctorSession{
			Profile:          profile,
			Authenticated:    false,
			RefreshAvailable: false,
		}
	}

	authenticated, _ := session.IsAuthenticated(now)
	doc := output.DoctorSession{
		Profile:          session.Profile,
		Authenticated:    authenticated,
		RefreshAvailable: session.RefreshToken != "",
		Scopes:           append([]string(nil), session.Scopes...),
		AuthFlow:         session.AuthFlow,
	}

	if !session.ExpiresAt.IsZero() {
		doc.ExpiresAt = session.ExpiresAt.UTC().Format(time.RFC3339)
		hours := int(session.ExpiresAt.Sub(now).Hours())
		if hours < 0 {
			hours = 0
		}
		doc.ExpiresInHours = hours
	}
	if !session.RefreshExpiresAt.IsZero() {
		doc.RefreshExpiresAt = session.RefreshExpiresAt.UTC().Format(time.RFC3339)
		days := int(session.RefreshExpiresAt.Sub(now).Hours() / 24)
		if days < 0 {
			days = 0
		}
		doc.RefreshInDays = days
	}
	if !session.ConnectedAt.IsZero() {
		doc.ConnectedAt = session.ConnectedAt.UTC().Format(time.RFC3339)
	}

	return doc
}

// runUserinfoProbe calls /v2/userinfo and returns the probe result.
// Errors are captured in DoctorProbe.Error; they never abort doctor.
func runUserinfoProbe(ctx context.Context, client *http.Client, timeout time.Duration, accessToken, targetURL string) output.DoctorProbe {
	probe := output.DoctorProbe{URL: targetURL, Attempted: true}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, targetURL, http.NoBody)
	if err != nil {
		probe.Error = fmt.Sprintf("build request: %s", err)
		return probe
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		probe.Error = fmt.Sprintf("request failed: %s", err)
		return probe
	}

	probe.Status = resp.StatusCode
	probe.RequestID = resp.Header.Get("X-RestLi-Id")
	if probe.RequestID == "" {
		probe.RequestID = resp.Header.Get("X-Request-Id")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if closeErr := resp.Body.Close(); closeErr != nil {
			probe.Error = fmt.Sprintf("HTTP %d (body close: %s)", resp.StatusCode, closeErr)
			return probe
		}
		probe.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return probe
	}

	var userinfo struct {
		Sub string `json:"sub"`
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&userinfo)
	if closeErr := resp.Body.Close(); closeErr != nil && decodeErr == nil {
		probe.Error = fmt.Sprintf("body close: %s", closeErr)
		return probe
	}
	if decodeErr != nil {
		probe.Error = fmt.Sprintf("decode response: %s", decodeErr)
		return probe
	}
	probe.Member = userinfo.Sub
	return probe
}

// buildFeatureMap evaluates each command family against the granted scopes.
func buildFeatureMap(scopes []string, session *auth.Session) []output.DoctorFeature {
	scopeSet := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		scopeSet[s] = struct{}{}
	}

	features := make([]output.DoctorFeature, 0, len(featureRules)+1)
	for _, rule := range featureRules {
		f := output.DoctorFeature{Command: rule.command}
		switch {
		case rule.fixedReason != "":
			f.Status = "unsupported"
			f.Reason = rule.fixedReason
		case rule.requiredScope == "":
			f.Status = "unsupported"
		default:
			// profile me: either openid or profile satisfies it
			if rule.command == "profile me" {
				_, hasOpenID := scopeSet["openid"]
				_, hasProfile := scopeSet["profile"]
				if hasOpenID || hasProfile {
					f.Status = "supported"
				} else {
					f.Status = "unsupported"
					f.Reason = "requires openid or profile scope"
				}
			} else {
				if _, ok := scopeSet[rule.requiredScope]; ok {
					f.Status = "supported"
				} else {
					f.Status = "unsupported"
					f.Reason = fmt.Sprintf("requires %s scope which is not granted", rule.requiredScope)
				}
			}
		}
		features = append(features, f)
	}

	// auth refresh: supported if refresh token stored
	refreshStatus := "unsupported"
	refreshReason := "no refresh token stored"
	if session != nil && session.RefreshToken != "" {
		refreshStatus = "supported"
		refreshReason = ""
	}
	features = append(features, output.DoctorFeature{
		Command: "auth refresh",
		Status:  refreshStatus,
		Reason:  refreshReason,
	})

	return features
}

// buildDoctorAudit checks the audit log path and file state.
func buildDoctorAudit(path string, enabled bool) output.DoctorAudit {
	doc := output.DoctorAudit{
		Path:    path,
		Enabled: enabled,
	}
	info, err := os.Stat(path)
	if err == nil {
		doc.Exists = true
		doc.Size = info.Size()
		doc.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339)
	}
	return doc
}

// writeDoctorText renders a human-readable doctor report.
// It builds the report in a strings.Builder (writes never fail) then flushes
// the result to w in a single call so the caller gets one meaningful error.
func writeDoctorText(w io.Writer, d output.DoctorData) error {
	var b strings.Builder

	b.WriteString("golink doctor — diagnostics\n\n")

	// Environment
	b.WriteString("Environment\n")
	clientIDVal := "NOT SET"
	if d.Environment.GOLINKClientID {
		clientIDVal = "set"
	}
	fmt.Fprintf(&b, "  GOLINK_CLIENT_ID:   %s\n", clientIDVal)
	apiVer := d.Environment.GOLINKAPIVersion
	if apiVer == "" {
		apiVer = "(not set; default used)"
	}
	fmt.Fprintf(&b, "  GOLINK_API_VERSION: %s\n", apiVer)
	if d.Environment.GOLINKRedirect != "" {
		fmt.Fprintf(&b, "  GOLINK_REDIRECT_PORT: %s\n", d.Environment.GOLINKRedirect)
	}
	if d.Environment.GOLINKJSON != "" {
		fmt.Fprintf(&b, "  GOLINK_JSON:        %s\n", d.Environment.GOLINKJSON)
	}
	if d.Environment.GOLINKTransport != "" {
		fmt.Fprintf(&b, "  GOLINK_TRANSPORT:   %s\n", d.Environment.GOLINKTransport)
	}
	if d.Environment.GOLINKOutput != "" {
		fmt.Fprintf(&b, "  GOLINK_OUTPUT:      %s\n", d.Environment.GOLINKOutput)
	}
	auditVal := d.Environment.GOLINKAudit
	if auditVal == "" {
		auditVal = "(not set; default on)"
	}
	fmt.Fprintf(&b, "  GOLINK_AUDIT:       %s\n", auditVal)
	if d.Environment.GOLINKAuditPath != "" {
		fmt.Fprintf(&b, "  GOLINK_AUDIT_PATH:  %s\n", d.Environment.GOLINKAuditPath)
	}
	configPath := d.Environment.ConfigPath
	if configPath == "" {
		configPath = "(none)"
	}
	fmt.Fprintf(&b, "  Config file:        %s\n\n", configPath)

	// Session
	fmt.Fprintf(&b, "Session (profile: %s)\n", d.Session.Profile)
	fmt.Fprintf(&b, "  Authenticated: %v\n", d.Session.Authenticated)
	if d.Session.ExpiresAt != "" {
		fmt.Fprintf(&b, "  Access expires: %s (%d hours)\n", d.Session.ExpiresAt, d.Session.ExpiresInHours)
	}
	if d.Session.RefreshAvailable {
		refreshExp := "(no expiry stored)"
		if d.Session.RefreshExpiresAt != "" {
			refreshExp = fmt.Sprintf("expires %s in %d days", d.Session.RefreshExpiresAt, d.Session.RefreshInDays)
		}
		fmt.Fprintf(&b, "  Refresh token: present (%s)\n", refreshExp)
	} else {
		b.WriteString("  Refresh token: not present\n")
	}
	if len(d.Session.Scopes) > 0 {
		fmt.Fprintf(&b, "  Scopes: %s\n", strings.Join(d.Session.Scopes, " "))
	}
	if d.Session.AuthFlow != "" {
		fmt.Fprintf(&b, "  Auth flow: %s\n", d.Session.AuthFlow)
	}
	if d.Session.ConnectedAt != "" {
		fmt.Fprintf(&b, "  Connected at: %s\n", d.Session.ConnectedAt)
	}
	b.WriteString("\n")

	// LinkedIn probe
	b.WriteString("LinkedIn probe\n")
	if !d.Probe.Attempted {
		b.WriteString("  (skipped — not authenticated)\n")
	} else if d.Probe.Error != "" {
		fmt.Fprintf(&b, "  GET %s -> ERROR: %s\n", d.Probe.URL, d.Probe.Error)
	} else {
		fmt.Fprintf(&b, "  GET %s -> %d (member %s)\n", d.Probe.URL, d.Probe.Status, d.Probe.Member)
		if d.Probe.RequestID != "" {
			fmt.Fprintf(&b, "  Request ID: %s\n", d.Probe.RequestID)
		}
	}
	b.WriteString("\n")

	// Feature support
	b.WriteString("Feature support\n")
	for _, f := range d.Features {
		if f.Reason != "" {
			fmt.Fprintf(&b, "  %-20s %s (%s)\n", f.Command, f.Status, f.Reason)
		} else {
			fmt.Fprintf(&b, "  %-20s %s\n", f.Command, f.Status)
		}
	}
	b.WriteString("\n")

	// Audit
	b.WriteString("Audit\n")
	fmt.Fprintf(&b, "  Path:    %s\n", d.Audit.Path)
	fmt.Fprintf(&b, "  Enabled: %v\n", d.Audit.Enabled)
	if d.Audit.Exists {
		fmt.Fprintf(&b, "  File:    %d bytes, modified %s\n", d.Audit.Size, d.Audit.ModifiedAt)
	} else {
		b.WriteString("  File:    (not yet created)\n")
	}
	b.WriteString("\n")

	// Warnings
	if len(d.Warnings) > 0 {
		b.WriteString("Warnings\n")
		for _, msg := range d.Warnings {
			fmt.Fprintf(&b, "  ! %s\n", msg)
		}
		b.WriteString("\n")
	}

	// Errors
	if len(d.Errors) > 0 {
		b.WriteString("Errors\n")
		for _, msg := range d.Errors {
			fmt.Fprintf(&b, "  x %s\n", msg)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "Health: %s\n", d.Health)

	_, err := io.WriteString(w, b.String())
	return err
}
