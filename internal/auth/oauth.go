package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	// AuthorizationURL is LinkedIn's native-PKCE authorization endpoint used
	// for the public-client flow — authorization + token exchange without a
	// client_secret. LinkedIn documents this native-client flow separately
	// from the confidential-client OAuth pages and requires the default
	// system browser plus a loopback redirect listener.
	AuthorizationURL = "https://www.linkedin.com/oauth/native-pkce/authorization"
	// OAuth2AuthorizationURL is LinkedIn's standard confidential-client OAuth
	// authorization endpoint.
	OAuth2AuthorizationURL = "https://www.linkedin.com/oauth/v2/authorization"
	// TokenURL is the LinkedIn OAuth token endpoint.
	TokenURL = "https://www.linkedin.com/oauth/v2/accessToken"
	// UserInfoURL is the LinkedIn OpenID Connect userinfo endpoint.
	UserInfoURL = "https://api.linkedin.com/v2/userinfo"
	// ProfileURL is the legacy/current-member profile endpoint used as a
	// non-OIDC fallback when native PKCE rejects OpenID scopes.
	ProfileURL          = "https://api.linkedin.com/v2/me?fields=id"
	defaultCallbackHost = "127.0.0.1"
	defaultCallbackPath = "/callback"
	// maxOAuthBodyBytes caps the size of OAuth/userinfo response bodies. Token
	// JSON and userinfo payloads are kilobytes; a malicious or compromised IdP
	// streaming gigabytes would otherwise exhaust process memory.
	maxOAuthBodyBytes = 64 << 10
)

// BrowserOpener launches the system browser for an authorization URL.
type BrowserOpener func(ctx context.Context, targetURL string) error

// LoginRequest contains the generated PKCE login request values.
type LoginRequest struct {
	URL              string
	RedirectURI      string
	State            string
	CodeVerifier     string
	CallbackListener net.Listener
}

// CallbackResult contains the captured authorization callback values.
type CallbackResult struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

// TokenResponse contains the OAuth token response fields used by golink.
type TokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	Scope                 string `json:"scope"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in,omitempty"`
}

// UserInfo contains the userinfo fields consumed by golink.
type UserInfo struct {
	Sub     string `json:"sub"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Picture string `json:"picture"`
	Locale  struct {
		Country  string `json:"country"`
		Language string `json:"language"`
	} `json:"locale"`
}

// LoginFlowOptions controls the full login flow behavior.
type LoginFlowOptions struct {
	HTTPClient      *http.Client
	BrowserOpener   BrowserOpener
	Interactive     bool
	Now             func() time.Time
	TokenURL        string
	ClientSecret    string
	AuthFlow        string
	UserInfoURL     string
	ProfileURL      string
	RequestedScopes []string
	ManualMemberURN string
}

// BuildLoginRequest constructs the initial PKCE authorization request.
func BuildLoginRequest(ctx context.Context, clientID string, preferredPort int, scopes []string) (*LoginRequest, error) {
	return BuildLoginRequestWithOptions(ctx, LoginRequestOptions{
		ClientID:      clientID,
		PreferredPort: preferredPort,
		Scopes:        scopes,
		AuthFlow:      "pkce",
	})
}

// LoginRequestOptions controls authorization URL generation.
type LoginRequestOptions struct {
	ClientID      string
	PreferredPort int
	Scopes        []string
	AuthFlow      string
}

// BuildLoginRequestWithOptions constructs the initial OAuth authorization request.
func BuildLoginRequestWithOptions(ctx context.Context, opts LoginRequestOptions) (*LoginRequest, error) {
	redirectReservation, err := resolveRedirectURI(ctx, opts.PreferredPort)
	if err != nil {
		return nil, err
	}

	state, err := randomURLSafeString(32)
	if err != nil {
		return nil, fmt.Errorf("generate oauth state: %w", err)
	}

	verifier, err := randomURLSafeString(64)
	if err != nil {
		return nil, fmt.Errorf("generate code verifier: %w", err)
	}

	authFlow := normalizeAuthFlow(opts.AuthFlow)
	requestURL, err := buildAuthorizationURL(opts.ClientID, redirectReservation.URL.String(), opts.Scopes, state, verifier, authFlow)
	if err != nil {
		_ = redirectReservation.Listener.Close()
		return nil, err
	}

	return &LoginRequest{
		URL:              requestURL,
		RedirectURI:      redirectReservation.URL.String(),
		State:            state,
		CodeVerifier:     codeVerifierForFlow(verifier, authFlow),
		CallbackListener: redirectReservation.Listener,
	}, nil
}

// CompleteLogin runs the PKCE callback wait, token exchange, and profile lookup flow.
func CompleteLogin(
	ctx context.Context,
	request *LoginRequest,
	profile string,
	transport string,
	options LoginFlowOptions,
) (*Session, error) {
	if request == nil {
		return nil, fmt.Errorf("complete login: request is nil")
	}
	if request.CallbackListener == nil {
		return nil, fmt.Errorf("complete login: callback listener is nil")
	}
	defer func() { _ = request.CallbackListener.Close() }()

	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	tokenURL := options.TokenURL
	if tokenURL == "" {
		tokenURL = TokenURL
	}
	authFlow := normalizeAuthFlow(options.AuthFlow)

	userInfoURL := options.UserInfoURL
	if userInfoURL == "" {
		userInfoURL = UserInfoURL
	}
	profileURL := options.ProfileURL
	if profileURL == "" {
		profileURL = ProfileURL
	}

	if options.Interactive && options.BrowserOpener != nil {
		if err := options.BrowserOpener(ctx, request.URL); err != nil {
			return nil, fmt.Errorf("complete login: open browser: %w", err)
		}
	}

	callback, err := WaitForOAuthCallback(ctx, request.CallbackListener, request.State)
	if err != nil {
		return nil, fmt.Errorf("complete login: wait for callback: %w", err)
	}

	token, err := ExchangeAuthorizationCode(ctx, httpClient, tokenURL, request, callback, options.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("complete login: exchange authorization code: %w", err)
	}

	userInfo, err := FetchUserInfo(ctx, httpClient, userInfoURL, token.AccessToken)
	if err != nil {
		profileID, profileErr := FetchProfileID(ctx, httpClient, profileURL, token.AccessToken)
		if profileErr != nil {
			manualMemberURN := strings.TrimSpace(options.ManualMemberURN)
			if manualMemberURN == "" {
				return nil, fmt.Errorf("complete login: fetch user info: %w; fetch profile id: %v", err, profileErr)
			}
			userInfo = &UserInfo{Sub: manualMemberURN}
		} else {
			userInfo = &UserInfo{Sub: memberURNFromProfileID(profileID)}
		}
	}

	session := &Session{
		Profile:        profile,
		Transport:      transport,
		AccessToken:    token.AccessToken,
		Scopes:         splitScopes(token.Scope),
		ConnectedAt:    now().UTC(),
		AuthFlow:       authFlow,
		MemberURN:      userInfo.Sub,
		ProfileID:      strings.TrimPrefix(userInfo.Sub, "urn:li:person:"),
		Name:           userInfo.Name,
		Email:          userInfo.Email,
		Picture:        userInfo.Picture,
		LocaleCountry:  userInfo.Locale.Country,
		LocaleLanguage: userInfo.Locale.Language,
	}
	if token.ExpiresIn > 0 {
		session.ExpiresAt = now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	if token.RefreshToken != "" {
		session.RefreshToken = token.RefreshToken
		if token.RefreshTokenExpiresIn > 0 {
			session.RefreshExpiresAt = now().UTC().Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second)
		}
	}
	if len(session.Scopes) == 0 {
		session.Scopes = cleanScopes(options.RequestedScopes)
	}

	if err := session.Validate(); err != nil {
		return nil, fmt.Errorf("complete login: validate session: %w", err)
	}

	return session, nil
}

func buildAuthorizationURL(clientID, redirectURI string, scopes []string, state, verifier, authFlow string) (string, error) {
	authURL := AuthorizationURL
	if normalizeAuthFlow(authFlow) == "oauth2" {
		authURL = OAuth2AuthorizationURL
	}
	base, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("parse authorization url: %w", err)
	}

	base.Path = path.Clean(base.Path)
	query := base.Query()
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", strings.Join(scopes, " "))
	query.Set("state", state)
	if normalizeAuthFlow(authFlow) == "pkce" {
		query.Set("code_challenge", codeChallengeS256(verifier))
		query.Set("code_challenge_method", "S256")
	}
	base.RawQuery = query.Encode()

	return base.String(), nil
}

// WaitForOAuthCallback waits for the loopback redirect with the expected state.
func WaitForOAuthCallback(ctx context.Context, listener net.Listener, expectedState string) (*CallbackResult, error) {
	if listener == nil {
		return nil, fmt.Errorf("wait for oauth callback: listener is nil")
	}

	resultCh := make(chan CallbackResult, 1)
	errorCh := make(chan error, 1)
	serveErrCh := make(chan error, 1)
	doneCh := make(chan struct{})
	expectedHost := listener.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc(defaultCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != expectedHost {
			http.Error(w, "invalid host", http.StatusBadRequest)
			select {
			case errorCh <- fmt.Errorf("unexpected callback host: %s", r.Host):
			default:
			}
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			select {
			case errorCh <- fmt.Errorf("unexpected callback method: %s", r.Method):
			default:
			}
			return
		}

		query := r.URL.Query()
		result := CallbackResult{
			Code:             query.Get("code"),
			State:            query.Get("state"),
			Error:            query.Get("error"),
			ErrorDescription: query.Get("error_description"),
		}

		switch {
		case subtle.ConstantTimeCompare([]byte(result.State), []byte(expectedState)) != 1:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			select {
			case errorCh <- fmt.Errorf("state mismatch"):
			default:
			}
		case result.Error != "":
			http.Error(w, "authorization failed", http.StatusBadRequest)
			select {
			case errorCh <- fmt.Errorf("authorization failed: %s", result.ErrorDescription):
			default:
			}
		case result.Code == "":
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			select {
			case errorCh <- fmt.Errorf("authorization callback missing code"):
			default:
			}
		default:
			_, _ = io.WriteString(w, "golink authentication received. You can return to the terminal.")
			select {
			case resultCh <- result:
			default:
			}
		}
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	go func() {
		defer close(doneCh)
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-doneCh
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errorCh:
		return nil, err
	case result := <-resultCh:
		return &result, nil
	case err := <-serveErrCh:
		return nil, fmt.Errorf("serve oauth callback: %w", err)
	}
}

// ExchangeAuthorizationCode exchanges the captured authorization code for a bearer token.
func ExchangeAuthorizationCode(
	ctx context.Context,
	httpClient *http.Client,
	tokenURL string,
	request *LoginRequest,
	callback *CallbackResult,
	clientSecret string,
) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", callback.Code)
	form.Set("client_id", requestClientID(request.URL))
	form.Set("redirect_uri", request.RedirectURI)
	if request.CodeVerifier != "" {
		form.Set("code_verifier", request.CodeVerifier)
	}
	if strings.TrimSpace(clientSecret) != "" {
		form.Set("client_secret", clientSecret)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	body, err := readCappedBody(response.Body)
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint returned %d: %s", response.StatusCode, snippetForError(body))
	}

	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	return &token, nil
}

// RefreshAccessToken exchanges a refresh token for a new access token.
// Returns the full token response so the caller can persist a rotated refresh
// token if one is returned. A 4xx response (e.g. invalid_grant) is returned as
// a non-nil error with the HTTP status embedded; a 5xx or network error is also
// returned as a non-nil error.
func RefreshAccessToken(ctx context.Context, httpClient *http.Client, tokenURL, clientID, refreshToken string) (*TokenResponse, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	body, err := readCappedBody(response.Body)
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint returned %d: %s", response.StatusCode, snippetForError(body))
	}

	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	return &token, nil
}

// FetchUserInfo fetches the authenticated member profile via OIDC userinfo.
func FetchUserInfo(ctx context.Context, httpClient *http.Client, userInfoURL, accessToken string) (*UserInfo, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	body, err := readCappedBody(response.Body)
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("userinfo endpoint returned %d: %s", response.StatusCode, snippetForError(body))
	}

	var userInfo UserInfo
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, err
	}
	if strings.TrimSpace(userInfo.Sub) == "" {
		return nil, fmt.Errorf("userinfo response missing sub")
	}

	return &userInfo, nil
}

// FetchProfileID fetches the current member's profile ID from LinkedIn's
// non-OIDC profile endpoint. It is used when native PKCE scopes cannot include
// openid/profile/email but the app has a profile-read permission.
func FetchProfileID(ctx context.Context, httpClient *http.Client, profileURL, accessToken string) (string, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, http.NoBody)
	if err != nil {
		return "", err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
	httpRequest.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()

	body, err := readCappedBody(response.Body)
	if err != nil {
		return "", err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("profile endpoint returned %d: %s", response.StatusCode, snippetForError(body))
	}

	var profile struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &profile); err != nil {
		return "", err
	}
	if strings.TrimSpace(profile.ID) == "" {
		return "", fmt.Errorf("profile response missing id")
	}

	return profile.ID, nil
}

type redirectReservation struct {
	URL      *url.URL
	Listener net.Listener
}

func resolveRedirectURI(ctx context.Context, preferredPort int) (*redirectReservation, error) {
	addr := net.JoinHostPort(defaultCallbackHost, strconv.Itoa(preferredPort))
	if preferredPort == 0 {
		addr = net.JoinHostPort(defaultCallbackHost, "0")
	}

	listenerConfig := net.ListenConfig{}
	listener, err := listenerConfig.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("reserve loopback redirect port: %w", err)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, fmt.Errorf("unexpected redirect listener address: %T", listener.Addr())
	}

	return &redirectReservation{
		URL: &url.URL{
			Scheme: "http",
			Host:   net.JoinHostPort(defaultCallbackHost, strconv.Itoa(tcpAddr.Port)),
			Path:   defaultCallbackPath,
		},
		Listener: listener,
	}, nil
}

func randomURLSafeString(rawBytes int) (string, error) {
	buf := make([]byte, rawBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func normalizeAuthFlow(raw string) string {
	if strings.TrimSpace(raw) == "oauth2" {
		return "oauth2"
	}

	return "pkce"
}

func codeVerifierForFlow(verifier, authFlow string) string {
	if normalizeAuthFlow(authFlow) == "oauth2" {
		return ""
	}

	return verifier
}

func splitScopes(raw string) []string {
	return cleanScopes(strings.Fields(raw))
}

func cleanScopes(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}

	cleaned := make([]string, 0, len(fields))
	for _, field := range fields {
		if scope := strings.TrimSpace(field); scope != "" {
			cleaned = append(cleaned, scope)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func memberURNFromProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if strings.HasPrefix(profileID, "urn:li:") {
		return profileID
	}
	return "urn:li:person:" + profileID
}

func requestClientID(requestURL string) string {
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return ""
	}

	return parsed.Query().Get("client_id")
}

// readCappedBody reads at most maxOAuthBodyBytes from r. Returns an error when
// the cap is exceeded so a malicious upstream cannot exhaust process memory.
func readCappedBody(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, maxOAuthBodyBytes+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxOAuthBodyBytes {
		return nil, fmt.Errorf("oauth response body exceeds %d bytes", maxOAuthBodyBytes)
	}
	return buf, nil
}

// snippetForError returns a short, safe representation of body for inclusion
// in an error message. Long bodies are truncated; whitespace is collapsed.
func snippetForError(body []byte) string {
	const maxLen = 256
	s := strings.TrimSpace(string(body))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	if s == "" {
		return "<empty body>"
	}
	return s
}
