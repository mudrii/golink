package httprecord_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mudrii/golink/internal/httprecord"
)

// roundTripper adapts a function to http.RoundTripper.
type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRecordReplayCycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")

	// Record phase.
	recorder, err := httprecord.Wrap(http.DefaultTransport, cassettePath, "")
	if err != nil {
		t.Fatalf("Wrap record: %v", err)
	}
	recClient := &http.Client{Transport: recorder}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", http.NoBody)
	resp, err := recClient.Do(req)
	if err != nil {
		t.Fatalf("record request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("record: status %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("record: unexpected body: %s", body)
	}

	// Replay phase.
	replayer, err := httprecord.Wrap(nil, "", cassettePath)
	if err != nil {
		t.Fatalf("Wrap replay: %v", err)
	}
	replayClient := &http.Client{Transport: replayer}

	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", http.NoBody)
	resp2, err := replayClient.Do(req2)
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("replay: status %d", resp2.StatusCode)
	}
	if !bytes.Equal(body, body2) {
		t.Errorf("replay body mismatch: got %s, want %s", body2, body)
	}
}

func TestReplayMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")

	// Record GET /recorded.
	recorder, _ := httprecord.Wrap(http.DefaultTransport, cassettePath, "")
	recClient := &http.Client{Transport: recorder}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/recorded", http.NoBody)
	resp, err := recClient.Do(req)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	// Replay a different path — should fail.
	replayer, _ := httprecord.Wrap(nil, "", cassettePath)
	replayClient := &http.Client{Transport: replayer}
	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/not-recorded", http.NoBody)
	_, err = replayClient.Do(req2)
	if err == nil {
		t.Fatal("expected error on replay miss, got nil")
	}
	if !strings.Contains(err.Error(), "no recorded response") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWrap_mutuallyExclusive(t *testing.T) {
	_, err := httprecord.Wrap(nil, "/tmp/a.jsonl", "/tmp/b.jsonl")
	if err == nil {
		t.Fatal("expected error for both RECORD and REPLAY set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWrap_noop(t *testing.T) {
	called := false
	base := roundTripper(func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	rt, err := httprecord.Wrap(base, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt == nil {
		t.Fatal("expected non-nil transport")
	}
	// Verify the returned transport is the original by calling through it.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/", http.NoBody)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	_ = resp.Body.Close()
	if !called {
		t.Error("expected original transport to be called (noop wrap should not wrap)")
	}
}

func TestHeaderRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Response headers should be preserved (non-sensitive).
		w.Header().Set("X-Custom", "visible")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")
	recorder, _ := httprecord.Wrap(http.DefaultTransport, cassettePath, "")

	// Add Authorization header to the request — must not appear in cassette.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/", http.NoBody)
	req.Header.Set("Authorization", "Bearer secret-token")

	resp, err := (&http.Client{Transport: recorder}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	raw, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	if strings.Contains(string(raw), "secret-token") {
		t.Error("Authorization token found in cassette — should be redacted")
	}
}

func TestCassetteRedactsPersonalData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sub":"urn:li:person:abc123","email":"ion@example.com","localizedFirstName":"Ion","text":"private response"}`))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")
	recorder, _ := httprecord.Wrap(http.DefaultTransport, cassettePath, "")

	body := strings.NewReader(`{"author":"urn:li:person:abc123","email":"ion@example.com","text":"private request"}`)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/v2/ugcPosts?author=urn:li:person:abc123&email=ion@example.com", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Transport: recorder}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	raw, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	cassette := string(raw)
	for _, leaked := range []string{
		"urn:li:person:abc123",
		"ion@example.com",
		"private request",
		"private response",
		"Ion",
	} {
		if strings.Contains(cassette, leaked) {
			t.Fatalf("cassette leaked %q: %s", leaked, cassette)
		}
	}

	replayer, err := httprecord.Wrap(nil, "", cassettePath)
	if err != nil {
		t.Fatalf("Wrap replay: %v", err)
	}
	replayReq, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/v2/ugcPosts?author=urn:li:person:abc123&email=ion@example.com", strings.NewReader(`{"author":"urn:li:person:abc123","email":"ion@example.com","text":"private request"}`))
	replayReq.Header.Set("Content-Type", "application/json")
	replayResp, err := (&http.Client{Transport: replayer}).Do(replayReq)
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	replayBody, _ := io.ReadAll(replayResp.Body)
	_ = replayResp.Body.Close()
	if !strings.Contains(string(replayBody), "REDACTED") {
		t.Fatalf("expected redacted replay body, got %s", replayBody)
	}
}

func TestLoadedCassetteSaveRedactsPersonalData(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.jsonl")
	outPath := filepath.Join(dir, "out.jsonl")

	rawBody := `{"email":"ion@example.com","text":"private response"}`
	line := `{"seq":1,"method":"GET","url":"https://api.linkedin.com/v2/userinfo?member=urn:li:person:abc123","body_sha256":"abc","request_body":"email=ion@example.com","response":{"status":200,"headers":{"Set-Cookie":["session=secret"]},"body_base64":"` +
		base64.StdEncoding.EncodeToString([]byte(rawBody)) + `"}}` + "\n"
	if err := os.WriteFile(inPath, []byte(line), 0o600); err != nil {
		t.Fatalf("write cassette: %v", err)
	}

	cassette, err := httprecord.LoadCassette(inPath)
	if err != nil {
		t.Fatalf("load cassette: %v", err)
	}
	if err := cassette.Save(outPath); err != nil {
		t.Fatalf("save cassette: %v", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read saved cassette: %v", err)
	}
	for _, leaked := range []string{"urn:li:person:abc123", "ion@example.com", "private response", "session=secret"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("saved cassette leaked %q: %s", leaked, raw)
		}
	}
}
