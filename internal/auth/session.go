// Package auth manages golink authentication state, including the native PKCE
// OAuth flow and profile-keyed session persistence.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// DefaultProfile is the fallback profile name used by the CLI.
const DefaultProfile = "default"

// ErrSessionNotFound is returned when no session exists for a profile.
var ErrSessionNotFound = errors.New("session not found")

// Session stores the authenticated state for a single profile.
type Session struct {
	Profile        string    `json:"profile"`
	Transport      string    `json:"transport"`
	AccessToken    string    `json:"access_token,omitempty"`
	Scopes         []string  `json:"scopes,omitempty"`
	ExpiresAt      time.Time `json:"expires_at,omitzero"`
	ConnectedAt    time.Time `json:"connected_at,omitzero"`
	AuthFlow       string    `json:"auth_flow,omitempty"`
	MemberURN      string    `json:"member_urn,omitempty"`
	ProfileID      string    `json:"profile_id,omitempty"`
	Name           string    `json:"name,omitempty"`
	Email          string    `json:"email,omitempty"`
	Picture        string    `json:"picture,omitempty"`
	LocaleCountry  string    `json:"locale_country,omitempty"`
	LocaleLanguage string    `json:"locale_language,omitempty"`
}

// Store persists and resolves profile sessions.
type Store interface {
	LoadSession(ctx context.Context, profile string) (*Session, error)
	SaveSession(ctx context.Context, session Session) error
	DeleteSession(ctx context.Context, profile string) error
}

// Validate checks whether the stored session has the minimum valid shape.
// A valid session may still be unauthenticated — use IsAuthenticated for that.
func (s Session) Validate() error {
	if strings.TrimSpace(s.Profile) == "" {
		return fmt.Errorf("stored session is missing profile")
	}

	if err := ValidateTransport(s.Transport); err != nil {
		return fmt.Errorf("stored session transport is invalid: %w", err)
	}

	return nil
}

// IsAuthenticated reports whether the session holds a usable bearer token.
// Returns false (with nil error) if the structural Validate passes but the
// session has no token or has expired.
func (s Session) IsAuthenticated(now time.Time) (bool, error) {
	if err := s.Validate(); err != nil {
		return false, err
	}
	if strings.TrimSpace(s.AccessToken) == "" {
		return false, nil
	}
	if !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt) {
		return false, nil
	}

	return true, nil
}

// ValidateTransport checks whether the session transport value is supported.
func ValidateTransport(transport string) error {
	switch strings.TrimSpace(transport) {
	case "official", "unofficial", "auto":
		return nil
	default:
		return fmt.Errorf("transport must be one of official|unofficial|auto")
	}
}

// MemoryStore is an in-memory session store used by tests.
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]Session
}

// NewMemoryStore constructs an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]Session),
	}
}

// LoadSession returns the stored session for the requested profile.
func (s *MemoryStore) LoadSession(_ context.Context, profile string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[profile]
	if !ok {
		return nil, ErrSessionNotFound
	}

	copySession := session
	return &copySession, nil
}

// SaveSession stores the provided session by profile.
func (s *MemoryStore) SaveSession(_ context.Context, session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.Profile] = session
	return nil
}

// DeleteSession removes the session for the requested profile.
func (s *MemoryStore) DeleteSession(_ context.Context, profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[profile]; !ok {
		return ErrSessionNotFound
	}

	delete(s.sessions, profile)
	return nil
}
