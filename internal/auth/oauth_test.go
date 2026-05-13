package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildLoginRequest(t *testing.T) {
	request, err := BuildLoginRequest(t.Context(), "client-123", 0, []string{"openid", "profile"})
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
	if len(request.State) < 43 {
		t.Fatalf("oauth state too short: %d", len(request.State))
	}
	if len(request.CodeVerifier) < 43 {
		t.Fatalf("code verifier too short: %d", len(request.CodeVerifier))
	}
	if got := parsed.Query().Get("code_challenge"); len(got) != 43 {
		t.Fatalf("unexpected code challenge length: %d", len(got))
	}
}

func TestBuildLoginRequestWithOptionsOAuth2(t *testing.T) {
	request, err := BuildLoginRequestWithOptions(t.Context(), LoginRequestOptions{
		ClientID:      "client-123",
		PreferredPort: 0,
		Scopes:        []string{"w_member_social", "r_profile_basicinfo"},
		AuthFlow:      "oauth2",
	})
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
	if got := parsed.Scheme + "://" + parsed.Host + parsed.Path; got != OAuth2AuthorizationURL {
		t.Fatalf("unexpected authorization endpoint: %q", got)
	}
	if got := parsed.Query().Get("scope"); got != "w_member_social r_profile_basicinfo" {
		t.Fatalf("unexpected scopes: %q", got)
	}
	if got := parsed.Query().Get("code_challenge"); got != "" {
		t.Fatalf("unexpected code_challenge: %q", got)
	}
	if got := parsed.Query().Get("code_challenge_method"); got != "" {
		t.Fatalf("unexpected code_challenge_method: %q", got)
	}
	if request.CodeVerifier != "" {
		t.Fatalf("expected no verifier for oauth2 flow, got %q", request.CodeVerifier)
	}
}

func TestWaitForOAuthCallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
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
	case <-ctx.Done():
		t.Fatalf("timed out waiting for callback result: %v", ctx.Err())
	}
}

func TestSnippetForError_RedactsAccessToken(t *testing.T) {
	body := []byte(`{"access_token":"t0ps3cr3t","error":"invalid_grant"}`)
	got := snippetForError(body)
	if strings.Contains(got, "t0ps3cr3t") {
		t.Fatalf("snippet leaked access token: %q", got)
	}
	if !strings.Contains(got, "invalid_grant") {
		t.Fatalf("snippet dropped error code: %q", got)
	}
}

func TestSnippetForError_RedactsClientSecret(t *testing.T) {
	body := []byte(`{"client_secret":"sh!","error":"invalid_grant"}`)
	got := snippetForError(body)
	if strings.Contains(got, "sh!") {
		t.Fatalf("snippet leaked client_secret: %q", got)
	}
	if !strings.Contains(got, "invalid_grant") {
		t.Fatalf("snippet dropped error code: %q", got)
	}
}

func TestSnippetForError_NonJSONBodyStillRedactsPII(t *testing.T) {
	body := []byte("error contacting urn:li:person:abc123 endpoint")
	got := snippetForError(body)
	if strings.Contains(got, "urn:li:person:abc123") {
		t.Fatalf("snippet leaked URN: %q", got)
	}
}

func TestSnippetForError_EmptyBody(t *testing.T) {
	if got := snippetForError(nil); got != "<empty body>" {
		t.Fatalf("empty body snippet = %q", got)
	}
}

func TestWaitForOAuthCallback_ContextCancelReturnsQuickly(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithCancel(t.Context())

	errorCh := make(chan error, 1)
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_, waitErr := WaitForOAuthCallback(ctx, listener, "state-123")
		errorCh <- waitErr
	}()

	// Give the server a moment to start, then cancel parent ctx.
	time.Sleep(20 * time.Millisecond)
	cancel()

	start := time.Now()
	select {
	case <-doneCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("WaitForOAuthCallback did not return within 500ms after ctx cancel; shutdown is using parent ctx")
	}
	elapsed := time.Since(start)

	waitErr := <-errorCh
	if waitErr == nil {
		t.Fatal("expected error after context cancel")
	}
	if !errors.Is(waitErr, context.Canceled) {
		t.Fatalf("expected error wrapping context.Canceled, got: %v", waitErr)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("returned too slowly after cancel: %s", elapsed)
	}
}

func TestWaitForOAuthCallback_StateMismatch(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, waitErr := WaitForOAuthCallback(ctx, listener, "state-123")
		errCh <- waitErr
	}()

	callbackURL := fmt.Sprintf("http://%s/callback?code=abc&state=wrong-state", listener.Addr().String())
	response, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("get callback: %v", err)
	}
	_ = response.Body.Close()

	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("expected state mismatch, got %v", err)
	}
}

func TestWaitForOAuthCallback_ErrorParam(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, waitErr := WaitForOAuthCallback(ctx, listener, "state-123")
		errCh <- waitErr
	}()

	callbackURL := fmt.Sprintf("http://%s/callback?state=state-123&error=access_denied&error_description=declined", listener.Addr().String())
	response, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("get callback: %v", err)
	}
	_ = response.Body.Close()

	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "authorization failed: declined") {
		t.Fatalf("expected authorization error, got %v", err)
	}
}

func TestWaitForOAuthCallback_WrongMethod(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, waitErr := WaitForOAuthCallback(ctx, listener, "state-123")
		errCh <- waitErr
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://%s/callback", listener.Addr().String()), http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post callback: %v", err)
	}
	_ = response.Body.Close()

	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "unexpected callback method") {
		t.Fatalf("expected method error, got %v", err)
	}
}

func TestWaitForOAuthCallback_HostMismatch(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, waitErr := WaitForOAuthCallback(ctx, listener, "state-123")
		errCh <- waitErr
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/callback?code=abc&state=state-123", listener.Addr().String()), http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "localhost"
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get callback: %v", err)
	}
	_ = response.Body.Close()

	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "unexpected callback host") {
		t.Fatalf("expected host validation error, got %v", err)
	}
}

func TestRefreshAccessToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("unexpected grant_type: %q", got)
		}
		if got := r.Form.Get("refresh_token"); got != "old-refresh" {
			t.Errorf("unexpected refresh_token: %q", got)
		}
		if got := r.Form.Get("client_id"); got != "client-abc" {
			t.Errorf("unexpected client_id: %q", got)
		}
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:           "new-access",
			ExpiresIn:             3600,
			Scope:                 "openid profile",
			RefreshToken:          "new-refresh",
			RefreshTokenExpiresIn: 31536000,
		})
	}))
	defer server.Close()

	token, err := RefreshAccessToken(t.Context(), server.Client(), server.URL, "client-abc", "old-refresh")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if token.AccessToken != "new-access" {
		t.Errorf("unexpected access token: %q", token.AccessToken)
	}
	if token.RefreshToken != "new-refresh" {
		t.Errorf("unexpected refresh token: %q", token.RefreshToken)
	}
	if token.RefreshTokenExpiresIn != 31536000 {
		t.Errorf("unexpected refresh_token_expires_in: %d", token.RefreshTokenExpiresIn)
	}
	if token.ExpiresIn != 3600 {
		t.Errorf("unexpected expires_in: %d", token.ExpiresIn)
	}
}

func TestRefreshAccessToken_InvalidGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid_grant","error_description":"token expired"}`)
	}))
	defer server.Close()

	_, err := RefreshAccessToken(t.Context(), server.Client(), server.URL, "client-abc", "expired-refresh")
	if err == nil {
		t.Fatal("expected error for 400 invalid_grant")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 in error, got: %v", err)
	}
}

func TestRefreshAccessToken_Server5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal server error")
	}))
	defer server.Close()

	_, err := RefreshAccessToken(t.Context(), server.Client(), server.URL, "client-abc", "some-refresh")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
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
			Scope:       "openid profile email w_member_social_feed",
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

	request, err := BuildLoginRequest(t.Context(), "client-123", 0, []string{"openid", "profile", "email"})
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}

	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	callbackDone := fireOAuthCallbackWhenReady(t, ctx, request)

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
	if !strings.Contains(strings.Join(session.Scopes, " "), "w_member_social_feed") {
		t.Fatalf("unexpected scopes: %#v", session.Scopes)
	}
	if err := <-callbackDone; err != nil {
		t.Fatalf("callback get: %v", err)
	}
}

func TestCompleteLoginOAuth2UsesClientSecret(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("client_secret"); got != "secret-123" {
			t.Fatalf("unexpected client_secret: %q", got)
		}
		if got := r.Form.Get("code_verifier"); got != "" {
			t.Fatalf("unexpected code verifier: %q", got)
		}

		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "token-123",
			ExpiresIn:   3600,
			Scope:       "w_member_social r_profile_basicinfo",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	mux.HandleFunc("/profile", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "profile123"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	request, err := BuildLoginRequestWithOptions(t.Context(), LoginRequestOptions{
		ClientID:      "client-123",
		PreferredPort: 0,
		Scopes:        []string{"w_member_social", "r_profile_basicinfo"},
		AuthFlow:      "oauth2",
	})
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	callbackDone := fireOAuthCallbackWhenReady(t, ctx, request)

	session, err := CompleteLogin(ctx, request, "default", "official", LoginFlowOptions{
		HTTPClient:      server.Client(),
		Interactive:     false,
		TokenURL:        server.URL + "/oauth/token",
		ClientSecret:    "secret-123",
		AuthFlow:        "oauth2",
		UserInfoURL:     server.URL + "/userinfo",
		ProfileURL:      server.URL + "/profile",
		RequestedScopes: []string{"w_member_social", "r_profile_basicinfo"},
	})
	if err != nil {
		t.Fatalf("complete login: %v", err)
	}
	if session.AuthFlow != "oauth2" {
		t.Fatalf("AuthFlow = %q", session.AuthFlow)
	}
	if session.MemberURN != "urn:li:person:profile123" {
		t.Fatalf("MemberURN = %q", session.MemberURN)
	}
	if err := <-callbackDone; err != nil {
		t.Fatalf("callback get: %v", err)
	}
}

func TestCompleteLogin_UsesManualMemberURNWhenUserInfoUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "token-123",
			ExpiresIn:   3600,
			Scope:       "w_member_social_feed",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	mux.HandleFunc("/profile", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	request, err := BuildLoginRequest(t.Context(), "client-123", 0, []string{"w_member_social_feed"})
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	callbackDone := fireOAuthCallbackWhenReady(t, ctx, request)

	session, err := CompleteLogin(ctx, request, "default", "official", LoginFlowOptions{
		HTTPClient:      server.Client(),
		Interactive:     false,
		TokenURL:        server.URL + "/oauth/token",
		UserInfoURL:     server.URL + "/userinfo",
		ProfileURL:      server.URL + "/profile",
		RequestedScopes: []string{"w_member_social_feed"},
		ManualMemberURN: "urn:li:person:manual123",
	})
	if err != nil {
		t.Fatalf("complete login: %v", err)
	}
	if session.MemberURN != "urn:li:person:manual123" {
		t.Fatalf("MemberURN = %q", session.MemberURN)
	}
	if session.ProfileID != "manual123" {
		t.Fatalf("ProfileID = %q", session.ProfileID)
	}
	if strings.Join(session.Scopes, " ") != "w_member_social_feed" {
		t.Fatalf("Scopes = %#v", session.Scopes)
	}
	if err := <-callbackDone; err != nil {
		t.Fatalf("callback get: %v", err)
	}
}

func TestCompleteLogin_UsesProfileIDWhenUserInfoUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "token-123",
			ExpiresIn:   3600,
			Scope:       "w_member_social r_profile_basicinfo",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	mux.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "profile123"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	request, err := BuildLoginRequest(t.Context(), "client-123", 0, []string{"w_member_social", "r_profile_basicinfo"})
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	callbackDone := fireOAuthCallbackWhenReady(t, ctx, request)

	session, err := CompleteLogin(ctx, request, "default", "official", LoginFlowOptions{
		HTTPClient:      server.Client(),
		Interactive:     false,
		TokenURL:        server.URL + "/oauth/token",
		UserInfoURL:     server.URL + "/userinfo",
		ProfileURL:      server.URL + "/profile",
		RequestedScopes: []string{"w_member_social", "r_profile_basicinfo"},
	})
	if err != nil {
		t.Fatalf("complete login: %v", err)
	}
	if session.MemberURN != "urn:li:person:profile123" {
		t.Fatalf("MemberURN = %q", session.MemberURN)
	}
	if session.ProfileID != "profile123" {
		t.Fatalf("ProfileID = %q", session.ProfileID)
	}
	if err := <-callbackDone; err != nil {
		t.Fatalf("callback get: %v", err)
	}
}

func fireOAuthCallbackWhenReady(t *testing.T, ctx context.Context, request *LoginRequest) <-chan error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		callbackURL := fmt.Sprintf("%s?code=callback-code&state=%s", request.RedirectURI, request.State)
		for {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, callbackURL, http.NoBody)
			if err != nil {
				done <- err
				return
			}
			response, err := http.DefaultClient.Do(req)
			if err == nil {
				_ = response.Body.Close()
				done <- nil
				return
			}

			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				done <- ctx.Err()
				return
			case <-timer.C:
			}
		}
	}()
	return done
}

func TestSplitScopesAllowsCommaSeparatedTokens(t *testing.T) {
	got := splitScopes("openid,profile email\tw_member_social_feed\n")
	want := []string{"openid", "profile", "email", "w_member_social_feed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitScopes = %#v, want %#v", got, want)
	}
}
