package api

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorClassifiers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		check  func(*Error) bool
	}{
		{"401", 401, (*Error).IsUnauthorized},
		{"403", 403, (*Error).IsForbidden},
		{"404", 404, (*Error).IsNotFound},
		{"422", 422, (*Error).IsValidation},
		{"429", 429, (*Error).IsRateLimited},
		{"503", 503, (*Error).IsServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := &Error{Status: tc.status}
			if !tc.check(err) {
				t.Fatalf("expected classifier to match for status %d", tc.status)
			}
		})
	}
}

func TestErrorStringBranches(t *testing.T) {
	if got := (*Error)(nil).Error(); got != "" {
		t.Fatalf("nil Error() = %q, want empty", got)
	}
	if got := (&Error{Status: 403, Code: "ACCESS_DENIED", Message: "forbidden"}).Error(); got != "linkedin 403 ACCESS_DENIED: forbidden" {
		t.Fatalf("coded Error() = %q", got)
	}
	if got := (&Error{Status: 500, Message: "upstream failed"}).Error(); got != "linkedin 500: upstream failed" {
		t.Fatalf("plain Error() = %q", got)
	}
}

func TestFeatureUnavailableIs(t *testing.T) {
	if got := (*ErrFeatureUnavailable)(nil).Error(); got != "" {
		t.Fatalf("nil ErrFeatureUnavailable Error() = %q, want empty", got)
	}

	err := fmt.Errorf("wrapped: %w", &ErrFeatureUnavailable{Feature: "search people", Reason: "not entitled"})
	if !errors.Is(err, &ErrFeatureUnavailable{}) {
		t.Fatal("errors.Is should match unavailable sentinel")
	}

	fe, ok := AsFeatureUnavailable(err)
	if !ok {
		t.Fatal("AsFeatureUnavailable should recover the typed error")
	}
	if fe.Feature != "search people" {
		t.Fatalf("unexpected feature: %q", fe.Feature)
	}
}

func TestAsError(t *testing.T) {
	err := fmt.Errorf("outer: %w", &Error{Status: 429, Code: "TOO_MANY_REQUESTS", Message: "slow down"})
	ae, ok := AsError(err)
	if !ok {
		t.Fatal("AsError should unwrap through fmt.Errorf")
	}
	if !ae.IsRateLimited() {
		t.Fatalf("expected rate limit classifier, got %+v", ae)
	}
}
