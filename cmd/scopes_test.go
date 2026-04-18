package cmd

import (
	"testing"

	"github.com/mudrii/golink/internal/auth"
)

func TestHasAnyScope(t *testing.T) {
	granted := []string{" w_member_social_feed ", "", "  "}
	if !hasAnyScope(granted, "w_member_social", "w_member_social_feed") {
		t.Fatal("expected to match granted scope")
	}
	if hasAnyScope(granted, "profile", "r_member_social") {
		t.Fatal("did not expect match for missing scope")
	}
}

func TestSessionHasAnyScope(t *testing.T) {
	session := auth.Session{Scopes: []string{"  profile  ", "email"}}
	if !sessionHasAnyScope(session, "profile") {
		t.Fatal("expected session scope match")
	}
}

func TestFormatScopeRequirement(t *testing.T) {
	if got := formatScopeRequirement(); got != "" {
		t.Fatalf("empty requirement = %q", got)
	}
	if got := formatScopeRequirement("scope-a"); got != "scope-a" {
		t.Fatalf("single requirement = %q", got)
	}
	if got := formatScopeRequirement("scope-a", "scope-b", "scope-c"); got != "scope-a (legacy: scope-b, scope-c)" {
		t.Fatalf("legacy format = %q", got)
	}
}
