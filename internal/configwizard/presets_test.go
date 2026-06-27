package configwizard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// withTestHome swaps HOME to a temp dir for the duration of t, returning the
// global presets path inside that temp home.
func withTestHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	return filepath.Join(tmpHome, ".cc-modelrouter", presetsFilename)
}

// withMockPresetsServer returns an httptest.Server that serves the given body
// for all GET requests, and swaps defaultPresetsURL to point at it. The
// server is closed automatically when the test ends.
func withMockPresetsServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	defaultPresetsURL = srv.URL
	return srv
}

func TestAutoFetch_CreatesGlobalFile(t *testing.T) {
	globalPath := withTestHome(t)
	payload := `{"anthropic":{"baseUrl":"https://api.anthropic.com","transformer":"anthropic","models":["claude-x"]}}`
	withMockPresetsServer(t, http.StatusOK, payload)

	// Sanity: file does not exist before the call.
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatalf("expected global presets file to not exist before fetch")
	}

	presets := loadProviderPresets()

	// File should have been written to disk.
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("global presets file was not created: %v", err)
	}
	if string(data) != payload {
		t.Errorf("fetched file content mismatch:\n got: %s\n want: %s", string(data), payload)
	}

	// Presets should reflect the fetched data.
	if p, ok := presets["anthropic"]; !ok {
		t.Error("expected 'anthropic' preset to be loaded")
	} else if p.BaseURL != "https://api.anthropic.com" {
		t.Errorf("anthropic BaseURL mismatch: got %s", p.BaseURL)
	}
}

func TestAutoFetch_DoesNotOverwrite(t *testing.T) {
	globalPath := withTestHome(t)

	// Pre-create global file with a custom provider.
	custom := `{"my-custom":{"baseUrl":"https://custom.test","transformer":"anthropic","models":["m1"]}}`
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(globalPath, []byte(custom), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Mock server would return different data — must not be fetched.
	mockHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mockHit = true
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"decoy":{"baseUrl":"https://decoy.test","transformer":"anthropic","models":["x"]}}`)
	}))
	t.Cleanup(srv.Close)
	defaultPresetsURL = srv.URL

	presets := loadProviderPresets()

	if mockHit {
		t.Error("fetch should be skipped when global presets file already exists")
	}
	if _, ok := presets["decoy"]; ok {
		t.Error("decoy provider from mock server should not appear")
	}
	if p, ok := presets["my-custom"]; !ok {
		t.Error("custom provider should survive")
	} else if p.BaseURL != "https://custom.test" {
		t.Errorf("custom provider BaseURL mismatch: got %s", p.BaseURL)
	}
}

func TestAutoFetch_NetworkFailure(t *testing.T) {
	globalPath := withTestHome(t)
	withMockPresetsServer(t, http.StatusInternalServerError, "")

	presets := loadProviderPresets()

	// No file should have been written.
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Errorf("global presets file should not be written on fetch failure")
	}
	// Empty map is acceptable — wizard still launches with manual entry.
	if len(presets) != 0 {
		t.Errorf("expected empty presets on fetch failure, got %d entries", len(presets))
	}
}

func TestAutoFetch_InvalidJSONNotWritten(t *testing.T) {
	globalPath := withTestHome(t)
	withMockPresetsServer(t, http.StatusOK, "this is not json")

	loadProviderPresets()

	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Errorf("corrupted presets file should not be written to disk")
	}
}

func TestMergePresetFile_ReplacesEntries(t *testing.T) {
	tmpDir := t.TempDir()
	presetPath := filepath.Join(tmpDir, "provider-presets.json")

	// Override "bigmodel" and add a new provider "custom-test".
	testData := map[string]presetsJSON{
		"bigmodel": {
			BaseURL:     "https://custom.bigmodel.url",
			Transformer: "custom_transformer",
			Models:      []string{"custom-model-1", "custom-model-2"},
		},
		"custom-test": {
			BaseURL:     "https://custom.test.url",
			Transformer: "test_transformer",
			Models:      []string{"test-model"},
		},
	}
	data, err := json.Marshal(testData)
	if err != nil {
		t.Fatalf("failed to marshal test data: %v", err)
	}
	if err := os.WriteFile(presetPath, data, 0o644); err != nil {
		t.Fatalf("failed to write preset file: %v", err)
	}

	// Seed dst with a base layer to verify per-key replacement semantics.
	dst := map[string]ProviderPreset{
		"bigmodel":  {BaseURL: "https://original.bigmodel", Transformer: "glm_anthropic", Models: []string{"glm-4"}},
		"untouched": {BaseURL: "https://untouched.test", Transformer: "anthropic", Models: []string{"u1"}},
	}

	mergePresetFile(dst, presetPath)

	if preset, ok := dst["bigmodel"]; !ok {
		t.Error("bigmodel should still exist after merge")
	} else {
		if preset.BaseURL != "https://custom.bigmodel.url" {
			t.Errorf("bigmodel BaseURL should be replaced: got %s", preset.BaseURL)
		}
		if preset.Transformer != "custom_transformer" {
			t.Errorf("bigmodel Transformer should be replaced: got %s", preset.Transformer)
		}
		if len(preset.Models) != 2 || preset.Models[0] != "custom-model-1" {
			t.Errorf("bigmodel Models should be replaced: got %v", preset.Models)
		}
	}

	if preset, ok := dst["custom-test"]; !ok {
		t.Error("custom-test should be added by merge")
	} else if preset.BaseURL != "https://custom.test.url" {
		t.Errorf("custom-test BaseURL mismatch: got %s", preset.BaseURL)
	}

	// Unmerged presets remain unchanged.
	if preset, ok := dst["untouched"]; !ok {
		t.Error("untouched should still exist")
	} else if preset.BaseURL != "https://untouched.test" {
		t.Errorf("untouched BaseURL should be unchanged: got %s", preset.BaseURL)
	}
}

func TestMergePresetFile_IgnoresMissingFile(t *testing.T) {
	dst := make(map[string]ProviderPreset)
	dst["test"] = ProviderPreset{BaseURL: "original"}

	mergePresetFile(dst, "/nonexistent/path/provider-presets.json")

	if dst["test"].BaseURL != "original" {
		t.Error("missing file should not modify dst")
	}
}

func TestMergePresetFile_KeepsDstOnParseError(t *testing.T) {
	tmpDir := t.TempDir()
	presetPath := filepath.Join(tmpDir, "provider-presets.json")

	if err := os.WriteFile(presetPath, []byte("{invalid json}"), 0644); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}

	dst := make(map[string]ProviderPreset)
	dst["test"] = ProviderPreset{BaseURL: "original"}

	mergePresetFile(dst, presetPath)

	if dst["test"].BaseURL != "original" {
		t.Error("parse error should not modify dst")
	}
}

// TestBigModelPresetBaseURL locks the canonical BigModel Anthropic-compatible
// baseURL. The glm_anthropic transformer appends "/v1/messages" to the baseURL
// when it does not already end with that suffix (glm_anthropic.go:117-118).
// A bare "https://open.bigmodel.cn/api" therefore produces the non-existent
// "/api/v1/messages" endpoint, which BigModel's gateway rejects with HTTP 403
// permission_error. The correct path is "/api/anthropic", which yields
// "/api/anthropic/v1/messages" — the documented Anthropic-compatible endpoint.
// See docs/troubleshooting.md (BigModel section) and
// https://docs.bigmodel.cn/cn/guide/develop/claude/introduction.
func TestBigModelPresetBaseURL(t *testing.T) {
	bm, ok := ProviderPresets["bigmodel"]
	if !ok {
		t.Fatal("bigmodel preset missing from ProviderPresets")
	}
	const want = "https://open.bigmodel.cn/api/anthropic"
	if bm.BaseURL != want {
		t.Errorf("bigmodel baseURL = %q, want %q (transformer appends /v1/messages; "+
			"bare /api yields non-existent /api/v1/messages → 403 permission_error)",
			bm.BaseURL, want)
	}
}

func TestMergePresetFile_ModelsSliceIsCopy(t *testing.T) {
	// Verify the merged Models slice is a copy, not an alias into shared
	// backing memory, so caller mutations stay isolated.
	tmpDir := t.TempDir()
	presetPath := filepath.Join(tmpDir, "provider-presets.json")

	testData := map[string]presetsJSON{
		"test": {Models: []string{"model-a", "model-b"}},
	}
	data, _ := json.Marshal(testData)
	os.WriteFile(presetPath, data, 0644)

	dst1 := make(map[string]ProviderPreset)
	mergePresetFile(dst1, presetPath)

	dst2 := make(map[string]ProviderPreset)
	mergePresetFile(dst2, presetPath)

	// Mutating dst1's slice must not affect dst2's slice.
	dst1["test"].Models[0] = "mutated"
	if dst2["test"].Models[0] != "model-a" {
		t.Errorf("dst2 slice should be independent: got %s", dst2["test"].Models[0])
	}
}