package config

import "testing"

func TestResolveAuditEnabled(t *testing.T) {
	tests := []struct {
		raw     string
		want    bool
		wantErr bool
	}{
		{raw: "", want: true},
		{raw: "on", want: true},
		{raw: "true", want: true},
		{raw: "1", want: true},
		{raw: "yes", want: true},
		{raw: "off", want: false},
		{raw: "false", want: false},
		{raw: "0", want: false},
		{raw: "no", want: false},
		{raw: "ON", want: true},
		{raw: "OFF", want: false},
		{raw: "garbage", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := resolveAuditEnabled(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveAuditEnabled(%q): want %v, got %v", tc.raw, tc.want, got)
			}
		})
	}
}

func TestSettingsValidate(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		wantErr  bool
	}{
		{
			name: "valid",
			settings: Settings{
				Profile:   "default",
				Transport: "official",
				Timeout:   defaultTimeout,
			},
		},
		{
			name: "missing profile",
			settings: Settings{
				Transport: "official",
				Timeout:   defaultTimeout,
			},
			wantErr: true,
		},
		{
			name: "invalid transport",
			settings: Settings{
				Profile:   "default",
				Transport: "broken",
				Timeout:   defaultTimeout,
			},
			wantErr: true,
		},
		{
			name: "timeout too large",
			settings: Settings{
				Profile:   "default",
				Transport: "official",
				Timeout:   maximumTimeout + 1,
			},
			wantErr: true,
		},
		{
			name: "invalid auth flow",
			settings: Settings{
				Profile:   "default",
				Transport: "official",
				Timeout:   defaultTimeout,
				AuthFlow:  "native",
			},
			wantErr: true,
		},
		{
			name: "oauth2 requires client secret",
			settings: Settings{
				Profile:   "default",
				Transport: "official",
				Timeout:   defaultTimeout,
				AuthFlow:  "oauth2",
			},
			wantErr: true,
		},
		{
			name: "oauth2 with client secret",
			settings: Settings{
				Profile:      "default",
				Transport:    "official",
				Timeout:      defaultTimeout,
				AuthFlow:     "oauth2",
				ClientSecret: "secret",
				RedirectPort: 8080,
			},
		},
		{
			name: "oauth2 requires fixed redirect port",
			settings: Settings{
				Profile:      "default",
				Transport:    "official",
				Timeout:      defaultTimeout,
				AuthFlow:     "oauth2",
				ClientSecret: "secret",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.settings.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoaderReadsAuthOverridesFromEnv(t *testing.T) {
	t.Setenv("GOLINK_AUTH_SCOPES", "w_member_social_feed")
	t.Setenv("GOLINK_MEMBER_URN", "urn:li:person:abc123")
	t.Setenv("GOLINK_AUTH_FLOW", "oauth2")
	t.Setenv("GOLINK_CLIENT_SECRET", "secret")
	t.Setenv("GOLINK_REDIRECT_PORT", "8080")

	settings, err := NewLoader().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.AuthScopes != "w_member_social_feed" {
		t.Fatalf("AuthScopes = %q", settings.AuthScopes)
	}
	if settings.MemberURN != "urn:li:person:abc123" {
		t.Fatalf("MemberURN = %q", settings.MemberURN)
	}
	if settings.AuthFlow != "oauth2" {
		t.Fatalf("AuthFlow = %q", settings.AuthFlow)
	}
	if settings.ClientSecret != "secret" {
		t.Fatalf("ClientSecret = %q", settings.ClientSecret)
	}
	if settings.RedirectPort != 8080 {
		t.Fatalf("RedirectPort = %d", settings.RedirectPort)
	}
}
