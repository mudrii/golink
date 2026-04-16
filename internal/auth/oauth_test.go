package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildLoginRequest(t *testing.T) {
	request, err := BuildLoginRequest(context.Background(), "client-123", 0, []string{"openid", "profile"})
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}
	if request.CallbackListener == nil {
		t.Fatal("expected callback listener")
	}
	defer func() { _ = request.CallbackListener.Close() }()

	parsed, err := url.Parse(request.URL)
	if err != nil {
		t.Fatalf("parse request url: %v", err)
	}
	if got := parsed.Query().Get("client_id"); got != "client-123" {
		t.Fatalf("unexpected client_id: %q", got)
	}
	if got := parsed.Query().Get("redirect_uri"); got != request.RedirectURI {
		t.Fatalf("unexpected redirect uri: %q", got)
	}
	if got := parsed.Query().Get("code_challenge_method"); got != "S256" {
		t.Fatalf("unexpected challenge method: %q", got)
	}
}

func TestWaitForOAuthCallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan *CallbackResult, 1)
	errorCh := make(chan error, 1)
	go func() {
		result, waitErr := WaitForOAuthCallback(ctx, listener, "state-123")
		if waitErr != nil {
			errorCh <- waitErr
			return
		}
		resultCh <- result
	}()

	callbackURL := fmt.Sprintf("http://%s/callback?code=abc&state=state-123", listener.Addr().String())
	response, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("get callback: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	select {
	case err := <-errorCh:
		t.Fatalf("wait callback: %v", err)
	case result := <-resultCh:
		if result.Code != "abc" {
			t.Fatalf("unexpected code: %q", result.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCompleteLogin(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Fatalf("unexpected grant type: %q", got)
		}
		if got := r.Form.Get("code_verifier"); got == "" {
			t.Fatal("expected code verifier")
		}

		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "token-123",
			ExpiresIn:   3600,
			Scope:       "openid profile email w_member_social",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected authorization header: %q", got)
		}

		_ = json.NewEncoder(w).Encode(UserInfo{
			Sub:     "urn:li:person:abc123",
			Name:    "Ion Mudreac",
			Email:   "ion@example.com",
			Picture: "https://example.com/picture.jpg",
			Locale: struct {
				Country  string `json:"country"`
				Language string `json:"language"`
			}{
				Country:  "MY",
				Language: "en",
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	request, err := BuildLoginRequest(context.Background(), "client-123", 0, []string{"openid", "profile", "email"})
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}

	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(25 * time.Millisecond)
		callbackURL := fmt.Sprintf("%s?code=callback-code&state=%s", request.RedirectURI, request.State)
		response, callbackErr := http.Get(callbackURL)
		if callbackErr != nil {
			t.Errorf("callback get: %v", callbackErr)
			return
		}
		_ = response.Body.Close()
	}()

	session, err := CompleteLogin(ctx, request, "default", "official", LoginFlowOptions{
		HTTPClient:  server.Client(),
		Interactive: false,
		Now:         func() time.Time { return now },
		TokenURL:    server.URL + "/oauth/token",
		UserInfoURL: server.URL + "/userinfo",
	})
	if err != nil {
		t.Fatalf("complete login: %v", err)
	}

	if session.AccessToken != "token-123" {
		t.Fatalf("unexpected access token: %q", session.AccessToken)
	}
	if session.MemberURN != "urn:li:person:abc123" {
		t.Fatalf("unexpected member urn: %q", session.MemberURN)
	}
	if session.ProfileID != "abc123" {
		t.Fatalf("unexpected profile id: %q", session.ProfileID)
	}
	if !strings.Contains(strings.Join(session.Scopes, " "), "w_member_social") {
		t.Fatalf("unexpected scopes: %#v", session.Scopes)
	}
}
