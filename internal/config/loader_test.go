package config

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestLoaderLoadPrecedence(t *testing.T) {
	t.Setenv("GOLINK_PROFILE", "env-profile")
	t.Setenv("GOLINK_TRANSPORT", "unofficial")
	t.Setenv("GOLINK_TIMEOUT", "45s")
	t.Setenv("GOLINK_CLIENT_ID", "env-client")

	loader := NewLoader()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Bool("json", false, "")
	flags.Bool("dry-run", false, "")
	flags.Bool("verbose", false, "")
	flags.String("profile", "", "")
	flags.String("transport", "", "")
	flags.Bool("accept-unofficial-risk", false, "")
	flags.Duration("timeout", 0, "")
	flags.String("client_id", "", "")
	flags.String("api_version", "", "")
	flags.Int("redirect_port", 0, "")
	flags.String("default_visibility", "", "")

	if err := flags.Set("profile", "flag-profile"); err != nil {
		t.Fatalf("set profile flag: %v", err)
	}
	if err := flags.Set("transport", "official"); err != nil {
		t.Fatalf("set transport flag: %v", err)
	}
	if err := flags.Set("timeout", "30s"); err != nil {
		t.Fatalf("set timeout flag: %v", err)
	}

	if err := loader.BindFlags(flags); err != nil {
		t.Fatalf("bind flags: %v", err)
	}

	settings, err := loader.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	if settings.Profile != "flag-profile" {
		t.Fatalf("expected flag profile, got %q", settings.Profile)
	}
	if settings.Transport != "official" {
		t.Fatalf("expected flag transport, got %q", settings.Transport)
	}
	if settings.Timeout != 30*time.Second {
		t.Fatalf("expected flag timeout, got %s", settings.Timeout)
	}
	if settings.ClientID != "env-client" {
		t.Fatalf("expected env client id, got %q", settings.ClientID)
	}
}
