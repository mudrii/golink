package cmd

import (
	"crypto/rand"
	"reflect"
	"testing"
	"time"
)

// TestDependencies_RandReadDefault asserts that normalizeDependencies wires in
// crypto/rand.Read by default. This is the entropy seam for command IDs and
// must never be nil at call time.
func TestDependencies_RandReadDefault(t *testing.T) {
	deps := normalizeDependencies(Dependencies{})
	if deps.RandRead == nil {
		t.Fatal("RandRead: want default crypto/rand.Read, got nil")
	}
	// Equality on function values is forbidden, but pointer-equality via
	// reflect.ValueOf().Pointer() suffices to confirm the default wiring.
	gotPtr := reflect.ValueOf(deps.RandRead).Pointer()
	wantPtr := reflect.ValueOf(rand.Read).Pointer()
	if gotPtr != wantPtr {
		t.Fatalf("RandRead: want crypto/rand.Read, got a different function")
	}
}

// TestApp_NewCommandIDUsesInjectedRandRead asserts that an injected RandRead
// drives newCommandID's random suffix deterministically, so tests can pin
// IDs without touching package-level state.
func TestApp_NewCommandIDUsesInjectedRandRead(t *testing.T) {
	deps := normalizeDependencies(Dependencies{
		RandRead: func(b []byte) (int, error) {
			for i := range b {
				b[i] = 0xAB
			}
			return len(b), nil
		},
	})
	a := newApp(BuildInfo{}, deps, nil)

	got := a.newCommandID("post create", time.Unix(1776427200, 0).UTC())
	want := "cmd_post_create_1776427200abababab"
	if got != want {
		t.Fatalf("newCommandID = %q, want %q", got, want)
	}
}
