package output

import "testing"

func TestParseVisibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		want    Visibility
		wantErr bool
	}{
		{"public", "PUBLIC", VisibilityPublic, false},
		{"connections lower", "connections", VisibilityConnections, false},
		{"padded", "  LOGGED_IN  ", VisibilityLoggedIn, false},
		{"invalid", "friends", "", true},
		{"empty", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseVisibility(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("visibility: want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestParseReactionType(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		want    ReactionType
		wantErr bool
	}{
		{"like", "LIKE", ReactionLike, false},
		{"praise lower", "praise", ReactionPraise, false},
		{"empathy padded", " EMPATHY ", ReactionEmpathy, false},
		{"interest", "INTEREST", ReactionInterest, false},
		{"appreciation", "APPRECIATION", ReactionAppreciation, false},
		{"entertainment", "ENTERTAINMENT", ReactionEntertainment, false},
		{"deprecated maybe", "MAYBE", "", true},
		{"invalid", "love", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseReactionType(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("reaction: want %q, got %q", tc.want, got)
			}
		})
	}
}
