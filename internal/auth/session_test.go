package auth

import "testing"

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
