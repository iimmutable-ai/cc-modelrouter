package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// EffectiveHomeDir returns the home directory that ccrouter should treat as
// "the user's home" when reading or writing per-user files such as
// ~/.cc-modelrouter/config.json or ~/.cc-modelrouter/shell_env.sh.
//
// Under bare `sudo` (no `-E`), the process runs with HOME pointing at root's
// home (e.g. /root), which causes os.UserHomeDir() to miss the invoking
// user's files. sudo sets SUDO_USER to the original invoking user, so when
// it is present we look up that user's home via os/user.Lookup and prefer
// it. Any failure (SUDO_USER unset, user.Lookup error, empty HomeDir)
// falls back to os.UserHomeDir() so non-sudo and root-login paths are
// unchanged.
func EffectiveHomeDir() (string, error) {
	if name := os.Getenv("SUDO_USER"); name != "" {
		if u, err := user.Lookup(name); err == nil && u.HomeDir != "" {
			return u.HomeDir, nil
		}
	}
	return os.UserHomeDir()
}

// DataDir returns the directory ccrouter should use for runtime data
// files: usage.db, logs/, instances/, restarts.jsonl, master.key, and
// the autocert cache.
//
// Resolution order:
//  1. $CCROUTER_DATA_DIR — explicit override. The start command sets
//     this from EffectiveHomeDir()+"/.cc-modelrouter" before launching
//     the service; the systemd unit's Environment=HOME=... directive
//     otherwise repoints $HOME at the data dir, which would cause the
//     fallback below to double the suffix (/home/admin/.cc-modelrouter/.cc-modelrouter).
//  2. $HOME/.cc-modelrouter — the default for ad-hoc `ccrouter start`
//     invocations where CCROUTER_DATA_DIR hasn't been set.
//
// All runtime subsystems (logging, usage, daemon, restartlog, auth,
// monitor) MUST route through this helper rather than appending
// ".cc-modelrouter" to os.UserHomeDir() themselves — otherwise they
// produce a doubled path under the systemd unit (where HOME is the
// data dir, not the user's home).
func DataDir() (string, error) {
	if v := os.Getenv("CCROUTER_DATA_DIR"); v != "" {
		return v, nil
	}
	home, err := EffectiveHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cc-modelrouter"), nil
}

// GlobalConfigPath returns the global config file path.
func GlobalConfigPath() string {
	home, _ := EffectiveHomeDir()
	return filepath.Join(home, ".cc-modelrouter", "config.json")
}

// ProjectConfigPath returns the project config file path.
func ProjectConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".cc-modelrouter", "config.json")
}

// Load loads configuration from a file, interpolating ${VAR} references.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Interpolate environment variables and get warnings
	expanded, warnings := interpolateEnvVars(string(data))

	// Print warnings for missing environment variables
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}

	cfg := Defaults()
	if err := json.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Migrate profiles from old location (Config.Profiles) to new location (Router.Profiles)
	// for backward compatibility with older config files.
	if len(cfg.Profiles) > 0 && len(cfg.Router.Profiles) == 0 {
		cfg.Router.Profiles = cfg.Profiles
		cfg.Profiles = nil // Clear old location - won't be saved
	}

	validateAutoRestart(&cfg.Server)

	return cfg, nil
}

// LoadRaw loads configuration from a file without interpolating ${VAR} references.
// This preserves environment variable placeholders in the in-memory config,
// so they are not lost when the config is saved back to disk.
func LoadRaw(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := Defaults()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Migrate profiles from old location (Config.Profiles) to new location (Router.Profiles)
	// for backward compatibility with older config files.
	if len(cfg.Profiles) > 0 && len(cfg.Router.Profiles) == 0 {
		cfg.Router.Profiles = cfg.Profiles
		cfg.Profiles = nil // Clear old location - won't be saved
	}

	validateAutoRestart(&cfg.Server)

	return cfg, nil
}

// validateAutoRestart emits non-fatal stderr warnings for malformed auto-restart
// fields. Bad values degrade the feature to disabled but do not abort loading.
func validateAutoRestart(sc *ServerConfig) {
	if sc.AutoRestartIdle != "" {
		if _, err := time.ParseDuration(sc.AutoRestartIdle); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: server.autoRestartIdle %q is not a valid duration; auto-restart disabled\n", sc.AutoRestartIdle)
		}
	}
	if sc.AutoRestartBackoffMax != "" {
		if _, err := time.ParseDuration(sc.AutoRestartBackoffMax); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: server.autoRestartBackoffMax %q is not a valid duration; backoff disabled\n", sc.AutoRestartBackoffMax)
		}
	}
	if sc.AutoRestartWindow != "" {
		if _, _, ok := sc.GetAutoRestartWindow(); !ok {
			fmt.Fprintf(os.Stderr, "Warning: server.autoRestartWindow %q is not in HH:MM-HH:MM format; window ignored\n", sc.AutoRestartWindow)
		}
	}
	if sc.AutoRestartTimezone != "" {
		if _, err := time.LoadLocation(sc.AutoRestartTimezone); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: server.autoRestartTimezone %q is not a known IANA timezone; using server local time\n", sc.AutoRestartTimezone)
		}
	}
}

// LoadWithOverride loads project config if exists, otherwise global.
func LoadWithOverride(projectRoot string) (*Config, string, error) {
	projectPath := ProjectConfigPath(projectRoot)
	if _, err := os.Stat(projectPath); err == nil {
		cfg, err := Load(projectPath)
		if err != nil {
			return nil, "", err
		}
		return cfg, "project", nil
	}

	globalPath := GlobalConfigPath()
	if _, err := os.Stat(globalPath); err == nil {
		cfg, err := Load(globalPath)
		if err != nil {
			return nil, "", err
		}
		return cfg, "global", nil
	}

	return nil, "", fmt.Errorf("no configuration found")
}

// Save saves configuration to a file.
func Save(cfg *Config, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write with restricted permissions
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// interpolateEnvVars replaces ${VAR} and $VAR with environment variable values.
// Returns the expanded string and a list of warnings for missing environment variables.
func interpolateEnvVars(s string) (string, []string) {
	result := s
	var warnings []string

	// Replace ${VAR} patterns
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		varName := result[start+2 : end]
		varValue := os.Getenv(varName)
		if varValue == "" {
			warnings = append(warnings, fmt.Sprintf("environment variable '%s' is not set", varName))
		}
		result = result[:start] + varValue + result[end+1:]
	}

	// Replace $VAR patterns (word boundary)
	words := strings.Fields(result)
	for _, word := range words {
		if strings.HasPrefix(word, "$") && !strings.Contains(word, "{") {
			varName := word[1:]
			// Handle punctuation at end
			varName = strings.TrimRight(varName, ".,;:!?")
			varValue := os.Getenv(varName)
			if varValue != "" {
				result = strings.ReplaceAll(result, word, varValue)
			} else {
				warnings = append(warnings, fmt.Sprintf("environment variable '%s' is not set", varName))
			}
		}
	}

	return result, warnings
}
