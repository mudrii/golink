package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const defaultServiceName = "github.com.mudrii.golink"

// KeyringStore stores sessions in the OS keyring.
type KeyringStore struct {
	service string
	backend keyringBackend
}

// NewKeyringStore constructs a keyring-backed session store.
func NewKeyringStore(service string) *KeyringStore {
	return newKeyringStore(service, systemKeyring{})
}

type keyringBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (systemKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

func newKeyringStore(service string, backend keyringBackend) *KeyringStore {
	if service == "" {
		service = defaultServiceName
	}
	if backend == nil {
		backend = systemKeyring{}
	}

	return &KeyringStore{service: service, backend: backend}
}

// LoadSession loads a session from the system keyring.
func (s *KeyringStore) LoadSession(_ context.Context, profile string) (*Session, error) {
	value, err := s.backend.Get(s.service, sessionKey(profile))
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, ErrSessionNotFound
		}

		return nil, fmt.Errorf("load keyring session: %w", err)
	}

	var session Session
	if err := json.Unmarshal([]byte(value), &session); err != nil {
		return nil, fmt.Errorf("decode keyring session: %w", err)
	}
	if err := session.Validate(); err != nil {
		return nil, fmt.Errorf("validate keyring session: %w", err)
	}

	return &session, nil
}

// SaveSession stores a session in the system keyring.
func (s *KeyringStore) SaveSession(_ context.Context, session Session) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode keyring session: %w", err)
	}

	if err := s.backend.Set(s.service, sessionKey(session.Profile), string(payload)); err != nil {
		return fmt.Errorf("save keyring session: %w", err)
	}

	return nil
}

// DeleteSession removes a session from the system keyring.
func (s *KeyringStore) DeleteSession(_ context.Context, profile string) error {
	if err := s.backend.Delete(s.service, sessionKey(profile)); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return ErrSessionNotFound
		}

		return fmt.Errorf("delete keyring session: %w", err)
	}

	return nil
}

func sessionKey(profile string) string {
	return "profile:" + profile
}
