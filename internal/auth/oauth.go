package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const (
	// AuthorizationURL is the LinkedIn native PKCE authorization endpoint.
	AuthorizationURL    = "https://www.linkedin.com/oauth/native-pkce/authorization"
	defaultCallbackHost = "127.0.0.1"
	defaultCallbackPath = "/callback"
)

// LoginRequest contains the generated PKCE login request values.
type LoginRequest struct {
	URL          string
	RedirectURI  string
	State        string
	CodeVerifier string
}

// BuildLoginRequest constructs the initial PKCE authorization request.
func BuildLoginRequest(ctx context.Context, clientID string, preferredPort int, scopes []string) (*LoginRequest, error) {
	redirectURI, err := resolveRedirectURI(ctx, preferredPort)
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

	requestURL, err := buildAuthorizationURL(clientID, redirectURI, scopes, state, verifier)
	if err != nil {
		return nil, err
	}

	return &LoginRequest{
		URL:          requestURL,
		RedirectURI:  redirectURI,
		State:        state,
		CodeVerifier: verifier,
	}, nil
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

func resolveRedirectURI(ctx context.Context, preferredPort int) (string, error) {
	addr := net.JoinHostPort(defaultCallbackHost, strconv.Itoa(preferredPort))
	if preferredPort == 0 {
		addr = net.JoinHostPort(defaultCallbackHost, "0")
	}

	listenerConfig := net.ListenConfig{}
	listener, err := listenerConfig.Listen(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("reserve loopback redirect port: %w", err)
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected redirect listener address: %T", listener.Addr())
	}

	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(defaultCallbackHost, strconv.Itoa(tcpAddr.Port)),
		Path:   defaultCallbackPath,
	}).String(), nil
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
