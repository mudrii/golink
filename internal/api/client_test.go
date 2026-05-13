package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientAddsHeadersAndAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("authorization header = %q", got)
		}
		if got := r.Header.Get("X-Restli-Protocol-Version"); got != "2.0.0" {
			t.Fatalf("restli header = %q", got)
		}
		if got := r.Header.Get("Linkedin-Version"); got != "202604" {
			t.Fatalf("linkedin-version header = %q", got)
		}
		w.Header().Set("X-RateLimit-Remaining", "75")
		w.Header().Set("X-RateLimit-Reset", "1776355200")
		w.Header().Set("X-Restli-Id", "req-abc")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"hello":"world"}`)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:    server.URL,
		APIVersion: "202604",
		Token: func(_ context.Context) (string, error) {
			return "token-123", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Do(t.Context(), http.MethodGet, "/rest/ping", nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.RequestID != "req-abc" {
		t.Fatalf("request id = %q", resp.RequestID)
	}
	if resp.RateLimit == nil || resp.RateLimit.Remaining == nil || *resp.RateLimit.Remaining != 75 {
		t.Fatalf("rate limit = %+v", resp.RateLimit)
	}
	if resp.RateLimit.ResetAt == "" {
		t.Fatal("expected reset_at populated")
	}
	var decoded map[string]string
	if err := resp.UnmarshalJSON(&decoded); err != nil || decoded["hello"] != "world" {
		t.Fatalf("decode: %v payload=%v", err, decoded)
	}
}

func TestClientRetriesOn429And5xx(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		switch n {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:      server.URL,
		RetryMax:     5,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: time.Millisecond,
		Token: func(_ context.Context) (string, error) {
			return "t", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Do(t.Context(), http.MethodGet, "/rest/x", nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d", resp.Status)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 calls, got %d", got)
	}
}

func TestClientHonorsRetryAfterSeconds(t *testing.T) {
	// Given a server that returns 429 with Retry-After: 1 on the first call
	// then 200 on the second, the retryable client must wait at least the
	// requested duration before retrying — not the LinearJitterBackoff
	// configured for ordinary 5xx waits.
	var calls atomic.Int32
	var firstAt, secondAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		switch n {
		case 1:
			firstAt = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			secondAt = time.Now()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:      server.URL,
		RetryMax:     2,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
		Token: func(_ context.Context) (string, error) {
			return "t", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Do(t.Context(), http.MethodGet, "/rest/x", nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d", resp.Status)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", calls.Load())
	}
	delta := secondAt.Sub(firstAt)
	if delta < 900*time.Millisecond {
		t.Fatalf("retry after %v; Retry-After: 1 not honored (RetryWaitMax was 5ms)", delta)
	}
}

func TestClientHonorsRetryAfterHTTPDate(t *testing.T) {
	// Same property but Retry-After expressed as an HTTP-date.
	var calls atomic.Int32
	var firstAt, secondAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		switch n {
		case 1:
			firstAt = time.Now()
			// HTTP-date resolution is 1 second, so offset by 2s and assert
			// delta > 800ms — at worst-case truncation we still see ~1s.
			w.Header().Set("Retry-After", time.Now().Add(2*time.Second).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			secondAt = time.Now()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:      server.URL,
		RetryMax:     2,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
		Token: func(_ context.Context) (string, error) {
			return "t", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Do(t.Context(), http.MethodGet, "/rest/x", nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d", resp.Status)
	}
	delta := secondAt.Sub(firstAt)
	if delta < 800*time.Millisecond {
		t.Fatalf("retry after %v; HTTP-date Retry-After not honored", delta)
	}
}

func TestClientDecodesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Restli-Id", "req-err")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  403,
			"code":    "INSUFFICIENT_PERMISSIONS",
			"message": "Not entitled",
		})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL: server.URL,
		Token: func(_ context.Context) (string, error) {
			return "t", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Do(t.Context(), http.MethodGet, "/rest/x", nil)
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if !apiErr.IsForbidden() {
		t.Fatalf("expected 403, got %+v", apiErr)
	}
	if apiErr.Code != "INSUFFICIENT_PERMISSIONS" || apiErr.Message != "Not entitled" {
		t.Fatalf("decoded = %+v", apiErr)
	}
	if apiErr.RequestID != "req-err" {
		t.Fatalf("request id = %q", apiErr.RequestID)
	}
}

func TestDecodeError_RedactsURNFromDetails(t *testing.T) {
	body := []byte(`{"status":403,"code":"ACCESS_DENIED","message":"denied","details":"member urn:li:person:abc123 not allowed"}`)
	apiErr := decodeError(http.StatusForbidden, body, "req-1")
	if apiErr == nil {
		t.Fatal("expected *Error")
	}
	if strings.Contains(apiErr.Details, "urn:li:person:abc123") {
		t.Fatalf("Details leaked member URN: %q", apiErr.Details)
	}
	if apiErr.Code != "ACCESS_DENIED" {
		t.Fatalf("Code should still decode: %q", apiErr.Code)
	}
	if apiErr.Message != "denied" {
		t.Fatalf("Message should still decode: %q", apiErr.Message)
	}
}

func TestDecodeError_RedactsURNFromNonJSONBody(t *testing.T) {
	body := []byte("plain text mentioning urn:li:person:abc123")
	apiErr := decodeError(http.StatusForbidden, body, "req-1")
	if strings.Contains(apiErr.Message, "urn:li:person:abc123") {
		t.Fatalf("Message leaked URN: %q", apiErr.Message)
	}
}

func TestParseRateLimitUnixSeconds(t *testing.T) {
	header := http.Header{}
	header.Set("X-RateLimit-Remaining", "12")
	header.Set("X-RateLimit-Reset", strconv.FormatInt(time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC).Unix(), 10))

	info := parseRateLimit(header)
	if info == nil || info.Remaining == nil || *info.Remaining != 12 {
		t.Fatalf("remaining = %+v", info)
	}
	if info.ResetAt != "2026-04-16T12:00:00Z" {
		t.Fatalf("reset_at = %q", info.ResetAt)
	}
}

func TestNormalizeRateResetVariants(t *testing.T) {
	if got := normalizeRateReset(" 2026-04-16T12:00:00Z "); got != "2026-04-16T12:00:00Z" {
		t.Fatalf("RFC3339 reset = %q", got)
	}
	if got := normalizeRateReset("not-a-time"); got != "not-a-time" {
		t.Fatalf("opaque reset = %q", got)
	}
	if got := normalizeRateReset("  "); got != "" {
		t.Fatalf("blank reset = %q", got)
	}
}

func TestResolveURLEdgeCases(t *testing.T) {
	client, err := NewClient(ClientConfig{
		BaseURL: "https://api.linkedin.test/base/",
		Token: func(_ context.Context) (string, error) {
			return "token", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if _, err := client.resolveURL(""); err == nil {
		t.Fatal("expected empty relative path error")
	}
	if _, err := client.resolveURL("http://[::1"); err == nil {
		t.Fatal("expected invalid URL error")
	}
	resolved, err := client.resolveURL("../rest/posts?q=hello%20world#frag")
	if err != nil {
		t.Fatalf("resolve URL: %v", err)
	}
	if got, want := resolved.String(), "https://api.linkedin.test/rest/posts?q=hello%20world#frag"; got != want {
		t.Fatalf("resolved URL = %q, want %q", got, want)
	}
}

func TestClient_DoesNotRetryPermanentNetworkError(t *testing.T) {
	// Reserve a port then close the listener so dialing yields a permanent
	// connection-refused error (no DNS lookup involved).
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	client, err := NewClient(ClientConfig{
		BaseURL:      "http://" + addr,
		RetryMax:     5,
		RetryWaitMin: 50 * time.Millisecond,
		RetryWaitMax: 50 * time.Millisecond,
		Token: func(_ context.Context) (string, error) {
			return "t", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	start := time.Now()
	_, err = client.Do(t.Context(), http.MethodGet, "/rest/x", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected network error, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("permanent connection-refused should not have retried; elapsed = %s", elapsed)
	}
}

func TestResolveURLBlocksAbsoluteOverride(t *testing.T) {
	var serverHits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&serverHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL: server.URL,
		Token: func(_ context.Context) (string, error) {
			return "secret-token", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Do(t.Context(), http.MethodGet, "https://attacker.example.com/steal", nil)
	if err == nil {
		t.Fatal("expected error for cross-host absolute URL, got nil")
	}
	if atomic.LoadInt32(&serverHits) != 0 {
		t.Fatalf("legitimate server should not have been hit, hits = %d", serverHits)
	}
	if !strings.Contains(err.Error(), "cross-host") {
		t.Fatalf("expected cross-host error, got: %v", err)
	}
}

func TestClientNoTokenRejects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		BaseURL: server.URL,
		Token: func(_ context.Context) (string, error) {
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Do(t.Context(), http.MethodGet, "/rest/x", nil)
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Fatalf("expected 401, got %+v", apiErr)
	}
}
