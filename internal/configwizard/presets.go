package configwizard

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iimmutable/cc-modelrouter/internal/logging"
)

// presetsFilename is the JSON file name used to override provider presets.
// It is searched in both the global (~/.cc-modelrouter) and project
// (<cwd>/.cc-modelrouter) config directories. Project files override global.
const presetsFilename = "provider-presets.json"

// defaultPresetsURL is the canonical source for provider presets. It is a
// package var (not const) so tests can override it to point at a local
// httptest.Server. On the first run of `ccrouter config`, if no global
// presets file exists, the wizard fetches this URL and writes it to
// ~/.cc-modelrouter/provider-presets.json.
var defaultPresetsURL = "https://raw.githubusercontent.com/iimmutable/cc-modelrouter/master/presets/provider-presets.json"

// ProviderPresets is the merged set of provider presets available to the
// wizard. It is initialized at package load time by loadProviderPresets from:
//  1. Auto-fetched canonical presets (on first run, written to
//     ~/.cc-modelrouter/provider-presets.json if missing)
//  2. ~/.cc-modelrouter/provider-presets.json (global)
//  3. <cwd>/.cc-modelrouter/provider-presets.json (project override)
//
// Each provider key present in a JSON file fully replaces the corresponding
// inherited entry. Missing keys keep their inherited value.
var ProviderPresets = loadProviderPresets()

// presetsJSON mirrors ProviderPreset with JSON-friendly field tags.
// Decoding into a dedicated type lets us validate/normalize fields before
// populating the canonical ProviderPreset map.
type presetsJSON struct {
	BaseURL     string   `json:"baseUrl"`
	Transformer string   `json:"transformer"`
	Models      []string `json:"models"`
}

func loadProviderPresets() map[string]ProviderPreset {
	result := make(map[string]ProviderPreset)

	// Ensure the global presets file exists. On first run of `ccrouter config`
	// (when configwizard is imported) the file is auto-fetched from GitHub.
	// On failure, log a warning and continue with empty presets — the wizard
	// still launches and the user can add providers manually.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		globalPath := filepath.Join(home, ".cc-modelrouter", presetsFilename)
		if _, statErr := os.Stat(globalPath); os.IsNotExist(statErr) {
			if fetchErr := fetchDefaultPresets(globalPath); fetchErr != nil {
				logging.Warnf("[PRESETS] Could not fetch defaults from %s: %v", defaultPresetsURL, fetchErr)
			}
		}

		// Layer 1: global presets (may have just been fetched).
		mergePresetFile(result, globalPath)
	}

	// Layer 2: project overrides (cwd). A missing file is not an error.
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		mergePresetFile(result, filepath.Join(cwd, ".cc-modelrouter", presetsFilename))
	}

	return result
}

// fetchDefaultPresets downloads the canonical presets JSON from GitHub and
// writes it to path. Returns nil if fetch/write fails (caller proceeds with
// empty presets; user can retry by deleting the file and re-running config).
//
// Validation: HTTP 200, Content-Type contains json OR body starts with '{',
// and json.Unmarshal succeeds. A corrupted file is never written to disk.
func fetchDefaultPresets(path string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(defaultPresetsURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errInvalidResponse("HTTP " + resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errInvalidResponse("empty response body")
	}

	// Validate Content-Type or leading character before trusting the body.
	ct := resp.Header.Get("Content-Type")
	trimmed := strings.TrimSpace(string(body))
	if !strings.Contains(ct, "json") && !strings.HasPrefix(trimmed, "{") {
		return errInvalidResponse("unexpected content-type: " + ct)
	}

	// Validate the body is a JSON object before writing to disk.
	var raw map[string]presetsJSON
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err
	}

	logging.Debugf("[PRESETS] Downloaded defaults to %s", path)
	return nil
}

// errInvalidResponse is a sentinel type for fetch validation failures.
type errInvalidResponse string

func (e errInvalidResponse) Error() string { return string(e) }

// mergePresetFile reads a JSON preset file and merges it into dst.
// Each provider key in the JSON file fully replaces the existing entry in dst.
// Missing files are silently ignored. Parse errors are logged as warnings.
func mergePresetFile(dst map[string]ProviderPreset, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logging.Warnf("[PRESETS] Failed to read %s: %v", path, err)
		}
		return
	}

	var raw map[string]presetsJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		logging.Warnf("[PRESETS] Failed to parse %s: %v", path, err)
		return
	}

	for name, p := range raw {
		// Models slice must be copied so callers can't mutate the JSON-backed
		// slice through ProviderPresets.
		models := make([]string, len(p.Models))
		copy(models, p.Models)
		dst[name] = ProviderPreset{
			BaseURL:     p.BaseURL,
			Transformer: p.Transformer,
			Models:      models,
		}
	}
}
