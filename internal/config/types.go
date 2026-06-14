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
	MultiUser MultiUserConfig           `json:"multiUser,omitempty"`
	// Profiles is kept at Config level for backward compatibility when loading old config files.
	// It is migrated to Router.Profiles during loading and always nil after that.
	Profiles map[string]ProfileConfig `json:"profiles,omitempty"` // Legacy location - migrated to Router.Profiles
}

// MultiUserConfig controls multi-user authentication and QoS.
type MultiUserConfig struct {
	Enabled        bool          `json:"enabled,omitempty"`
	GlobalMaxConc  int           `json:"globalMaxConcurrency,omitempty"` // 0 = auto (100)
	WREDMinDepth   float64       `json:"wredMinDepth,omitempty"`          // default 0.5
	WREDMaxDepth   float64       `json:"wredMaxDepth,omitempty"`          // default 0.9
	Groups         []GroupConfig `json:"groups,omitempty"`
}

// GroupConfig defines a user group with profile mapping and QoS settings.
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
