package configwizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// EffectiveHomeDir delegates to config.EffectiveHomeDir. Kept for backward
// compatibility with callers that predate the move to internal/config.
func EffectiveHomeDir() (string, error) { return config.EffectiveHomeDir() }

// ShellConfig handles shell configuration for API keys.
type ShellConfig struct {
	ShellPath    string
	RCFilePath   string
	ExportLine   string
}

// GetShellConfig returns the appropriate shell configuration.
func GetShellConfig() (*ShellConfig, error) {
	// Detect shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh" // Default to zsh on macOS
	}

	var rcPath string
	if strings.Contains(shell, "bash") {
		// Try .bashrc first, then .bash_profile
		home, _ := EffectiveHomeDir()
		if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
			rcPath = filepath.Join(home, ".bashrc")
		} else if _, err := os.Stat(filepath.Join(home, ".bash_profile")); err == nil {
			rcPath = filepath.Join(home, ".bash_profile")
		} else {
			rcPath = filepath.Join(home, ".bashrc")
		}
	} else {
		// Default to zsh
		home, _ := EffectiveHomeDir()
		rcPath = filepath.Join(home, ".zshrc")
	}

	return &ShellConfig{
		ShellPath:  shell,
		RCFilePath: rcPath,
	}, nil
}

// GenerateExportLine generates the shell export line for an API key.
//
// Emits a `# provider: <name>` comment immediately before the export so
// parseEnvExports can recover the original provider name (including case)
// from the file. This closes the round-trip gap that produced duplicate
// lowercase provider entries (e.g. `bigmodelKNDY` → `bigmodelkndy`) when
// the env var name was the only carrier of the provider name — ToUpper in
// GenerateEnvVarName collapsed case, and the reverse ToLower in
// parseEnvExports had no way to recover it.
func GenerateExportLine(providerName, apiKey string) string {
	varName := GenerateEnvVarName(providerName)
	return fmt.Sprintf(`# provider: %s
export %s="%s"`, providerName, varName, apiKey)
}

// AddToShellConfig adds the API key export to the shell RC file.
// Uses a two-phase approach: remove all existing ccrouter entries for this
// provider, then append a single fresh comment+export pair.
func (s *ShellConfig) AddToShellConfig(providerName, apiKey string) error {
	varName := GenerateEnvVarName(providerName)
	commentPrefix := fmt.Sprintf("# ccrouter - %s", providerName)
	exportPrefix := fmt.Sprintf("export %s=", varName)

	// Read existing RC file
	existingContent := ""
	if _, err := os.Stat(s.RCFilePath); err == nil {
		content, err := os.ReadFile(s.RCFilePath)
		if err == nil {
			existingContent = string(content)
		}
	}

	// Phase 1: Remove ALL lines matching the comment or export for this provider
	var filtered []string
	for _, line := range strings.Split(existingContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == commentPrefix || strings.HasPrefix(trimmed, exportPrefix) {
			continue
		}
		filtered = append(filtered, line)
	}

	// Phase 2: Append a single fresh comment + export pair
	filtered = append(filtered, "", commentPrefix, fmt.Sprintf(`export %s="%s"`, varName, apiKey))

	result := strings.Join(filtered, "\n")
	return os.WriteFile(s.RCFilePath, []byte(result), 0644)
}

// RemoveFromShellConfig removes the API key export for a provider from the RC file.
// Uses the same Phase 1 filtering as AddToShellConfig but without Phase 2 append.
func (s *ShellConfig) RemoveFromShellConfig(providerName string) error {
	varName := GenerateEnvVarName(providerName)
	commentPrefix := fmt.Sprintf("# ccrouter - %s", providerName)
	exportPrefix := fmt.Sprintf("export %s=", varName)

	existingContent := ""
	if _, err := os.Stat(s.RCFilePath); err == nil {
		content, err := os.ReadFile(s.RCFilePath)
		if err == nil {
			existingContent = string(content)
		}
	}

	var filtered []string
	for _, line := range strings.Split(existingContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == commentPrefix || strings.HasPrefix(trimmed, exportPrefix) {
			continue
		}
		filtered = append(filtered, line)
	}

	result := strings.Join(filtered, "\n")
	return os.WriteFile(s.RCFilePath, []byte(result), 0644)
}

// SyncAllShellExports removes ALL ccrouter entries from the RC file, then
// appends a single source line that loads ~/.cc-modelrouter/shell_env.sh.
// The real API keys live in shell_env.sh (written by WriteEnvFile); the RC
// file only needs the one source directive, so re-running the wizard keeps
// the RC stable while shell_env.sh absorbs key changes.
func (s *ShellConfig) SyncAllShellExports(apiKeys map[string]string) error {
	existingContent := ""
	if _, err := os.Stat(s.RCFilePath); err == nil {
		content, err := os.ReadFile(s.RCFilePath)
		if err == nil {
			existingContent = string(content)
		}
	}

	// Phase 1: strip every ccrouter-managed line we might have written
	// previously — per-provider exports, their comments, and the older
	// single-source form (so the append below is exactly-once).
	var filtered []string
	for _, line := range strings.Split(existingContent, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "# ccrouter - "):
			continue
		case strings.HasPrefix(trimmed, "export CCROUTER_"):
			continue
		case trimmed == "# ccrouter-env (auto-source API keys — managed by ccrouter config)":
			continue
		case strings.HasPrefix(trimmed, "[ -f ") && strings.Contains(trimmed, "shell_env.sh ]"):
			continue
		}
		filtered = append(filtered, line)
	}

	// Phase 2: append a single source directive. shell_env.sh is the
	// authoritative store written by WriteEnvFile; any provider whose key
	// is empty gets skipped there, so no filtering is needed here.
	_ = apiKeys
	filtered = append(filtered,
		"",
		"# ccrouter-env (auto-source API keys — managed by ccrouter config)",
		`[ -f ~/.cc-modelrouter/shell_env.sh ] && source ~/.cc-modelrouter/shell_env.sh`,
	)

	result := strings.Join(filtered, "\n")
	return os.WriteFile(s.RCFilePath, []byte(result), 0644)
}

// SourceNow exports the API key in the current process environment.
func (s *ShellConfig) SourceNow(providerName, apiKey string) error {
	varName := GenerateEnvVarName(providerName)
	return os.Setenv(varName, apiKey)
}

// SourceAllNow exports all API keys in the current process environment.
func (s *ShellConfig) SourceAllNow(apiKeys map[string]string) {
	for provider, key := range apiKeys {
		if key != "" {
			_ = s.SourceNow(provider, key)
		}
	}
}

// WriteEnvFile writes a shell env file at ~/.cc-modelrouter/shell_env.sh
// containing export lines for the given API keys. Returns the file path.
func (s *ShellConfig) WriteEnvFile(apiKeys map[string]string) (string, error) {
	home, _ := EffectiveHomeDir()
	dir := filepath.Join(home, ".cc-modelrouter")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config dir: %w", err)
	}
	path := filepath.Join(dir, "shell_env.sh")

	var lines []string
	lines = append(lines, "# Auto-generated by ccrouter config wizard")
	lines = append(lines, "# Source this file to load API keys: source "+path)
	for provider, key := range apiKeys {
		if key != "" {
			lines = append(lines, GenerateExportLine(provider, key))
		}
	}
	return path, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// LoadEnvFile reads ~/.cc-modelrouter/shell_env.sh and returns a map of
// provider name → API key. Returns an empty map (nil error) if the file
// does not exist. Malformed lines are skipped silently.
//
// This is the inverse of WriteEnvFile. Provider names written by current
// code are recovered verbatim from the `# provider: <name>` comment that
// precedes each export, so mixed-case names like `bigmodelKNDY` round-trip
// exactly. Legacy files written before the case-preservation fix lacked
// the comment; for those, the name is derived from the env var name by
// stripping the CCROUTER_ prefix and _API_KEY suffix and lowercasing, so
// mixed-case custom names come back lowercased. Re-running
// `ccrouter config` regenerates the file with comments.
func (s *ShellConfig) LoadEnvFile() (map[string]string, error) {
	home, _ := EffectiveHomeDir()
	path := filepath.Join(home, ".cc-modelrouter", "shell_env.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	return parseEnvExports(string(data)), nil
}

// parseEnvExports extracts CCROUTER_<NAME>_API_KEY="value" pairs from the
// shell_env.sh file content into a provider-name → key map.
//
// When a `# provider: <name>` comment immediately precedes an export line,
// the name is taken verbatim from the comment, preserving original casing.
// Without such a comment (legacy shell_env.sh files written before the
// case-preservation fix), the name is derived from the env var by stripping
// `CCROUTER_` and `_API_KEY` and lowercasing — the historical behavior.
func parseEnvExports(content string) map[string]string {
	out := map[string]string{}
	const exportPrefix = "export CCROUTER_"
	const providerCommentPrefix = "# provider:"
	var pendingProviderName string
	pendingValid := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, providerCommentPrefix) {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, providerCommentPrefix))
			if name != "" {
				pendingProviderName = name
				pendingValid = true
			}
			continue
		}
		if !strings.HasPrefix(trimmed, exportPrefix) {
			// Any non-blank, non-provider-comment line between a provider
			// comment and its export invalidates the pending name — a
			// provider comment must be immediately followed by its export.
			if trimmed != "" {
				pendingValid = false
			}
			continue
		}
		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			pendingValid = false
			continue
		}
		varName := strings.TrimSpace(trimmed[len(exportPrefix):eq])
		val := strings.TrimSpace(trimmed[eq+1:])
		val = strings.TrimPrefix(val, `"`)
		val = strings.TrimSuffix(val, `"`)
		if !strings.HasSuffix(varName, "_API_KEY") || val == "" {
			pendingValid = false
			continue
		}
		envName := strings.TrimSuffix(varName, "_API_KEY")
		if envName == "" {
			pendingValid = false
			continue
		}
		if pendingValid {
			out[pendingProviderName] = val
		} else {
			out[strings.ToLower(envName)] = val
		}
		pendingValid = false
	}
	return out
}

// GenerateEnvVarName generates the environment variable name for a provider.
func GenerateEnvVarName(providerName string) string {
	// Convert to uppercase and replace non-alphanumeric with underscore
	var result strings.Builder
	for i, c := range strings.ToUpper(providerName) {
		if c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			result.WriteByte(byte(c))
		} else if i > 0 {
			result.WriteByte('_')
		}
	}
	return "CCROUTER_" + result.String() + "_API_KEY"
}

// GetExportPreview returns a preview of what will be added to the shell config.
func GetExportPreview(providerName, apiKey string) string {
	varName := GenerateEnvVarName(providerName)
	if strings.HasPrefix(apiKey, "${") && strings.HasSuffix(apiKey, "}") {
		envName := apiKey[2 : len(apiKey)-1]
		return fmt.Sprintf(`# ccrouter - %s
# (env var %s not set)
export %s="<your-api-key>"`, providerName, envName, varName)
	}
	maskedKey := maskAPIKey(apiKey)
	return fmt.Sprintf(`# ccrouter - %s
export %s="%s"`, providerName, varName, maskedKey)
}

// maskAPIKey masks the API key for display.
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}


// ValidatePort validates that the port is in the valid range.
func ValidatePort(port string) bool {
	var portNum int
	_, err := fmt.Sscanf(port, "%d", &portNum)
	if err != nil {
		return false
	}
	return portNum >= 1024 && portNum <= 65535
}

// ValidateHost validates that the host is not empty.
func ValidateHost(host string) bool {
	return strings.TrimSpace(host) != ""
}