package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestNewClient_IgnoresEnvVars pins the seam: NewClient must not read
// GOLINK_RECORD / GOLINK_REPLAY (or any other process env) at construction
// time. Environment lookups belong in cmd/ boot; the api package only
// honors fields on ClientConfig. With env vars pointing at a cassette path
// inside a t.TempDir, no file must be created — that proves the record
// transport never engaged.
func TestNewClient_IgnoresEnvVars(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	cassettePath := filepath.Join(t.TempDir(), "env-driven.jsonl")
	t.Setenv("GOLINK_RECORD", cassettePath)
	t.Setenv("GOLINK_REPLAY", "")

	client, err := NewClient(ClientConfig{
		BaseURL: server.URL,
		Token: func(_ context.Context) (string, error) {
			return "tok", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Do(t.Context(), http.MethodGet, "/rest/ping", nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Status)
	}
	if _, err := os.Stat(cassettePath); !os.IsNotExist(err) {
		t.Fatalf("cassette file unexpectedly created: stat err = %v (want IsNotExist)", err)
	}
}
