package auth

import (
	"context"
	"errors"
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
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
	ConnectedAt    time.Time `json:"connected_at,omitempty"`
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
