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
}

// NewKeyringStore constructs a keyring-backed session store.
func NewKeyringStore(service string) *KeyringStore {
	if service == "" {
		service = defaultServiceName
	}

	return &KeyringStore{service: service}
}

// LoadSession loads a session from the system keyring.
func (s *KeyringStore) LoadSession(_ context.Context, profile string) (*Session, error) {
	value, err := keyring.Get(s.service, sessionKey(profile))
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

	return &session, nil
}

// SaveSession stores a session in the system keyring.
func (s *KeyringStore) SaveSession(_ context.Context, session Session) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode keyring session: %w", err)
	}

	if err := keyring.Set(s.service, sessionKey(session.Profile), string(payload)); err != nil {
		return fmt.Errorf("save keyring session: %w", err)
	}

	return nil
}

// DeleteSession removes a session from the system keyring.
func (s *KeyringStore) DeleteSession(_ context.Context, profile string) error {
	if err := keyring.Delete(s.service, sessionKey(profile)); err != nil {
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
