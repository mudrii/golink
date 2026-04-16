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

func TestFeatureUnavailableIs(t *testing.T) {
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
