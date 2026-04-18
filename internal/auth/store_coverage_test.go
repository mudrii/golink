package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreLifecycle(t *testing.T) {
	store := NewMemoryStore()
	session := Session{
		Profile:     "default",
		Transport:   "official",
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	if err := store.SaveSession(context.Background(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	loaded, err := store.LoadSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.AccessToken != "token" {
		t.Fatalf("access token = %q, want token", loaded.AccessToken)
	}

	if err := store.DeleteSession(context.Background(), "default"); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	_, err = store.LoadSession(context.Background(), "default")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestMemoryStoreDeleteMissing(t *testing.T) {
	store := NewMemoryStore()
	if err := store.DeleteSession(context.Background(), "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestKeyringHelpers(t *testing.T) {
	if got := NewKeyringStore("").service; got != defaultServiceName {
		t.Fatalf("default service = %q, want %q", got, defaultServiceName)
	}
	if got := NewKeyringStore("custom.service").service; got != "custom.service" {
		t.Fatalf("custom service = %q", got)
	}
	if got := sessionKey("profile-a"); got != "profile:profile-a" {
		t.Fatalf("sessionKey = %q", got)
	}
}
