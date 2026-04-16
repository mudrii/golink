package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	defaultProfile      = "default"
	defaultTransport    = "official"
	defaultTimeout      = 30 * time.Second
	maximumTimeout      = 5 * time.Minute
	defaultVisibility   = "PUBLIC"
	defaultConfigName   = "config"
	defaultConfigType   = "yaml"
	defaultConfigSubdir = "golink"
)

var validTransports = map[string]struct{}{
	"official":   {},
	"unofficial": {},
	"auto":       {},
}

// Settings contains merged runtime configuration.
type Settings struct {
	JSON                 bool
	DryRun               bool
	Verbose              bool
	Profile              string
	Transport            string
	AcceptUnofficialRisk bool
	Timeout              time.Duration
	ClientID             string
	APIVersion           string
	RedirectPort         int
	DefaultVisibility    string
}

// Loader resolves settings from flags, environment variables, and config files.
type Loader struct {
	v *viper.Viper
}

// NewLoader constructs a config loader with golink defaults and search paths.
func NewLoader() *Loader {
	v := viper.New()
	v.SetEnvPrefix("golink")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	v.SetConfigName(defaultConfigName)
	v.SetConfigType(defaultConfigType)
	v.SetDefault("profile", defaultProfile)
	v.SetDefault("transport", defaultTransport)
	v.SetDefault("timeout", defaultTimeout)
	v.SetDefault("default_visibility", defaultVisibility)

	if configDir, err := os.UserConfigDir(); err == nil {
		v.AddConfigPath(filepath.Join(configDir, defaultConfigSubdir))
	}

	return &Loader{v: v}
}

// BindFlags binds a Cobra flag set into the loader's precedence chain.
func (l *Loader) BindFlags(flags *pflag.FlagSet) error {
	if err := l.v.BindPFlags(flags); err != nil {
		return fmt.Errorf("bind persistent flags: %w", err)
	}

	return nil
}

// Load resolves and validates the merged settings.
func (l *Loader) Load() (Settings, error) {
	if err := l.readConfig(); err != nil {
		return Settings{}, err
	}

	settings := Settings{
		JSON:                 l.v.GetBool("json"),
		DryRun:               l.v.GetBool("dry-run"),
		Verbose:              l.v.GetBool("verbose"),
		Profile:              l.v.GetString("profile"),
		Transport:            l.v.GetString("transport"),
		AcceptUnofficialRisk: l.v.GetBool("accept-unofficial-risk"),
		Timeout:              l.v.GetDuration("timeout"),
		ClientID:             l.v.GetString("client_id"),
		APIVersion:           l.v.GetString("api_version"),
		RedirectPort:         l.v.GetInt("redirect_port"),
		DefaultVisibility:    l.v.GetString("default_visibility"),
	}

	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}

	return settings, nil
}

// Validate checks the caller-visible configuration invariants.
func (s Settings) Validate() error {
	if s.Profile == "" {
		return errors.New("profile must not be empty")
	}

	if _, ok := validTransports[s.Transport]; !ok {
		return fmt.Errorf("transport must be one of official|unofficial|auto")
	}

	if s.Timeout <= 0 {
		return errors.New("timeout must be greater than zero")
	}

	if s.Timeout > maximumTimeout {
		return fmt.Errorf("timeout must be %s or less", maximumTimeout)
	}

	return nil
}

func (l *Loader) readConfig() error {
	if err := l.v.ReadInConfig(); err != nil {
		var configNotFound viper.ConfigFileNotFoundError
		if errors.As(err, &configNotFound) {
			return nil
		}

		return fmt.Errorf("read config: %w", err)
	}

	return nil
}
