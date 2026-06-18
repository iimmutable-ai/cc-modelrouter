package configwizard

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain seeds ProviderPresets from the canonical presets JSON checked
// into the repo (presets/provider-presets.json). Without this, tests that
// reference ProviderPresets depend on network access at package-init time
// (loadProviderPresets auto-fetches from GitHub). Seeding here makes the
// wizard tests hermetic and deterministic.
func TestMain(m *testing.M) {
	seedPresetsFromRepo()
	os.Exit(m.Run())
}

// seedPresetsFromRepo locates the repo's canonical presets JSON (walking up
// from the test working directory) and merges it into ProviderPresets so the
// wizard tests have the same data that production users see after auto-fetch.
func seedPresetsFromRepo() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "presets", "provider-presets.json")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			mergePresetFile(ProviderPresets, candidate)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}
