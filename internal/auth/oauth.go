package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	// AuthorizationURL is the LinkedIn native PKCE authorization endpoint.
	AuthorizationURL = "https://www.linkedin.com/oauth/native-pkce/authorization"
	// TokenURL is the LinkedIn OAuth token endpoint.
	TokenURL = "https://www.linkedin.com/oauth/v2/accessToken"
	// UserInfoURL is the LinkedIn OpenID Connect userinfo endpoint.
	UserInfoURL         = "https://api.linkedin.com/v2/userinfo"
	defaultCallbackHost = "127.0.0.1"
	defaultCallbackPath = "/callback"
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
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
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
	HTTPClient    *http.Client
	BrowserOpener BrowserOpener
	Interactive   bool
	Now           func() time.Time
	TokenURL      string
	UserInfoURL   string
}

// BuildLoginRequest constructs the initial PKCE authorization request.
func BuildLoginRequest(ctx context.Context, clientID string, preferredPort int, scopes []string) (*LoginRequest, error) {
	redirectReservation, err := resolveRedirectURI(ctx, preferredPort)
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

	requestURL, err := buildAuthorizationURL(clientID, redirectReservation.URL.String(), scopes, state, verifier)
	if err != nil {
		_ = redirectReservation.Listener.Close()
		return nil, err
	}

	return &LoginRequest{
		URL:              requestURL,
		RedirectURI:      redirectReservation.URL.String(),
		State:            state,
		CodeVerifier:     verifier,
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

	userInfoURL := options.UserInfoURL
	if userInfoURL == "" {
		userInfoURL = UserInfoURL
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

	token, err := ExchangeAuthorizationCode(ctx, httpClient, tokenURL, request, callback)
	if err != nil {
		return nil, fmt.Errorf("complete login: exchange authorization code: %w", err)
	}

	userInfo, err := FetchUserInfo(ctx, httpClient, userInfoURL, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("complete login: fetch user info: %w", err)
	}

	session := &Session{
		Profile:        profile,
		Transport:      transport,
		AccessToken:    token.AccessToken,
		Scopes:         splitScopes(token.Scope),
		ConnectedAt:    now().UTC(),
		AuthFlow:       "pkce",
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
	if len(session.Scopes) == 0 {
		session.Scopes = []string{"openid", "profile", "email", "w_member_social"}
	}

	if err := session.Validate(); err != nil {
		return nil, fmt.Errorf("complete login: validate session: %w", err)
	}

	return session, nil
}

func buildAuthorizationURL(clientID, redirectURI string, scopes []string, state, verifier string) (string, error) {
	base, err := url.Parse(AuthorizationURL)
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
	query.Set("code_challenge", codeChallengeS256(verifier))
	query.Set("code_challenge_method", "S256")
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

	mux := http.NewServeMux()
	mux.HandleFunc(defaultCallbackPath, func(w http.ResponseWriter, r *http.Request) {
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
		case result.State != expectedState:
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

	server := &http.Server{Handler: mux}
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
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
) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", callback.Code)
	form.Set("client_id", requestClientID(request.URL))
	form.Set("redirect_uri", request.RedirectURI)
	form.Set("code_verifier", request.CodeVerifier)

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

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
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

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("userinfo endpoint returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
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

func splitScopes(raw string) []string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}

	return fields
}

func requestClientID(requestURL string) string {
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return ""
	}

	return parsed.Query().Get("client_id")
}
