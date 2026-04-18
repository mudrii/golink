package cmd

import (
	"fmt"
	"strings"

	"github.com/mudrii/golink/internal/auth"
)

var (
	memberWriteScopes = []string{"w_member_social_feed", "w_member_social"}
	orgWriteScopes    = []string{"w_organization_social_feed", "w_organization_social"}
)

func hasAnyScope(granted []string, required ...string) bool {
	scopeSet := make(map[string]struct{}, len(granted))
	for _, scope := range granted {
		scopeSet[strings.TrimSpace(scope)] = struct{}{}
	}

	for _, scope := range required {
		if _, ok := scopeSet[strings.TrimSpace(scope)]; ok {
			return true
		}
	}

	return false
}

func sessionHasAnyScope(session auth.Session, required ...string) bool {
	return hasAnyScope(session.Scopes, required...)
}

func formatScopeRequirement(required ...string) string {
	if len(required) == 0 {
		return ""
	}
	if len(required) == 1 {
		return required[0]
	}
	return fmt.Sprintf("%s (legacy: %s)", required[0], strings.Join(required[1:], ", "))
}
