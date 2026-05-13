package output

import (
	"encoding/json"
	"testing"
)

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

// TestScheduleRunStatus_Marshal pins the JSON wire values for the three
// terminal buckets emitted by `schedule run`. Schema-first: the contract in
// schemas/golink-output.schema.json requires these exact strings, so the test
// fails loudly if the typed enum ever drifts from the schema enum.
func TestScheduleRunStatus_Marshal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status ScheduleRunStatus
		want   string
	}{
		{"succeeded", ScheduleRunStatusSucceeded, `"succeeded"`},
		{"failed", ScheduleRunStatusFailed, `"failed"`},
		{"skipped", ScheduleRunStatusSkipped, `"skipped"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.status)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tc.want {
				t.Fatalf("marshal: want %s, got %s", tc.want, string(b))
			}
		})
	}
}
