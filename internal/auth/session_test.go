package auth

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionValidate(t *testing.T) {
	tests := []struct {
		name    string
		session Session
		wantErr bool
	}{
		{
			name: "valid",
			session: Session{
				Profile:   "default",
				Transport: "official",
			},
		},
		{
			name: "missing profile",
			session: Session{
				Transport: "official",
			},
			wantErr: true,
		},
		{
			name: "invalid transport",
			session: Session{
				Profile:   "default",
				Transport: "broken",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.session.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSessionRefreshFieldsRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	original := Session{
		Profile:          "default",
		Transport:        "official",
		AccessToken:      "access-token",
		RefreshToken:     "refresh-token",
		ExpiresAt:        now.Add(60 * 24 * time.Hour),
		RefreshExpiresAt: now.Add(365 * 24 * time.Hour),
		Scopes:           []string{"openid", "profile"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Session
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.RefreshToken != original.RefreshToken {
		t.Errorf("refresh token mismatch: got %q want %q", decoded.RefreshToken, original.RefreshToken)
	}
	if !decoded.RefreshExpiresAt.Equal(original.RefreshExpiresAt) {
		t.Errorf("refresh_expires_at mismatch: got %v want %v", decoded.RefreshExpiresAt, original.RefreshExpiresAt)
	}
}

func TestSessionIsAuthenticatedWithRefreshToken(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		session       Session
		wantAuthentic bool
	}{
		{
			name: "valid access token",
			session: Session{
				Profile:      "default",
				Transport:    "official",
				AccessToken:  "token",
				ExpiresAt:    now.Add(time.Hour),
				RefreshToken: "refresh",
			},
			wantAuthentic: true,
		},
		{
			name: "expired access token but refresh present",
			session: Session{
				Profile:          "default",
				Transport:        "official",
				AccessToken:      "token",
				ExpiresAt:        now.Add(-time.Hour),
				RefreshToken:     "refresh",
				RefreshExpiresAt: now.Add(365 * 24 * time.Hour),
			},
			wantAuthentic: false,
		},
		{
			name: "expired access token no refresh",
			session: Session{
				Profile:     "default",
				Transport:   "official",
				AccessToken: "token",
				ExpiresAt:   now.Add(-time.Hour),
			},
			wantAuthentic: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.session.IsAuthenticated(now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantAuthentic {
				t.Errorf("IsAuthenticated = %v, want %v", got, tc.wantAuthentic)
			}
		})
	}
}
