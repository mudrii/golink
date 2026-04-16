package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

	resp, err := client.Do(context.Background(), http.MethodGet, "/rest/ping", nil)
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

	resp, err := client.Do(context.Background(), http.MethodGet, "/rest/x", nil)
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

	_, err = client.Do(context.Background(), http.MethodGet, "/rest/x", nil)
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

	_, err = client.Do(context.Background(), http.MethodGet, "/rest/x", nil)
	apiErr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Fatalf("expected 401, got %+v", apiErr)
	}
}
