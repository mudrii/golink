package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mudrii/golink/internal/auth"
	"github.com/mudrii/golink/internal/config"
)

// TestMaybeRefreshSessionSerialisesConcurrentRefreshes guards the M13 fix:
// when two callers race into maybeRefreshSession with a near-expiry session,
// only ONE refresh request reaches the token endpoint. The first caller takes
// the flock, refreshes, and persists; the second caller waits for the lock,
// re-reads the now-fresh session, and skips the network call.
func TestMaybeRefreshSessionSerialisesConcurrentRefreshes(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	// Block briefly so the second goroutine has time to queue on the lock
	// before the first caller's request completes and the session is saved.
	releaseRefresh := make(chan struct{})
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCount.Add(1)
		<-releaseRefresh
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"new-token","expires_in":3600,"refresh_token":"new-refresh","refresh_token_expires_in":7200,"scope":"openid profile"}`)
	}))
	defer tokenServer.Close()

	store := auth.NewMemoryStore()
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	if err := store.SaveSession(t.Context(), auth.Session{
		Profile:      "default",
		Transport:    "official",
		AccessToken:  "old-token",
		ExpiresAt:    now.Add(2 * time.Minute), // inside the 5-min refresh window
		RefreshToken: "refresh-token",
		MemberURN:    "urn:li:person:abc123",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	deps := normalizeDependencies(Dependencies{
		HTTPClient:      tokenServer.Client(),
		TokenURL:        tokenServer.URL,
		SessionStore:    store,
		Now:             func() time.Time { return now },
		RefreshLockPath: filepath.Join(t.TempDir(), "refresh.lock"),
	})
	a := &app{
		deps:     deps,
		settings: config.Settings{ClientID: "client-123"},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	loaded, err := store.LoadSession(t.Context(), "default")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}

	const workers = 2
	var (
		wg      sync.WaitGroup
		results = make([]auth.Session, workers)
		errs    = make([]error, workers)
	)
	wg.Add(workers)
	for i := range workers {
		go func(idx int) {
			defer wg.Done()
			session, refreshErr := a.maybeRefreshSession(t.Context(), *loaded)
			results[idx] = session
			errs[idx] = refreshErr
		}(i)
	}

	// Wait long enough that both goroutines have entered maybeRefreshSession
	// and one is parked on the flock. With a 10ms slack the first request
	// is guaranteed to have hit the server and incremented refreshCount.
	deadline := time.Now().Add(2 * time.Second)
	for refreshCount.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	close(releaseRefresh)
	wg.Wait()

	if got := refreshCount.Load(); got != 1 {
		t.Fatalf("token endpoint refresh count: want 1, got %d (concurrent processes both hit the wire — M13 race not serialised)", got)
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d returned error: %v", i, err)
		}
		if results[i].AccessToken != "new-token" {
			t.Errorf("worker %d access token: want new-token, got %q", i, results[i].AccessToken)
		}
	}

	stored, err := store.LoadSession(t.Context(), "default")
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if stored.AccessToken != "new-token" {
		t.Fatalf("persisted access token: want new-token, got %q", stored.AccessToken)
	}
}

// TestMaybeRefreshSessionSkipsWhenAlreadyFresh asserts the post-lock re-read
// path: if another process refreshed the session while we waited for the
// flock, the caller observes the freshly stored token and skips the network.
func TestMaybeRefreshSessionSkipsWhenAlreadyFresh(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"unexpected","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	store := auth.NewMemoryStore()
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	// Persisted session is already fresh (60 minutes out); the stale copy
	// the caller hands in still claims to be near expiry.
	if err := store.SaveSession(t.Context(), auth.Session{
		Profile:      "default",
		Transport:    "official",
		AccessToken:  "fresh-token",
		ExpiresAt:    now.Add(60 * time.Minute),
		RefreshToken: "refresh-token",
		MemberURN:    "urn:li:person:abc123",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	deps := normalizeDependencies(Dependencies{
		HTTPClient:      tokenServer.Client(),
		TokenURL:        tokenServer.URL,
		SessionStore:    store,
		Now:             func() time.Time { return now },
		RefreshLockPath: filepath.Join(t.TempDir(), "refresh.lock"),
	})
	a := &app{
		deps:     deps,
		settings: config.Settings{ClientID: "client-123"},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	stale := auth.Session{
		Profile:      "default",
		Transport:    "official",
		AccessToken:  "stale-token",
		ExpiresAt:    now.Add(2 * time.Minute),
		RefreshToken: "refresh-token",
	}

	session, err := a.maybeRefreshSession(t.Context(), stale)
	if err != nil {
		t.Fatalf("maybeRefreshSession: %v", err)
	}
	if got := refreshCount.Load(); got != 0 {
		t.Fatalf("token endpoint hits: want 0 (post-lock re-read should see fresh session), got %d", got)
	}
	if session.AccessToken != "fresh-token" {
		t.Errorf("returned access token: want fresh-token (from store), got %q", session.AccessToken)
	}
}

// saveFailingStore wraps an auth.Store and forces SaveSession to return an
// error. Used by TestMaybeRefreshSessionFailsOnSaveError to verify M4: the
// refresh contract requires that we never return a refreshed session that
// has not been durably persisted.
type saveFailingStore struct {
	inner   auth.Store
	saveErr error
}

func (s *saveFailingStore) LoadSession(ctx context.Context, profile string) (*auth.Session, error) {
	return s.inner.LoadSession(ctx, profile)
}

func (s *saveFailingStore) SaveSession(context.Context, auth.Session) error {
	return s.saveErr
}

func (s *saveFailingStore) DeleteSession(ctx context.Context, profile string) error {
	return s.inner.DeleteSession(ctx, profile)
}

// TestMaybeRefreshSessionFailsOnSaveError guards M4: if SaveSession fails after
// a successful network refresh, maybeRefreshSession must return the persistence
// error (not the new session). Otherwise the in-memory caller would use the new
// access token while the next CLI invocation reloads a stale refresh-token,
// double-consuming a rotated credential.
func TestMaybeRefreshSessionFailsOnSaveError(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"new-token","expires_in":3600,"refresh_token":"new-refresh"}`)
	}))
	defer tokenServer.Close()

	inner := auth.NewMemoryStore()
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	if err := inner.SaveSession(t.Context(), auth.Session{
		Profile:      "default",
		Transport:    "official",
		AccessToken:  "old-token",
		ExpiresAt:    now.Add(2 * time.Minute),
		RefreshToken: "old-refresh",
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	wantErr := errors.New("disk full")
	store := &saveFailingStore{inner: inner, saveErr: wantErr}

	deps := normalizeDependencies(Dependencies{
		HTTPClient:      tokenServer.Client(),
		TokenURL:        tokenServer.URL,
		SessionStore:    store,
		Now:             func() time.Time { return now },
		RefreshLockPath: filepath.Join(t.TempDir(), "refresh.lock"),
	})
	a := &app{
		deps:     deps,
		settings: config.Settings{ClientID: "client-123"},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	stale, err := inner.LoadSession(t.Context(), "default")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}

	session, err := a.maybeRefreshSession(t.Context(), *stale)
	if err == nil {
		t.Fatalf("expected error from SaveSession failure, got nil; session=%+v", session)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error chain: want %v, got %v", wantErr, err)
	}
	// On save failure the caller must observe the OLD session, not the
	// half-persisted refresh result.
	if session.AccessToken != "old-token" {
		t.Errorf("returned access token after save failure: want old-token, got %q", session.AccessToken)
	}
}

// TestMaybeRefreshSessionFailsOnLockError guards M3: if the refresh lock cannot
// be acquired, the function must hard-fail rather than silently fall through
// to an unserialised refresh.
func TestMaybeRefreshSessionFailsOnLockError(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("token endpoint should not be hit when lock fails")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tokenServer.Close()

	store := auth.NewMemoryStore()
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	if err := store.SaveSession(t.Context(), auth.Session{
		Profile:      "default",
		Transport:    "official",
		AccessToken:  "old-token",
		ExpiresAt:    now.Add(2 * time.Minute),
		RefreshToken: "old-refresh",
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// Point RefreshLockPath at a path that cannot be created because its
	// parent is a regular file, not a directory. acquireRefreshLock's
	// MkdirAll will return an error.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	lockPath := filepath.Join(blocker, "refresh.lock")

	deps := normalizeDependencies(Dependencies{
		HTTPClient:      tokenServer.Client(),
		TokenURL:        tokenServer.URL,
		SessionStore:    store,
		Now:             func() time.Time { return now },
		RefreshLockPath: lockPath,
	})
	a := &app{
		deps:     deps,
		settings: config.Settings{ClientID: "client-123"},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	stale, err := store.LoadSession(t.Context(), "default")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}

	if _, err := a.maybeRefreshSession(t.Context(), *stale); err == nil {
		t.Fatalf("expected hard-fail on lock error, got nil")
	}
}
