// Package output owns the canonical CLI response envelopes, JSON schema,
// and parsers for the enum types exposed to users.
package output

import (
	"fmt"
	"strings"
)

// ParseVisibility normalizes user input into a Visibility enum.
func ParseVisibility(raw string) (Visibility, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(VisibilityPublic):
		return VisibilityPublic, nil
	case string(VisibilityConnections):
		return VisibilityConnections, nil
	case string(VisibilityLoggedIn):
		return VisibilityLoggedIn, nil
	default:
		return "", fmt.Errorf("visibility must be PUBLIC|CONNECTIONS|LOGGED_IN")
	}
}

// ParseReactionType normalizes user input into a ReactionType enum.
func ParseReactionType(raw string) (ReactionType, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(ReactionLike):
		return ReactionLike, nil
	case string(ReactionPraise):
		return ReactionPraise, nil
	case string(ReactionEmpathy):
		return ReactionEmpathy, nil
	case string(ReactionInterest):
		return ReactionInterest, nil
	case string(ReactionAppreciation):
		return ReactionAppreciation, nil
	case string(ReactionEntertainment):
		return ReactionEntertainment, nil
	default:
		return "", fmt.Errorf("type must be LIKE|PRAISE|EMPATHY|INTEREST|APPRECIATION|ENTERTAINMENT")
	}
}
