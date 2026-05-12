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
	command        string
	requiredScopes []string // empty means entitlement-gated or always-unsupported
	fixedReason    string   // non-empty means always unsupported regardless of scopes
}

var featureRules = []featureRule{
	{command: "profile me", requiredScopes: []string{"openid", "profile"}},
	{command: "post create", requiredScopes: memberWriteScopes},
	{command: "post list", fixedReason: "r_member_social is closed by LinkedIn (entitlement-gated)"},
	{command: "post get", fixedReason: "r_member_social is closed by LinkedIn (entitlement-gated)"},
	{command: "post delete", requiredScopes: memberWriteScopes},
	{command: "comment add", requiredScopes: memberWriteScopes},
	{command: "comment list", fixedReason: "r_member_social is closed by LinkedIn (entitlement-gated)"},
	{command: "react add", requiredScopes: memberWriteScopes},
	{command: "react list", fixedReason: "r_member_social is closed by LinkedIn (entitlement-gated)"},
	{command: "search people", fixedReason: "not available on official transport (use --transport=unofficial)"},
	{command: "org list", requiredScopes: orgWriteScopes},
	{command: "post create --as-org", requiredScopes: orgWriteScopes},
}

func newDoctorCommand(a *app) *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose golink configuration, session, and feature support",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runDoctorCommand(cmd, strict)
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "exit 2 on warnings, exit 5 on errors")
	return cmd
}

func (a *app) runDoctorCommand(cmd *cobra.Command, strict bool) error {
	data := a.buildDoctorData(cmd.Context())
	if err := a.writeDoctorData(cmd, data); err != nil {
		return err
	}
	return doctorStrictFailure(a.settings.Output, data.Health, strict)
}

func (a *app) buildDoctorData(ctx context.Context) output.DoctorData {
	now := a.deps.Now()
	env := buildDoctorEnvironment()
	session, sessionErr := a.loadDoctorSession(ctx)
	docSession := buildDoctorSession(session, a.settings.Profile, now)
	probe := a.buildDoctorProbe(ctx, session, now)
	features := buildFeatureMap(doctorScopes(session), session)
	docAudit := buildDoctorAudit(a.doctorAuditPath(), a.settings.Audit)
	warnings, errs := doctorWarningsAndErrors(env, session, sessionErr, probe, docAudit, now)

	return output.DoctorData{
		APIVersion:  a.settings.APIVersion,
		Environment: env,
		Session:     docSession,
		Probe:       probe,
		Features:    features,
		Audit:       docAudit,
		Warnings:    warnings,
		Errors:      errs,
		Health:      doctorHealth(warnings, errs),
	}
}

func (a *app) loadDoctorSession(ctx context.Context) (*auth.Session, error) {
	session, err := a.deps.SessionStore.LoadSession(ctx, a.settings.Profile)
	if err != nil && !errors.Is(err, auth.ErrSessionNotFound) {
		return session, fmt.Errorf("load session: %w", err)
	}
	return session, err
}

func (a *app) buildDoctorProbe(ctx context.Context, session *auth.Session, now time.Time) output.DoctorProbe {
	probeTarget := a.deps.UserinfoURL
	if probeTarget == "" {
		probeTarget = userinfoURL
	}
	probe := output.DoctorProbe{URL: probeTarget, Attempted: false}
	if session == nil || session.AccessToken == "" {
		return probe
	}
	authenticated, _ := session.IsAuthenticated(now)
	if !authenticated {
		return probe
	}
	return runUserinfoProbe(ctx, a.deps.HTTPClient, a.settings.Timeout, session.AccessToken, probeTarget)
}

func doctorScopes(session *auth.Session) []string {
	if session == nil {
		return nil
	}
	return session.Scopes
}

func (a *app) doctorAuditPath() string {
	if a.settings.AuditPath != "" {
		return a.settings.AuditPath
	}
	return audit.ResolvePath()
}

func doctorWarningsAndErrors(env output.DoctorEnvironment, session *auth.Session, sessionErr error, probe output.DoctorProbe, docAudit output.DoctorAudit, now time.Time) ([]string, []string) {
	var warnings, errs []string
	warnings, errs = appendDoctorSessionFindings(warnings, errs, session, sessionErr, now)
	if !env.GOLINKClientID {
		warnings = append(warnings, "GOLINK_CLIENT_ID is not set — required for auth login and auth refresh")
	}
	if probe.Attempted && probe.Error != "" {
		errs = append(errs, fmt.Sprintf("LinkedIn probe failed: %s", probe.Error))
	}
	if probe.Attempted && probe.Status == http.StatusUnauthorized {
		errs = append(errs, "LinkedIn probe returned 401 — token may be invalid")
	}
	if !docAudit.Enabled {
		warnings = append(warnings, "audit log is disabled")
	}
	return warnings, errs
}

func appendDoctorSessionFindings(warnings, errs []string, session *auth.Session, sessionErr error, now time.Time) ([]string, []string) {
	if session == nil || errors.Is(sessionErr, auth.ErrSessionNotFound) {
		return append(warnings, "no active session — run: golink auth login"), errs
	}
	if sessionErr != nil {
		return warnings, append(errs, fmt.Sprintf("session load error: %s", sessionErr))
	}
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
	return warnings, errs
}

func doctorHealth(warnings, errs []string) string {
	switch {
	case len(errs) > 0:
		return "error"
	case len(warnings) > 0:
		return "warnings"
	default:
		return "ok"
	}
}

func (a *app) writeDoctorData(cmd *cobra.Command, data output.DoctorData) error {
	if a.settings.Output == output.ModeText {
		return writeDoctorText(a.deps.Stdout, data)
	}
	return a.writeSuccess(cmd, data, "")
}

func doctorStrictFailure(mode, health string, strict bool) error {
	if !strict {
		return nil
	}
	switch health {
	case "error":
		return &commandFailure{outputMode: mode, exitCode: 5, text: "doctor: one or more errors detected"}
	case "warnings":
		return &commandFailure{outputMode: mode, exitCode: 2, text: "doctor: one or more warnings detected"}
	default:
		return nil
	}
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
	features := make([]output.DoctorFeature, 0, len(featureRules)+1)
	for _, rule := range featureRules {
		f := output.DoctorFeature{Command: rule.command}
		switch {
		case rule.fixedReason != "":
			f.Status = "unsupported"
			f.Reason = rule.fixedReason
		case len(rule.requiredScopes) == 0:
			f.Status = "unsupported"
		default:
			if hasAnyScope(scopes, rule.requiredScopes...) {
				f.Status = "supported"
			} else {
				f.Status = "unsupported"
				f.Reason = fmt.Sprintf("requires %s which is not granted", formatScopeRequirement(rule.requiredScopes...))
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
	writeDoctorEnvironmentText(&b, d.Environment)
	writeDoctorSessionText(&b, d.Session)
	writeDoctorProbeText(&b, d.Probe)
	writeDoctorFeatureText(&b, d.Features)
	writeDoctorAuditText(&b, d.Audit)
	writeDoctorMessagesText(&b, "Warnings", "!", d.Warnings)
	writeDoctorMessagesText(&b, "Errors", "x", d.Errors)
	fmt.Fprintf(&b, "Health: %s\n", d.Health)

	_, err := io.WriteString(w, b.String())
	return err
}

func writeDoctorEnvironmentText(b *strings.Builder, env output.DoctorEnvironment) {
	b.WriteString("Environment\n")
	clientIDVal := "NOT SET"
	if env.GOLINKClientID {
		clientIDVal = "set"
	}
	fmt.Fprintf(b, "  GOLINK_CLIENT_ID:   %s\n", clientIDVal)
	apiVer := env.GOLINKAPIVersion
	if apiVer == "" {
		apiVer = "(not set; default used)"
	}
	fmt.Fprintf(b, "  GOLINK_API_VERSION: %s\n", apiVer)
	if env.GOLINKRedirect != "" {
		fmt.Fprintf(b, "  GOLINK_REDIRECT_PORT: %s\n", env.GOLINKRedirect)
	}
	if env.GOLINKJSON != "" {
		fmt.Fprintf(b, "  GOLINK_JSON:        %s\n", env.GOLINKJSON)
	}
	if env.GOLINKTransport != "" {
		fmt.Fprintf(b, "  GOLINK_TRANSPORT:   %s\n", env.GOLINKTransport)
	}
	if env.GOLINKOutput != "" {
		fmt.Fprintf(b, "  GOLINK_OUTPUT:      %s\n", env.GOLINKOutput)
	}
	auditVal := env.GOLINKAudit
	if auditVal == "" {
		auditVal = "(not set; default on)"
	}
	fmt.Fprintf(b, "  GOLINK_AUDIT:       %s\n", auditVal)
	if env.GOLINKAuditPath != "" {
		fmt.Fprintf(b, "  GOLINK_AUDIT_PATH:  %s\n", env.GOLINKAuditPath)
	}
	configPath := env.ConfigPath
	if configPath == "" {
		configPath = "(none)"
	}
	fmt.Fprintf(b, "  Config file:        %s\n\n", configPath)
}

func writeDoctorSessionText(b *strings.Builder, session output.DoctorSession) {
	fmt.Fprintf(b, "Session (profile: %s)\n", session.Profile)
	fmt.Fprintf(b, "  Authenticated: %v\n", session.Authenticated)
	if session.ExpiresAt != "" {
		fmt.Fprintf(b, "  Access expires: %s (%d hours)\n", session.ExpiresAt, session.ExpiresInHours)
	}
	if session.RefreshAvailable {
		refreshExp := "(no expiry stored)"
		if session.RefreshExpiresAt != "" {
			refreshExp = fmt.Sprintf("expires %s in %d days", session.RefreshExpiresAt, session.RefreshInDays)
		}
		fmt.Fprintf(b, "  Refresh token: present (%s)\n", refreshExp)
	} else {
		b.WriteString("  Refresh token: not present\n")
	}
	if len(session.Scopes) > 0 {
		fmt.Fprintf(b, "  Scopes: %s\n", strings.Join(session.Scopes, " "))
	}
	if session.AuthFlow != "" {
		fmt.Fprintf(b, "  Auth flow: %s\n", session.AuthFlow)
	}
	if session.ConnectedAt != "" {
		fmt.Fprintf(b, "  Connected at: %s\n", session.ConnectedAt)
	}
	b.WriteString("\n")
}

func writeDoctorProbeText(b *strings.Builder, probe output.DoctorProbe) {
	b.WriteString("LinkedIn probe\n")
	if !probe.Attempted {
		b.WriteString("  (skipped — not authenticated)\n")
	} else if probe.Error != "" {
		fmt.Fprintf(b, "  GET %s -> ERROR: %s\n", probe.URL, probe.Error)
	} else {
		fmt.Fprintf(b, "  GET %s -> %d (member %s)\n", probe.URL, probe.Status, probe.Member)
		if probe.RequestID != "" {
			fmt.Fprintf(b, "  Request ID: %s\n", probe.RequestID)
		}
	}
	b.WriteString("\n")
}

func writeDoctorFeatureText(b *strings.Builder, features []output.DoctorFeature) {
	b.WriteString("Feature support\n")
	for _, f := range features {
		if f.Reason != "" {
			fmt.Fprintf(b, "  %-20s %s (%s)\n", f.Command, f.Status, f.Reason)
		} else {
			fmt.Fprintf(b, "  %-20s %s\n", f.Command, f.Status)
		}
	}
	b.WriteString("\n")
}

func writeDoctorAuditText(b *strings.Builder, docAudit output.DoctorAudit) {
	b.WriteString("Audit\n")
	fmt.Fprintf(b, "  Path:    %s\n", docAudit.Path)
	fmt.Fprintf(b, "  Enabled: %v\n", docAudit.Enabled)
	if docAudit.Exists {
		fmt.Fprintf(b, "  File:    %d bytes, modified %s\n", docAudit.Size, docAudit.ModifiedAt)
	} else {
		b.WriteString("  File:    (not yet created)\n")
	}
	b.WriteString("\n")
}

func writeDoctorMessagesText(b *strings.Builder, title, marker string, messages []string) {
	if len(messages) == 0 {
		return
	}
	b.WriteString(title + "\n")
	for _, msg := range messages {
		fmt.Fprintf(b, "  %s %s\n", marker, msg)
	}
	b.WriteString("\n")
}
