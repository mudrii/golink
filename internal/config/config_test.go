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
