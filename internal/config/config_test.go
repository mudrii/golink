package config

import "testing"

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
