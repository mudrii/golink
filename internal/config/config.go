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

var validOutputModes = map[string]struct{}{
	"text":    {},
	"json":    {},
	"jsonl":   {},
	"compact": {},
	"table":   {},
}

// Settings contains merged runtime configuration.
type Settings struct {
	JSON                 bool
	DryRun               bool
	RequireApproval      bool
	Verbose              bool
	Profile              string
	Transport            string
	AcceptUnofficialRisk bool
	Timeout              time.Duration
	ClientID             string
	APIVersion           string
	RedirectPort         int
	DefaultVisibility    string
	// Output is the resolved output mode: text|json|jsonl|compact|table.
	// Normalized from --output, --compact, and --json flags during Load().
	Output string
	// Audit controls whether mutating commands append to the audit log.
	// Default true. Set via GOLINK_AUDIT env (on/off/true/false/1/0/yes/no)
	// or `audit: false` in the config file.
	Audit bool
	// AuditPath is the audit log file path. Empty means use audit.ResolvePath().
	// Override via GOLINK_AUDIT_PATH env or `audit_path` config key.
	AuditPath string
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
	v.SetDefault("audit", true)

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

	compact := l.v.GetBool("compact")
	outputFlag := strings.TrimSpace(l.v.GetString("output"))
	jsonFlag := l.v.GetBool("json")

	// Resolve output mode: --compact > --output > --json > text default.
	// Reject --compact combined with an explicit non-compact --output.
	var resolvedOutput string
	switch {
	case compact:
		if outputFlag != "" && outputFlag != "compact" {
			return Settings{}, fmt.Errorf("--compact and --output=%s are mutually exclusive", outputFlag)
		}
		resolvedOutput = "compact"
	case outputFlag != "":
		resolvedOutput = outputFlag
	case jsonFlag:
		resolvedOutput = "json"
	default:
		resolvedOutput = "text"
	}

	auditEnabled, err := resolveAuditEnabled(l.v.GetString("audit"))
	if err != nil {
		return Settings{}, err
	}

	settings := Settings{
		JSON:                 resolvedOutput == "json",
		DryRun:               l.v.GetBool("dry-run"),
		RequireApproval:      l.v.GetBool("require-approval"),
		Verbose:              l.v.GetBool("verbose"),
		Profile:              l.v.GetString("profile"),
		Transport:            l.v.GetString("transport"),
		AcceptUnofficialRisk: l.v.GetBool("accept-unofficial-risk"),
		Timeout:              l.v.GetDuration("timeout"),
		ClientID:             l.v.GetString("client_id"),
		APIVersion:           l.v.GetString("api_version"),
		RedirectPort:         l.v.GetInt("redirect_port"),
		DefaultVisibility:    l.v.GetString("default_visibility"),
		Output:               resolvedOutput,
		Audit:                auditEnabled,
		AuditPath:            l.v.GetString("audit_path"),
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

	if s.Output != "" {
		if _, ok := validOutputModes[s.Output]; !ok {
			return fmt.Errorf("output must be one of text|json|jsonl|compact|table")
		}
	}

	return nil
}

// resolveAuditEnabled converts the raw string value of the audit setting to a
// bool. Accepts on/true/1/yes (→ true) and off/false/0/no (→ false). Empty
// string maps to true (the default). Any other value is a validation error.
func resolveAuditEnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("audit must be one of on|off|true|false|1|0|yes|no, got %q", raw)
	}
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
