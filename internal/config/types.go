// Package config handles configuration loading and management.
package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LogLevel represents the severity level for logging.
type LogLevel int

const (
	// LevelDebug is the lowest level, most verbose logging.
	LevelDebug LogLevel = iota
	// LevelInfo is the default level for general information.
	LevelInfo
	// LevelWarn is for warning messages.
	LevelWarn
	// LevelError is for error messages only, least verbose.
	LevelError
)

// Config represents the complete configuration.
type Config struct {
	Server    ServerConfig              `json:"server"`
	Providers map[string]ProviderConfig `json:"providers"`
	Router    RouterConfig              `json:"router"`
	Logging   LoggingConfig             `json:"logging,omitempty"`
	// MultiUser is deprecated — multi-user data is now stored in SQLite (usage.db).
	// Kept for backward compatibility when loading old config files.
	// On first load, settings are migrated to SQLite and this field is cleared.
	MultiUser MultiUserConfig           `json:"multiUser,omitempty"`
	// Profiles is kept at Config level for backward compatibility when loading old config files.
	// It is migrated to Router.Profiles during loading and always nil after that.
	Profiles map[string]ProfileConfig `json:"profiles,omitempty"` // Legacy location - migrated to Router.Profiles
}

// MultiUserConfig is deprecated — settings now live in SQLite (multi_user_settings table).
// Kept for loading legacy config files during one-time migration.
type MultiUserConfig struct {
	Enabled        bool          `json:"enabled,omitempty"`
	GlobalMaxConc  int           `json:"globalMaxConcurrency,omitempty"` // 0 = auto (100)
	WREDMinDepth   float64       `json:"wredMinDepth,omitempty"`          // default 0.5
	WREDMaxDepth   float64       `json:"wredMaxDepth,omitempty"`          // default 0.9
	Groups         []GroupConfig `json:"groups,omitempty"`
}

// GroupConfig is deprecated — groups now live in SQLite (user_groups table).
// Kept for loading legacy config files during one-time migration.
type GroupConfig struct {
	Name           string  `json:"name"`
	Profile        string  `json:"profile"`
	PriorityWeight float64 `json:"priorityWeight"`
	MaxConcurrency int     `json:"maxConcurrency"`
}

// ProfileConfig represents a named route profile.
// Profiles allow users to define multiple route configurations
// and switch between them during a session without restarting.
type ProfileConfig struct {
	Name        string            `json:"name"`                  // Display name for the profile
	Description string            `json:"description,omitempty"` // Optional description
	Routes      map[string]string `json:"routes"`                // Route name to provider:model chain
}

// ServerConfig represents server configuration.
type ServerConfig struct {
	Port int    `json:"port"`
	Host string `json:"host"`
	// TLS enables HTTPS on the standalone server. When set, the server accepts
	// HTTPS on Host:Port. Nil/empty = plaintext HTTP (the default, suitable for
	// local use and reverse-proxy deployments).
	TLS *TLSConfig `json:"tls,omitempty"`
	// AutoRestartIdle enables self-restart after the server is idle (no requests
	// and zero in-flight connections) for the given duration. Empty = disabled.
	AutoRestartIdle string `json:"autoRestartIdle,omitempty"`
	// AutoRestartWindow restricts restart to a time-of-day range "HH:MM-HH:MM"
	// interpreted in AutoRestartTimezone. Empty = always eligible.
	AutoRestartWindow string `json:"autoRestartWindow,omitempty"`
	// AutoRestartTimezone is an IANA timezone name (e.g. "Asia/Shanghai") used to
	// evaluate AutoRestartWindow. Empty = server local time.
	AutoRestartTimezone string `json:"autoRestartTimezone,omitempty"`
	// AutoRestartBackoffMax is the cap on a random delay applied after idle fires
	// and before the restart. Empty/0 = no backoff.
	AutoRestartBackoffMax string `json:"autoRestartBackoffMax,omitempty"`
	// UserAgent overrides the User-Agent header sent to providers. Empty = use the
	// default that mimics the @anthropic-ai/sdk User-Agent Claude Code sends.
	UserAgent string `json:"userAgent,omitempty"`
}

// TLSConfig configures HTTPS on the standalone server. Two modes, mutually
// exclusive: manual cert files (CertFile + KeyFile) or automatic Let's Encrypt
// certificates (Domain). Redirect listens on :80 and 301-redirects HTTP→HTTPS;
// in autocert mode it also serves the ACME http-01 challenge.
type TLSConfig struct {
	// CertFile is the path to a PEM-encoded certificate (manual cert mode).
	// Required together with KeyFile. Mutually exclusive with Domain.
	CertFile string `json:"certFile,omitempty"`
	// KeyFile is the path to the certificate's private key (manual cert mode).
	KeyFile string `json:"keyFile,omitempty"`
	// Domain enables autocert (Let's Encrypt) for the given FQDN. The domain
	// must point at this server and port 80 must be reachable from the internet.
	// Mutually exclusive with CertFile/KeyFile.
	Domain string `json:"domain,omitempty"`
	// Redirect listens on :80 and redirects plain HTTP requests to HTTPS. In
	// autocert mode this is forced on (the ACME http-01 challenge needs :80).
	Redirect bool `json:"redirect,omitempty"`
}

// Mode returns the TLS mode implied by this config: "manual", "autocert", or
// "" (disabled). Returns "manual" only when both CertFile and KeyFile are set.
func (t *TLSConfig) Mode() string {
	if t == nil {
		return ""
	}
	if t.Domain != "" {
		return "autocert"
	}
	if t.CertFile != "" && t.KeyFile != "" {
		return "manual"
	}
	return ""
}

// Enabled reports whether any TLS mode is configured.
func (t *TLSConfig) Enabled() bool {
	return t.Mode() != ""
}

// GetAutoRestartIdle parses AutoRestartIdle as a duration. Returns 0 (disabled)
// on empty string or parse error.
func (sc *ServerConfig) GetAutoRestartIdle() time.Duration {
	if sc.AutoRestartIdle == "" {
		return 0
	}
	d, err := time.ParseDuration(sc.AutoRestartIdle)
	if err != nil {
		return 0
	}
	return d
}

// GetAutoRestartWindow parses AutoRestartWindow as "HH:MM-HH:MM" (24h, strict
// 2-digit hour and minute). Returns enabled=false if empty or malformed.
// Supports overnight wrap (e.g. "23:00-04:00").
func (sc *ServerConfig) GetAutoRestartWindow() (start, end time.Duration, enabled bool) {
	if sc.AutoRestartWindow == "" {
		return 0, 0, false
	}
	parts := strings.Split(sc.AutoRestartWindow, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])
	// time.Parse("15:04", ...) is lenient about single-digit hours (it accepts
	// "1:00"), so enforce strict HH:MM via length check before parsing.
	if len(startStr) != 5 || len(endStr) != 5 {
		return 0, 0, false
	}
	s, err := time.Parse("15:04", startStr)
	if err != nil {
		return 0, 0, false
	}
	e, err := time.Parse("15:04", endStr)
	if err != nil {
		return 0, 0, false
	}
	start = time.Duration(s.Hour())*time.Hour + time.Duration(s.Minute())*time.Minute
	end = time.Duration(e.Hour())*time.Hour + time.Duration(e.Minute())*time.Minute
	return start, end, true
}

// GetAutoRestartTimezone loads AutoRestartTimezone as an IANA location.
// Returns time.Local on empty string or unknown zone.
func (sc *ServerConfig) GetAutoRestartTimezone() *time.Location {
	if sc.AutoRestartTimezone == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(sc.AutoRestartTimezone)
	if err != nil {
		return time.Local
	}
	return loc
}

// GetAutoRestartBackoffMax parses AutoRestartBackoffMax as a duration.
// Returns 0 (no backoff) on empty string or parse error.
func (sc *ServerConfig) GetAutoRestartBackoffMax() time.Duration {
	if sc.AutoRestartBackoffMax == "" {
		return 0
	}
	d, err := time.ParseDuration(sc.AutoRestartBackoffMax)
	if err != nil {
		return 0
	}
	return d
}

// CompactionConfig controls how oversized requests are compacted to fit within provider limits.
type CompactionConfig struct {
	// Method is "llm" (default) to summarize via LLM, or "trim" to drop oldest messages.
	Method string `json:"method,omitempty"`

	// SummarizeProvider is the provider name used for summarization (auto-detect if empty).
	SummarizeProvider string `json:"summarizeProvider,omitempty"`

	// SummarizeModel is the model for summarization (optional, uses provider default).
	SummarizeModel string `json:"summarizeModel,omitempty"`
}

// ProviderConfig represents a provider configuration.
type ProviderConfig struct {
	APIKey            string   `json:"apiKey"`
	BaseURL           string   `json:"baseURL"`
	Models            []string `json:"models"`
	Transformer       string   `json:"transformer,omitempty"`
	DisableKeepAlives bool     `json:"disableKeepAlives,omitempty"` // Disable HTTP keep-alive for providers with connection issues

	// MaxRequestBodyBytes is the maximum request body size in bytes for this provider.
	// 0 means no limit. Requests exceeding this limit trigger compaction (if configured)
	// or are skipped during failover.
	MaxRequestBodyBytes int64            `json:"maxRequestBodyBytes,omitempty"`
	Compaction          *CompactionConfig `json:"compaction,omitempty"`
}

// Validate validates the provider configuration.
func (pc *ProviderConfig) Validate() error {
	if pc.APIKey == "" {
		return fmt.Errorf("apiKey is required")
	}
	if pc.BaseURL == "" {
		return fmt.Errorf("baseURL is required")
	}
	if len(pc.Models) == 0 {
		return fmt.Errorf("at least one model is required")
	}
	return nil
}

// RouterConfig represents router configuration.
type RouterConfig struct {
	Routes     map[string]string          `json:"routes,omitempty"`     // Legacy routes (empty when profiles are used)
	Profiles   map[string]ProfileConfig   `json:"profiles,omitempty"`   // Named route profiles (new location)
	MaxRetries int                        `json:"maxRetries,omitempty"` // Maximum retries for failover
	RetryDelay string                     `json:"retryDelay,omitempty"` // Delay between retries
}

// LoggingConfig represents logging configuration.
type LoggingConfig struct {
	// Enabled controls whether logging is active.
	// If false or not specified, logging is disabled.
	// Default: false (opt-in only)
	Enabled bool `json:"enabled,omitempty"`

	// Destination is where logs should be written.
	// Valid values: "stdout", "stderr", "file", or a file path.
	// If "file", uses the default log file path.
	Destination string `json:"destination,omitempty"`

	// FilePath is the specific file path when Destination is "file" or a custom path.
	// If empty, uses the default: ~/.cc-modelrouter/router.log
	FilePath string `json:"filePath,omitempty"`

	// Level controls log verbosity.
	// Valid values: "debug", "info" (default), "warn", "error".
	// - debug: Shows all messages including detailed streaming events
	// - info: Shows request/response summaries and warnings
	// - warn: Shows only warnings and errors
	// - error: Shows only errors
	Level string `json:"level,omitempty"`
}

// GetLogPath returns the resolved log file path.
func (lc *LoggingConfig) GetLogPath() (string, error) {
	if lc.FilePath != "" {
		return lc.FilePath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".cc-modelrouter", "router.log"), nil
}

// GetLogPathWithInstance returns the resolved log file path with an instance ID.
// The log file will be named <instanceID>.log in the logs directory.
func (lc *LoggingConfig) GetLogPathWithInstance(instanceID string) (string, error) {
	if lc.FilePath != "" {
		return lc.FilePath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".cc-modelrouter", "logs", instanceID+".log"), nil
}

// ShouldLogToFile returns true if logs should go to a file.
func (lc *LoggingConfig) ShouldLogToFile() bool {
	return lc.Destination == "file" || (lc.Destination != "" && lc.Destination != "stdout" && lc.Destination != "stderr")
}

// ShouldLogToConsole returns true if logs should go to console.
func (lc *LoggingConfig) ShouldLogToConsole() bool {
	return lc.Destination == "stdout" || lc.Destination == "stderr"
}

// IsEnabled returns true if logging is explicitly enabled.
func (lc *LoggingConfig) IsEnabled() bool {
	return lc.Enabled
}

// GetLevel returns the parsed log level, defaulting to LevelInfo.
func (lc *LoggingConfig) GetLevel() LogLevel {
	if lc.Level == "" {
		return LevelInfo // default
	}
	return ParseLogLevel(lc.Level)
}

// GetLogWriter returns the appropriate writer for the log destination.
func (lc *LoggingConfig) GetLogWriter() (io.Writer, error) {
	switch lc.Destination {
	case "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	case "", "file":
		// Default to file
		logPath, err := lc.GetLogPath()
		if err != nil {
			return nil, err
		}
		// Create directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			return nil, err
		}
		return os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	default:
		// Treat as a custom file path
		if err := os.MkdirAll(filepath.Dir(lc.Destination), 0755); err != nil {
			return nil, err
		}
		return os.OpenFile(lc.Destination, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	}
}
func (rc *RouterConfig) GetRetryDelay() time.Duration {
	d, err := time.ParseDuration(rc.RetryDelay)
	if err != nil {
		return 500 * time.Millisecond
	}
	return d
}

// RouteTarget represents a parsed route target.
type RouteTarget struct {
	Provider string
	Model    string
}

// GetActiveRoutes returns the routes to use based on profile name or legacy config.
// If profiles are configured and profileName is set, returns that profile's routes.
// Otherwise, falls back to the legacy router.routes for backward compatibility.
func (cfg *Config) GetActiveRoutes(profileName string) map[string]string {
	// Check if profiles are configured in Router
	if len(cfg.Router.Profiles) > 0 && profileName != "" {
		if profile, ok := cfg.Router.Profiles[profileName]; ok {
			return profile.Routes
		}
	}
	// Fall back to legacy routes
	return cfg.Router.Routes
}

// GetDefaultProfile returns the default profile name to use at startup.
// Returns "default" if it exists, otherwise the first profile alphabetically.
// Returns "" if no profiles are configured (legacy mode).
func (cfg *Config) GetDefaultProfile() string {
	if len(cfg.Router.Profiles) == 0 {
		return ""
	}
	// Prefer "default" profile if it exists
	if _, ok := cfg.Router.Profiles["default"]; ok {
		return "default"
	}
	// Return first profile alphabetically
	for name := range cfg.Router.Profiles {
		return name
	}
	return ""
}

// HasProfiles returns true if profiles are configured.
func (cfg *Config) HasProfiles() bool {
	return len(cfg.Router.Profiles) > 0
}

// GetProfileNames returns a sorted list of profile names.
func (cfg *Config) GetProfileNames() []string {
	names := make([]string, 0, len(cfg.Router.Profiles))
	for name := range cfg.Router.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ParseRoute parses a route string into targets.
// Format: "provider1:model1;provider2:model2"
func ParseRoute(route string) []RouteTarget {
	var targets []RouteTarget
	parts := strings.Split(route, ";")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		providerModel := strings.SplitN(part, ":", 2)
		if len(providerModel) != 2 {
			continue
		}

		targets = append(targets, RouteTarget{
			Provider: strings.TrimSpace(providerModel[0]),
			Model:    strings.TrimSpace(providerModel[1]),
		})
	}

	return targets
}

// Defaults returns the default configuration.
func Defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 8081,
			Host: "localhost",
		},
		Providers: make(map[string]ProviderConfig),
		Router: RouterConfig{
			Routes:   make(map[string]string),
			Profiles: make(map[string]ProfileConfig), // Empty profiles for legacy mode
			MaxRetries: 2,
			RetryDelay: "500ms",
		},
		// Logging is opt-in - not included in defaults
	}
}

// String returns the string representation of the LogLevel.
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ShouldLog returns true if the given message level should be logged
// based on the current log level. Messages with a level equal to or
// higher than the current level will be logged.
func (l LogLevel) ShouldLog(msgLevel LogLevel) bool {
	return msgLevel >= l
}

// ParseLogLevel parses a string into a LogLevel.
// The comparison is case-insensitive. Empty or invalid strings
// default to LevelInfo.
func ParseLogLevel(s string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}
