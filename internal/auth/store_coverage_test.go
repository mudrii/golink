package auth

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestMemoryStoreLifecycle(t *testing.T) {
	store := NewMemoryStore()
	session := Session{
		Profile:     "default",
		Transport:   "official",
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	if err := store.SaveSession(t.Context(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	loaded, err := store.LoadSession(t.Context(), "default")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.AccessToken != "token" {
		t.Fatalf("access token = %q, want token", loaded.AccessToken)
	}

	if err := store.DeleteSession(t.Context(), "default"); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	_, err = store.LoadSession(t.Context(), "default")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestMemoryStoreDeleteMissing(t *testing.T) {
	store := NewMemoryStore()
	if err := store.DeleteSession(t.Context(), "missing"); !errors.Is(err, ErrSessionNotFound) {
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

func TestKeyringStoreNilBackendDefaultsToSystem(t *testing.T) {
	store := newKeyringStore("test.service", nil)
	if store.service != "test.service" {
		t.Fatalf("service = %q", store.service)
	}
	if _, ok := store.backend.(systemKeyring); !ok {
		t.Fatalf("backend = %T, want systemKeyring", store.backend)
	}
}

func TestKeyringStoreLifecycleWithBackend(t *testing.T) {
	backend := newFakeKeyringBackend()
	store := newKeyringStore("test.service", backend)
	session := Session{
		Profile:     "default",
		Transport:   "official",
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	if err := store.SaveSession(t.Context(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if backend.lastService != "test.service" || backend.lastUser != "profile:default" {
		t.Fatalf("backend key = %s/%s", backend.lastService, backend.lastUser)
	}

	loaded, err := store.LoadSession(t.Context(), "default")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.AccessToken != "token" || loaded.Profile != "default" {
		t.Fatalf("loaded session = %+v", loaded)
	}

	if err := store.DeleteSession(t.Context(), "default"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := store.LoadSession(t.Context(), "default"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("load deleted session err = %v", err)
	}
}

func TestKeyringStoreErrorsWithBackend(t *testing.T) {
	backend := newFakeKeyringBackend()
	store := newKeyringStore("test.service", backend)

	if _, err := store.LoadSession(t.Context(), "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing load err = %v", err)
	}

	backend.values["test.service/profile:bad-json"] = "{"
	if _, err := store.LoadSession(t.Context(), "bad-json"); err == nil {
		t.Fatal("expected decode error")
	}

	payload, err := json.Marshal(Session{Profile: "bad-session", Transport: "bad-transport"})
	if err != nil {
		t.Fatalf("marshal invalid session: %v", err)
	}
	backend.values["test.service/profile:bad-session"] = string(payload)
	if _, err := store.LoadSession(t.Context(), "bad-session"); err == nil {
		t.Fatal("expected validation error")
	}

	backend.setErr = errors.New("locked")
	if err := store.SaveSession(t.Context(), Session{Profile: "default", Transport: "official"}); err == nil {
		t.Fatal("expected save error")
	}
	backend.setErr = nil

	backend.deleteErr = errors.New("denied")
	if err := store.DeleteSession(t.Context(), "default"); err == nil {
		t.Fatal("expected delete error")
	}
}

type fakeKeyringBackend struct {
	values      map[string]string
	lastService string
	lastUser    string
	setErr      error
	deleteErr   error
}

func newFakeKeyringBackend() *fakeKeyringBackend {
	return &fakeKeyringBackend{values: map[string]string{}}
}

func (b *fakeKeyringBackend) key(service, user string) string {
	return service + "/" + user
}

func (b *fakeKeyringBackend) Get(service, user string) (string, error) {
	b.lastService = service
	b.lastUser = user
	value, ok := b.values[b.key(service, user)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (b *fakeKeyringBackend) Set(service, user, password string) error {
	b.lastService = service
	b.lastUser = user
	if b.setErr != nil {
		return b.setErr
	}
	b.values[b.key(service, user)] = password
	return nil
}

func (b *fakeKeyringBackend) Delete(service, user string) error {
	b.lastService = service
	b.lastUser = user
	if b.deleteErr != nil {
		return b.deleteErr
	}
	key := b.key(service, user)
	if _, ok := b.values[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(b.values, key)
	return nil
}
