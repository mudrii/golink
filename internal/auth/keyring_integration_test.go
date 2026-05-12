//go:build keyring_integration

package auth

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSystemKeyringIntegration(t *testing.T) {
	backend := systemKeyring{}
	service := "github.com.mudrii.golink.integration-test"
	user := "profile:test"
	value := "secret"

	if err := backend.Set(service, user, value); err != nil {
		t.Skipf("system keyring unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = backend.Delete(service, user)
	})

	got, err := backend.Get(service, user)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != value {
		t.Fatalf("Get = %q, want %q", got, value)
	}
	if err := backend.Delete(service, user); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := backend.Get(service, user); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("Get after delete err = %v, want ErrNotFound", err)
	}
}
