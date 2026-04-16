package api

import "testing"

func TestEncodeURN(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"ugcPost", "urn:li:ugcPost:7123", "urn%3Ali%3AugcPost%3A7123", false},
		{"share", "urn:li:share:45", "urn%3Ali%3Ashare%3A45", false},
		{"empty", "", "", true},
		{"no prefix", "7123", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeURN(tc.input)
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
				t.Fatalf("encode: want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestActivityURNFromPost(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"share", "urn:li:share:7123", "urn:li:activity:7123", false},
		{"ugcPost", "urn:li:ugcPost:7123", "urn:li:activity:7123", false},
		{"activity pass-through", "urn:li:activity:7123", "urn:li:activity:7123", false},
		{"unknown family", "urn:li:other:7123", "", true},
		{"empty", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ActivityURNFromPost(tc.input)
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
				t.Fatalf("activity urn: want %q, got %q", tc.want, got)
			}
		})
	}
}
