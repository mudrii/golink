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

func TestCassetteRedactsOpaqueRequestBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`OK`))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")
	recorder, err := httprecord.Wrap(http.DefaultTransport, cassettePath, "")
	if err != nil {
		t.Fatalf("Wrap record: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/upload", strings.NewReader("raw-private-bytes"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := (&http.Client{Transport: recorder}).Do(req)
	if err != nil {
		t.Fatalf("record request: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	raw, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	if strings.Contains(string(raw), "raw-private-bytes") {
		t.Fatalf("opaque request body leaked: %s", raw)
	}
	if !strings.Contains(string(raw), `{\"redacted\":true}`) {
		t.Fatalf("cassette missing redacted placeholder: %s", raw)
	}
}

func TestCassetteRedactsFormRequestBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`OK`))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")
	recorder, err := httprecord.Wrap(http.DefaultTransport, cassettePath, "")
	if err != nil {
		t.Fatalf("Wrap record: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/rest/forms", strings.NewReader("client_secret=secret&visibility=PUBLIC"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Transport: recorder}).Do(req)
	if err != nil {
		t.Fatalf("record request: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	raw, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	cassette := string(raw)
	if strings.Contains(cassette, "client_secret=secret") {
		t.Fatalf("form request body leaked: %s", cassette)
	}
	if !strings.Contains(cassette, "visibility=PUBLIC") {
		t.Fatalf("cassette missing safe form value: %s", cassette)
	}
}

func TestRecordRejectsOversizedResponse(t *testing.T) {
	oversized := strings.Repeat("x", 8<<20+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(oversized))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "cassette.jsonl")
	recorder, err := httprecord.Wrap(http.DefaultTransport, cassettePath, "")
	if err != nil {
		t.Fatalf("Wrap record: %v", err)
	}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/oversized", http.NoBody)
	resp, err := recorder.RoundTrip(req)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected oversized response error")
	}
	if !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(cassettePath); !os.IsNotExist(statErr) {
		t.Fatalf("oversized response should not be recorded, statErr=%v", statErr)
	}
}

func TestWrap_replayMissingCassette(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	if _, err := httprecord.Wrap(nil, "", missingPath); err == nil {
		t.Fatal("expected error opening missing replay cassette, got nil")
	}
}

func TestWrap_replayMalformedCassetteLine(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "bad.jsonl")
	// Mix one valid line with a malformed one — LoadCassette should fail fast.
	good := `{"seq":1,"method":"GET","url":"https://example.com/","body_sha256":"","response":{"status":200,"headers":{},"body_base64":""}}`
	bad := `{"seq":2,"method":"GET",` // truncated, not valid JSON
	contents := good + "\n" + bad + "\n"
	if err := os.WriteFile(cassettePath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write cassette: %v", err)
	}
	_, err := httprecord.Wrap(nil, "", cassettePath)
	if err == nil {
		t.Fatal("expected parse error for malformed cassette line, got nil")
	}
	if !strings.Contains(err.Error(), "parse cassette line") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRecordReplay_replayedBodyStableAcrossReplays(t *testing.T) {
	// The recorder may re-marshal a JSON payload before persisting (key order
	// is not preserved). What MUST hold byte-for-byte is the replayed body
	// across successive replays from the same cassette: cassette → response
	// is deterministic and stable.
	payload := []byte(`{"id":"abc","count":42,"items":[1,2,3],"status":"ok"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "stable.jsonl")

	recorder, err := httprecord.Wrap(http.DefaultTransport, cassettePath, "")
	if err != nil {
		t.Fatalf("Wrap record: %v", err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/x", http.NoBody)
	resp, err := (&http.Client{Transport: recorder}).Do(req)
	if err != nil {
		t.Fatalf("record request: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	replayer, err := httprecord.Wrap(nil, "", cassettePath)
	if err != nil {
		t.Fatalf("Wrap replay: %v", err)
	}
	client := &http.Client{Transport: replayer}

	read := func(label string) ([]byte, int) {
		r, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/x", http.NoBody)
		r2, err := client.Do(r)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		b, _ := io.ReadAll(r2.Body)
		_ = r2.Body.Close()
		return b, r2.StatusCode
	}

	first, statusA := read("replay #1")
	second, statusB := read("replay #2")
	if !bytes.Equal(first, second) {
		t.Fatalf("replay body not stable across reads:\n #1=%s\n #2=%s", first, second)
	}
	if statusA != http.StatusOK || statusB != http.StatusOK {
		t.Fatalf("replay status mismatch: %d, %d", statusA, statusB)
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
