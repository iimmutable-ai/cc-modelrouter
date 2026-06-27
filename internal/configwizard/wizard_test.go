package configwizard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/iimmutable-ai/cc-modelrouter/internal/auth"
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// newTestModel creates a WizardModel with minimal setup for testing.
func newTestModel() *WizardModel {
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Router:    config.RouterConfig{Routes: make(map[string]string)},
	}
	m := NewWizardModel(cfg, "/tmp/test-config.json")
	m.width = 80
	m.height = 40
	return m
}

// toScreen transitions the model to the Add Provider (1/2) screen.
func toScreen(m *WizardModel, screen Screen) {
	m.state.CurrentScreen = screen
}

// ---------------------------------------------------------------------------
// getModelSuggestions
// ---------------------------------------------------------------------------

func TestGetModelSuggestions_NilForUnknownProvider(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "nonexistent"
	m.state.NewProviderModels = ""

	got := m.getModelSuggestions()
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestGetModelSuggestions_AllModelsWhenEmpty(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "anthropic"
	m.state.NewProviderModels = ""

	got := m.getModelSuggestions()
	if got == nil {
		t.Fatal("expected non-nil")
	}
	preset := ProviderPresets["anthropic"]
	if len(got) != len(preset.Models) {
		t.Errorf("expected %d models, got %d", len(preset.Models), len(got))
	}
}

func TestGetModelSuggestions_PrefixFiltering(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "anthropic"
	m.state.NewProviderModels = "claude-sonnet"

	got := m.getModelSuggestions()
	for _, model := range got {
		if !strings.HasPrefix(strings.ToLower(model), "claude-sonnet") {
			t.Errorf("model %q does not match prefix", model)
		}
	}
}

func TestGetModelSuggestions_CaseInsensitive(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "ANTHROPIC"
	m.state.NewProviderModels = "CLAUDE-"

	got := m.getModelSuggestions()
	if got == nil || len(got) == 0 {
		t.Fatal("expected matches for case-insensitive provider and prefix")
	}
	for _, model := range got {
		if !strings.HasPrefix(strings.ToLower(model), "claude-") {
			t.Errorf("model %q should match prefix 'claude-'", model)
		}
	}
}

func TestGetModelSuggestions_ExcludesExistingModels(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "anthropic"
	// Put one model already in the field
	m.state.NewProviderModels = "claude-haiku-4.5\n"

	got := m.getModelSuggestions()
	for _, model := range got {
		if strings.ToLower(model) == "claude-haiku-4.5" {
			t.Errorf("should have excluded already-present model %q", model)
		}
	}
}

func TestGetModelSuggestions_SixItemCap(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "bigmodel"
	m.state.NewProviderModels = ""

	got := m.getModelSuggestions()
	if len(got) > 6 {
		t.Errorf("expected at most 6 items, got %d", len(got))
	}
}

func TestGetModelSuggestions_MatchesByPresetKey(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "openrouter"
	m.state.NewProviderModels = ""

	got := m.getModelSuggestions()
	if got == nil {
		t.Fatal("expected matches for preset key 'openrouter'")
	}
	expected := ProviderPresets["openrouter"].Models
	if len(got) != len(expected) {
		t.Errorf("expected %d models, got %d", len(expected), len(got))
	}
}

func TestGetModelSuggestions_MatchesByPresetName(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "openrouter-openai"
	m.state.NewProviderModels = ""

	got := m.getModelSuggestions()
	if got == nil {
		t.Fatal("expected matches for preset key 'openrouter-openai'")
	}
	expected := ProviderPresets["openrouter-openai"].Models
	if len(got) != len(expected) {
		t.Errorf("expected %d models, got %d", len(expected), len(got))
	}
}

func TestGetModelSuggestions_PrefixOnSecondLine(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "anthropic"
	m.state.NewProviderModels = "claude-haiku-4.5\nclaude-opus"

	got := m.getModelSuggestions()
	// Should filter by "claude-opus" prefix on the current (second) line
	for _, model := range got {
		if !strings.HasPrefix(strings.ToLower(model), "claude-opus") {
			t.Errorf("model %q does not match current line prefix 'claude-opus'", model)
		}
	}
}

// ---------------------------------------------------------------------------
// insertModelFromDropdown
// ---------------------------------------------------------------------------

func TestInsertModelFromDropdown_EmptyText(t *testing.T) {
	m := newTestModel()
	m.state.NewProviderModels = ""

	m.insertModelFromDropdown("claude-sonnet-4.6")

	want := "claude-sonnet-4.6\n"
	if m.state.NewProviderModels != want {
		t.Errorf("got %q, want %q", m.state.NewProviderModels, want)
	}
	if !m.state.ShowModelDropdown {
		t.Error("dropdown should remain open for next selection")
	}
	if m.state.ModelDropdownCursor != 0 {
		t.Errorf("cursor should be 0, got %d", m.state.ModelDropdownCursor)
	}
}

func TestInsertModelFromDropdown_ReplacesCurrentLine(t *testing.T) {
	m := newTestModel()
	m.state.NewProviderModels = "claude-haiku-4.5\nclaude-so"

	m.insertModelFromDropdown("claude-sonnet-4.6")

	want := "claude-haiku-4.5\nclaude-sonnet-4.6\n"
	if m.state.NewProviderModels != want {
		t.Errorf("got %q, want %q", m.state.NewProviderModels, want)
	}
}

func TestInsertModelFromDropdown_PreservesLinesAbove(t *testing.T) {
	m := newTestModel()
	m.state.NewProviderModels = "first-model\nsecond-model\nglm"

	m.insertModelFromDropdown("glm-5-turbo")

	want := "first-model\nsecond-model\nglm-5-turbo\n"
	if m.state.NewProviderModels != want {
		t.Errorf("got %q, want %q", m.state.NewProviderModels, want)
	}
}

func TestInsertModelFromDropdown_MultiLineText(t *testing.T) {
	m := newTestModel()
	m.state.NewProviderModels = "a\nb\nc\nd"

	m.insertModelFromDropdown("replacement")

	want := "a\nb\nc\nreplacement\n"
	if m.state.NewProviderModels != want {
		t.Errorf("got %q, want %q", m.state.NewProviderModels, want)
	}
}

// ---------------------------------------------------------------------------
// applyPreset model auto-populate
// ---------------------------------------------------------------------------

func TestApplyPreset_DoesNotAutoPopulateModelsWhenEmpty(t *testing.T) {
	m := newTestModel()
	m.state.NewProviderModels = ""

	m.applyPreset("anthropic")

	if m.state.NewProviderModels != "" {
		t.Errorf("expected models to remain empty, got %q", m.state.NewProviderModels)
	}
}

func TestApplyPreset_DoesNotOverwriteExistingModels(t *testing.T) {
	m := newTestModel()
	existingModels := "my-custom-model\nanother-model"
	m.state.NewProviderModels = existingModels

	m.applyPreset("anthropic")

	if m.state.NewProviderModels != existingModels {
		t.Errorf("models should not change, got %q", m.state.NewProviderModels)
	}
}

func TestApplyPreset_DoesNotAutoPopulateWhitespaceOnly(t *testing.T) {
	m := newTestModel()
	m.state.NewProviderModels = "   "

	m.applyPreset("bigmodel")

	if strings.TrimSpace(m.state.NewProviderModels) != "" {
		t.Errorf("expected models to remain whitespace-only, got %q", m.state.NewProviderModels)
	}
}

func TestApplyPreset_ClosesDropdown(t *testing.T) {
	m := newTestModel()
	m.state.ShowDropdown = true
	m.state.DropdownCursor = 3

	m.applyPreset("anthropic")

	if m.state.ShowDropdown {
		t.Error("provider dropdown should be closed after applyPreset")
	}
	if m.state.DropdownCursor != 0 {
		t.Errorf("dropdown cursor should be reset to 0, got %d", m.state.DropdownCursor)
	}
}

func TestApplyPreset_SetsProviderFields(t *testing.T) {
	m := newTestModel()
	m.state.NewProviderModels = ""

	m.applyPreset("bigmodel")

	preset := ProviderPresets["bigmodel"]
	if m.state.NewProviderName != "bigmodel" {
		t.Errorf("name = %q, want %q", m.state.NewProviderName, "bigmodel")
	}
	if m.state.NewProviderBaseURL != preset.BaseURL {
		t.Errorf("baseURL = %q, want %q", m.state.NewProviderBaseURL, preset.BaseURL)
	}
	if m.state.NewProviderTransformer != preset.Transformer {
		t.Errorf("transformer = %q, want %q", m.state.NewProviderTransformer, preset.Transformer)
	}
}

func TestApplyPreset_UnknownPresetIsNoOp(t *testing.T) {
	m := newTestModel()
	originalName := "custom-name"
	m.state.NewProviderName = originalName
	m.state.NewProviderModels = ""

	m.applyPreset("nonexistent-preset")

	if m.state.NewProviderName != originalName {
		t.Error("unknown preset should not modify state")
	}
}

// ---------------------------------------------------------------------------
// Tab key behavior — model dropdown
// ---------------------------------------------------------------------------

func TestTab_HidesModelDropdownWhenLeavingField(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 2

	msg := tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)

	// Tab from field 2 → wraps to 0 (maxFields=3, (2+1)%3=0)
	if m.state.ShowModelDropdown {
		t.Error("model dropdown should be hidden when tabbing away from field 2")
	}
	if m.state.ModelDropdownCursor != 0 {
		t.Errorf("model dropdown cursor should be reset, got %d", m.state.ModelDropdownCursor)
	}
}

func TestTab_KeepsModelDropdownOnModelsField(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 1 // Tab will move to field 2
	m.state.ShowModelDropdown = false

	msg := tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)

	// Now focusedField should be 2 — model dropdown state should not be force-closed
	// (it wasn't open, but the Tab handler should NOT hide it when arriving at field 2)
	if m.focusedField != 2 {
		t.Errorf("expected focusedField=2, got %d", m.focusedField)
	}
	// The dropdown should NOT be hidden when arriving at field 2
	// (Tab only hides when leaving, not when arriving)
}

// ---------------------------------------------------------------------------
// Escape key behavior — model dropdown
// ---------------------------------------------------------------------------

func TestEscape_ClosesModelDropdownFirst(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 3

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.ShowModelDropdown {
		t.Error("first Escape should close model dropdown")
	}
	if m.state.ModelDropdownCursor != 0 {
		t.Errorf("cursor should be reset, got %d", m.state.ModelDropdownCursor)
	}
	// Should still be on the same screen
	if m.state.CurrentScreen != ScreenAddProvider1 {
		t.Errorf("should still be on AddProvider1, got %d", m.state.CurrentScreen)
	}
}

func TestEscape_SecondEscapeResetsAddProvider(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "test-provider"
	m.state.NewProviderModels = "some-model"

	// First Escape — no model dropdown open, should go to providers
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenProviders {
		t.Errorf("expected ScreenProviders, got %d", m.state.CurrentScreen)
	}
}

// ---------------------------------------------------------------------------
// Enter key in model dropdown
// ---------------------------------------------------------------------------

func TestEnter_InsertsModelFromDropdown(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "anthropic"
	m.state.NewProviderModels = ""
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	// The first model from anthropic preset should be inserted
	if m.state.NewProviderModels == "" {
		t.Error("expected a model to be inserted")
	}
	if !m.state.ShowModelDropdown {
		t.Error("model dropdown should remain open after selection")
	}
}

func TestEnter_ClosesDropdownWhenNoMatches(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "nonexistent"
	m.state.NewProviderModels = "some-prefix"
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.ShowModelDropdown {
		t.Error("dropdown should close when no matches")
	}
}

// ---------------------------------------------------------------------------
// Up/Down navigation in model dropdown
// ---------------------------------------------------------------------------

func TestNavigation_ModelDropdownUp(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "anthropic"
	m.state.NewProviderModels = ""
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 1

	msg := tea.KeyMsg{Type: tea.KeyUp}
	_, _ = m.Update(msg)

	// (1-1+4)%4 = 0 — wraps from index 1 to 0
	if m.state.ModelDropdownCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.state.ModelDropdownCursor)
	}
}

func TestNavigation_ModelDropdownUp_WrapsFromZero(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "anthropic"
	m.state.NewProviderModels = ""
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyUp}
	_, _ = m.Update(msg)

	// (0-1+4)%4 = 3 — wraps from index 0 to last item
	want := len(ProviderPresets["anthropic"].Models) - 1
	if m.state.ModelDropdownCursor != want {
		t.Errorf("cursor = %d, want %d", m.state.ModelDropdownCursor, want)
	}
}

func TestNavigation_ModelDropdownDown(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderName = "anthropic"
	m.state.NewProviderModels = ""
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyDown}
	m.Update(msg)

	if m.state.ModelDropdownCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.state.ModelDropdownCursor)
	}
}

// ---------------------------------------------------------------------------
// Form input shows model dropdown
// ---------------------------------------------------------------------------

func TestFormInput_OpensModelDropdownOnTyping(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2
	m.state.NewProviderModels = ""
	m.state.ShowModelDropdown = false

	// Simulate typing a character on the models field
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
	m.Update(msg)

	if !m.state.ShowModelDropdown {
		t.Error("model dropdown should open when typing in models field")
	}
	if m.state.ModelDropdownCursor != 0 {
		t.Errorf("cursor should reset to 0, got %d", m.state.ModelDropdownCursor)
	}
}

// ---------------------------------------------------------------------------
// ProviderPresets completeness
// ---------------------------------------------------------------------------

func TestProviderPresets_AllHaveModels(t *testing.T) {
	for name, preset := range ProviderPresets {
		if len(preset.Models) == 0 {
			t.Errorf("preset %q has no models defined", name)
		}
	}
}

func TestProviderPresets_AllHaveRequiredFields(t *testing.T) {
	for name, preset := range ProviderPresets {
		if preset.BaseURL == "" {
			t.Errorf("preset %q has empty BaseURL", name)
		}
		if preset.Transformer == "" {
			t.Errorf("preset %q has empty Transformer", name)
		}
	}
}

func TestProviderPresets_ModelsAreUnique(t *testing.T) {
	for name, preset := range ProviderPresets {
		seen := make(map[string]bool)
		for _, model := range preset.Models {
			lower := strings.ToLower(model)
			if seen[lower] {
				t.Errorf("preset %q has duplicate model %q", name, model)
			}
			seen[lower] = true
		}
	}
}

func TestProviderPresets_AllHaveDistinctKeys(t *testing.T) {
	// Preset keys must be unique (they're map keys, so guaranteed by Go,
	// but this validates the intent that no key is accidentally empty).
	for key := range ProviderPresets {
		if key == "" {
			t.Error("found preset with empty key")
		}
		if key != strings.ToLower(key) {
			t.Errorf("preset key %q should be lowercase", key)
		}
	}
}

// ---------------------------------------------------------------------------
// getPresetMatches — filters existing providers
// ---------------------------------------------------------------------------

func TestGetPresetMatches_ExcludesExistingProviders(t *testing.T) {
	m := newTestModel()
	m.state.Config.Providers["bigmodel"] = config.ProviderConfig{Models: []string{"glm-4"}}

	matches := m.getPresetMatches()

	for _, match := range matches {
		if match == "bigmodel" {
			t.Errorf("preset %q should be excluded because bigmodel already exists in config", match)
		}
	}

	// Verify other presets are still present
	found := false
	for _, match := range matches {
		if match == "anthropic" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected preset 'anthropic' to remain")
	}
}

// ---------------------------------------------------------------------------
// resetAddProviderState clears model dropdown state
// ---------------------------------------------------------------------------

func TestResetAddProviderState_ClearsModelDropdown(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 5
	m.state.ShowDropdown = true
	m.state.DropdownCursor = 3
	m.state.NewProviderName = "test"
	m.state.EditingProvider = true

	m.resetAddProviderState()

	if m.state.ShowModelDropdown {
		t.Error("ShowModelDropdown should be false after reset")
	}
	if m.state.ModelDropdownCursor != 0 {
		t.Errorf("ModelDropdownCursor should be 0, got %d", m.state.ModelDropdownCursor)
	}
	if m.state.ShowDropdown {
		t.Error("ShowDropdown should be false after reset")
	}
	if m.state.DropdownCursor != 0 {
		t.Errorf("DropdownCursor should be 0, got %d", m.state.DropdownCursor)
	}
}

// ---------------------------------------------------------------------------
// Paste support — bracketed paste mode
// ---------------------------------------------------------------------------

func TestInit_ReturnsEnableBracketedPaste(t *testing.T) {
	m := newTestModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() should return a non-nil command (EnableBracketedPaste)")
	}
}

func TestPasteViaBracketedPaste_AddProvider2(t *testing.T) {
	// Bracketed paste mode sends a single KeyMsg with Paste=true and all runes.
	m := newTestModel()
	toScreen(m, ScreenAddProvider2)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("sk-test-api-key")}
	m.Update(msg)

	if m.state.NewProviderAPIKey != "sk-test-api-key" {
		t.Errorf("got %q, want %q", m.state.NewProviderAPIKey, "sk-test-api-key")
	}
}

func TestPasteViaBracketedPaste_ProviderName(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 0

	msg := tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("openrouter")}
	m.Update(msg)

	if m.state.NewProviderName != "openrouter" {
		t.Errorf("got %q, want %q", m.state.NewProviderName, "openrouter")
	}
}

func TestPasteViaBracketedPaste_BaseURL(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 1

	msg := tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("https://api.example.com/v1")}
	m.Update(msg)

	if m.state.NewProviderBaseURL != "https://api.example.com/v1" {
		t.Errorf("got %q, want %q", m.state.NewProviderBaseURL, "https://api.example.com/v1")
	}
}

func TestPasteViaBracketedPaste_Models(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAddProvider1)
	m.focusedField = 2

	msg := tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("claude-sonnet-4.6\nclaude-opus-4.6")}
	m.Update(msg)

	want := "claude-sonnet-4.6\nclaude-opus-4.6"
	if m.state.NewProviderModels != want {
		t.Errorf("got %q, want %q", m.state.NewProviderModels, want)
	}
}

func TestPasteViaBracketedPaste_ServerHost(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenServer)
	m.focusedField = 0

	msg := tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("127.0.0.1")}
	m.Update(msg)

	if m.state.ServerHost != "127.0.0.1" {
		t.Errorf("got %q, want %q", m.state.ServerHost, "127.0.0.1")
	}
}

func TestPasteViaBracketedPaste_ServerPortDigitsOnly(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenServer)
	m.focusedField = 1
	m.state.ServerPort = "" // clear default "8081"

	// Paste "8080abc" — digits should be kept, letters filtered
	msg := tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("8080abc")}
	m.Update(msg)

	if m.state.ServerPort != "8080" {
		t.Errorf("got %q, want %q", m.state.ServerPort, "8080")
	}
}

func TestPasteViaBracketedPaste_RouteEdit(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("custom-route")}
	m.Update(msg)

	if m.state.EditRouteName != "custom-route" {
		t.Errorf("got %q, want %q", m.state.EditRouteName, "custom-route")
	}
}

// ---------------------------------------------------------------------------
// Edit Route — chain editing
// ---------------------------------------------------------------------------

func TestEditRoute_AddChainItem(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = nil
	m.state.EditRouteChainCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	m.Update(msg)

	if len(m.state.EditRouteChain) != 1 {
		t.Fatalf("expected 1 chain item, got %d", len(m.state.EditRouteChain))
	}
	if !m.state.ShowDropdown {
		t.Error("provider dropdown should open after adding chain item")
	}
	if m.state.EditRouteChain[0].Provider != "" || m.state.EditRouteChain[0].Model != "" {
		t.Error("new chain item should have empty provider and model")
	}
}

func TestEditRoute_RemoveChainItem(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
		{Provider: "bigmodel", Model: "glm-4.7"},
	}
	m.state.EditRouteChainCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	m.Update(msg)

	if len(m.state.EditRouteChain) != 1 {
		t.Fatalf("expected 1 chain item after delete, got %d", len(m.state.EditRouteChain))
	}
	if m.state.EditRouteChain[0].Provider != "bigmodel" {
		t.Errorf("expected remaining item to be bigmodel, got %s", m.state.EditRouteChain[0].Provider)
	}
}

func TestEditRoute_RemoveLastItemClampsCursor(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
	}
	m.state.EditRouteChainCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	m.Update(msg)

	if len(m.state.EditRouteChain) != 0 {
		t.Fatalf("expected 0 chain items, got %d", len(m.state.EditRouteChain))
	}
}

func TestEditRoute_NavigationWithinChain(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
		{Provider: "bigmodel", Model: "glm-4.7"},
		{Provider: "gemini", Model: "gemini-2.5-pro"},
	}
	m.state.EditRouteChainCursor = 0

	// Down
	msg := tea.KeyMsg{Type: tea.KeyDown}
	m.Update(msg)
	if m.state.EditRouteChainCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.state.EditRouteChainCursor)
	}

	// Down again
	m.Update(msg)
	if m.state.EditRouteChainCursor != 2 {
		t.Errorf("cursor = %d, want 2", m.state.EditRouteChainCursor)
	}

	// Wrap around
	m.Update(msg)
	if m.state.EditRouteChainCursor != 0 {
		t.Errorf("cursor = %d, want 0 (wrap)", m.state.EditRouteChainCursor)
	}

	// Up (wraps to last)
	msg = tea.KeyMsg{Type: tea.KeyUp}
	m.Update(msg)
	if m.state.EditRouteChainCursor != 2 {
		t.Errorf("cursor = %d, want 2 (wrap up)", m.state.EditRouteChainCursor)
	}
}

func TestEditRoute_TabSwitchesFields(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 0

	msg := tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)

	if m.focusedField != 1 {
		t.Errorf("focusedField = %d, want 1", m.focusedField)
	}

	// Tab again wraps to 0
	m.Update(msg)
	if m.focusedField != 0 {
		t.Errorf("focusedField = %d, want 0 (wrap)", m.focusedField)
	}
}

func TestEditRoute_TabHidesDropdowns(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.ShowDropdown = true
	m.state.ShowModelDropdown = true

	msg := tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)

	if m.state.ShowDropdown {
		t.Error("provider dropdown should be hidden after tab")
	}
	if m.state.ShowModelDropdown {
		t.Error("model dropdown should be hidden after tab")
	}
}

func TestEditRoute_EscapeClosesProviderDropdown(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.state.ShowDropdown = true

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.ShowDropdown {
		t.Error("provider dropdown should be closed after escape")
	}
	if m.state.CurrentScreen != ScreenEditRoute {
		t.Error("should still be on ScreenEditRoute")
	}
}

func TestEditRoute_EscapeClosesModelDropdown(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.state.ShowModelDropdown = true

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.ShowModelDropdown {
		t.Error("model dropdown should be closed after escape")
	}
	if m.state.CurrentScreen != ScreenEditRoute {
		t.Error("should still be on ScreenEditRoute")
	}
}

func TestEditRoute_EscapeNavigatesBack(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenRoutes {
		t.Errorf("expected ScreenRoutes, got %d", m.state.CurrentScreen)
	}
}

func TestEditRoute_EnterOnChainSavesRoute(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteName = "test-route"
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
	}
	m.state.EditRouteChainCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenRoutes {
		t.Errorf("expected ScreenRoutes after save, got %d", m.state.CurrentScreen)
	}
	route := m.state.Config.Router.Routes["test-route"]
	if route != "openrouter:claude-sonnet-4" {
		t.Errorf("route = %q, want openrouter:claude-sonnet-4", route)
	}
}

func TestEditRoute_SaveRouteSkipsIncompleteEntries(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1 // Use chain field to save (Enter on field 0 now opens dropdown)
	m.state.EditRouteName = "test-route"
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
		{Provider: "", Model: ""}, // incomplete — should be skipped
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenRoutes {
		t.Errorf("expected ScreenRoutes after save, got %d", m.state.CurrentScreen)
	}
	route := m.state.Config.Router.Routes["test-route"]
	if route != "openrouter:claude-sonnet-4" {
		t.Errorf("route = %q, want only complete entries", route)
	}
}

func TestEditRoute_SaveRequiresName(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1 // Use chain field to save (Enter on field 0 now opens dropdown)
	m.state.EditRouteName = ""
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen == ScreenRoutes {
		t.Error("should not navigate away without route name")
	}
	if m.state.ErrorMessage == "" {
		t.Error("should have error message")
	}
}

func TestEditRoute_EnterFromRoutesResetsState(t *testing.T) {
	m := newTestModel()
	m.state.Config.Router.Routes["default"] = "openrouter:claude-sonnet-4;bigmodel:glm-4.7"
	m.initProfileTabs() // Initialize profile tabs before testing
	toScreen(m, ScreenRoutes)
	m.state.RouteCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenEditRoute {
		t.Fatalf("expected ScreenEditRoute, got %d", m.state.CurrentScreen)
	}
	if m.state.EditRouteChainCursor != 0 {
		t.Errorf("chain cursor should be 0, got %d", m.state.EditRouteChainCursor)
	}
	if m.state.ShowDropdown || m.state.ShowModelDropdown {
		t.Error("dropdowns should be closed on entry")
	}
	if len(m.state.EditRouteChain) != 2 {
		t.Fatalf("expected 2 chain items, got %d", len(m.state.EditRouteChain))
	}
}

func TestEditRoute_AddKeyFromRoutesResetsState(t *testing.T) {
	m := newTestModel()
	m.state.Config.Router.Routes["default"] = "openrouter:claude-sonnet-4;bigmodel:glm-4.7" // Add a route so we're not on the [+] tab
	m.initProfileTabs() // Initialize profile tabs before testing
	toScreen(m, ScreenRoutes)
	m.state.ProfileTabIndex = 0 // Ensure we're on the legacy tab

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenEditRoute {
		t.Fatalf("expected ScreenEditRoute, got %d", m.state.CurrentScreen)
	}
	if m.state.EditRouteChainCursor != 0 {
		t.Errorf("chain cursor should be 0, got %d", m.state.EditRouteChainCursor)
	}
	if m.state.EditRouteName != "" {
		t.Errorf("route name should be empty, got %q", m.state.EditRouteName)
	}
}

func TestEditRoute_GetMaxFields(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)

	if m.getMaxFields() != 2 {
		t.Errorf("expected 2 fields for ScreenEditRoute, got %d", m.getMaxFields())
	}
}

func TestEditRoute_GetChainProviderList(t *testing.T) {
	m := newTestModel()
	m.state.Config.Providers["bigmodel"] = config.ProviderConfig{Models: []string{"glm-4.7"}}
	m.state.Config.Providers["openrouter"] = config.ProviderConfig{Models: []string{"claude-sonnet-4"}}

	providers := m.getChainProviderList()

	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}
	// Should be sorted
	if providers[0] != "bigmodel" || providers[1] != "openrouter" {
		t.Errorf("expected [bigmodel, openrouter], got %v", providers)
	}
}

func TestEditRoute_GetChainModelList(t *testing.T) {
	m := newTestModel()
	m.state.Config.Providers["openrouter"] = config.ProviderConfig{Models: []string{"claude-sonnet-4", "claude-opus-4"}}
	m.state.EditRouteChain = []config.RouteTarget{{Provider: "openrouter"}}
	m.state.EditRouteChainCursor = 0

	models := m.getChainModelList()

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
}

func TestEditRoute_GetChainModelList_NilForEmptyProvider(t *testing.T) {
	m := newTestModel()
	m.state.EditRouteChain = []config.RouteTarget{{Provider: ""}}
	m.state.EditRouteChainCursor = 0

	models := m.getChainModelList()

	if models != nil {
		t.Errorf("expected nil for empty provider, got %v", models)
	}
}

func TestEditRoute_DropdownNavigation(t *testing.T) {
	m := newTestModel()
	m.state.Config.Providers["bigmodel"] = config.ProviderConfig{Models: []string{"glm-4.7"}}
	m.state.Config.Providers["openrouter"] = config.ProviderConfig{Models: []string{"claude-sonnet-4"}}
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = []config.RouteTarget{{Provider: "", Model: ""}}
	m.state.EditRouteChainCursor = 0
	m.state.ShowDropdown = true
	m.state.DropdownCursor = 0

	// Down
	msg := tea.KeyMsg{Type: tea.KeyDown}
	m.Update(msg)
	if m.state.DropdownCursor != 1 {
		t.Errorf("dropdown cursor = %d, want 1", m.state.DropdownCursor)
	}

	// Up wraps
	msg = tea.KeyMsg{Type: tea.KeyUp}
	m.Update(msg)
	if m.state.DropdownCursor != 0 {
		t.Errorf("dropdown cursor = %d, want 0 (wrap)", m.state.DropdownCursor)
	}
}

// ---------------------------------------------------------------------------
// Append and cancel fixes
// ---------------------------------------------------------------------------

func TestEditRoute_InsertsAfterCursor(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
		{Provider: "bigmodel", Model: "glm-4.7"},
	}
	m.state.EditRouteChainCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	m.Update(msg)

	if len(m.state.EditRouteChain) != 3 {
		t.Fatalf("expected 3 chain items, got %d", len(m.state.EditRouteChain))
	}
	// First item unchanged, new entry inserted after cursor (index 0 → inserted at index 1)
	if m.state.EditRouteChain[0].Provider != "openrouter" || m.state.EditRouteChain[0].Model != "claude-sonnet-4" {
		t.Error("first chain item should be unchanged")
	}
	if m.state.EditRouteChain[1].Provider != "" || m.state.EditRouteChain[1].Model != "" {
		t.Error("new chain item at index 1 should have empty provider and model")
	}
	if m.state.EditRouteChain[2].Provider != "bigmodel" || m.state.EditRouteChain[2].Model != "glm-4.7" {
		t.Error("second chain item should be shifted to index 2")
	}
	// Cursor should point to the new entry
	if m.state.EditRouteChainCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.state.EditRouteChainCursor)
	}
	if !m.state.ShowDropdown {
		t.Error("provider dropdown should open for the new entry")
	}
}

func TestEditRoute_CancelProviderRemovesEmptyEntry(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
	}

	// Press 'a' to add entry (opens provider dropdown)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	m.Update(msg)

	if len(m.state.EditRouteChain) != 2 {
		t.Fatalf("expected 2 items after add, got %d", len(m.state.EditRouteChain))
	}

	// Press Escape to cancel provider selection
	msg = tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.ShowDropdown {
		t.Error("provider dropdown should be closed")
	}
	// The empty entry should be removed
	if len(m.state.EditRouteChain) != 1 {
		t.Fatalf("expected 1 item after cancel, got %d", len(m.state.EditRouteChain))
	}
	if m.state.EditRouteChain[0].Provider != "openrouter" {
		t.Error("original entry should remain intact")
	}
}

func TestEditRoute_CancelModelRemovesPartialEntry(t *testing.T) {
	m := newTestModel()
	m.state.Config.Providers["openrouter"] = config.ProviderConfig{Models: []string{"claude-sonnet-4"}}
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
	}

	// Press 'a' to add entry
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	m.Update(msg)

	// Simulate selecting a provider from the dropdown (sets provider, clears model, opens model dropdown)
	m.state.EditRouteChain[len(m.state.EditRouteChain)-1].Provider = "openrouter"
	m.state.EditRouteChain[len(m.state.EditRouteChain)-1].Model = ""
	m.state.ShowDropdown = false
	m.state.ShowModelDropdown = true

	// Press Escape to cancel model selection
	msg = tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.ShowModelDropdown {
		t.Error("model dropdown should be closed")
	}
	// The partial entry (provider set, model empty) should be removed
	if len(m.state.EditRouteChain) != 1 {
		t.Fatalf("expected 1 item after cancel, got %d", len(m.state.EditRouteChain))
	}
	if m.state.EditRouteChain[0].Provider != "openrouter" || m.state.EditRouteChain[0].Model != "claude-sonnet-4" {
		t.Error("original entry should remain intact")
	}
}

func TestEditRoute_EscapeProviderDropdownKeepsCompletedEntry(t *testing.T) {
	m := newTestModel()
	m.state.Config.Providers["openrouter"] = config.ProviderConfig{Models: []string{"claude-sonnet-4"}}
	toScreen(m, ScreenEditRoute)
	m.focusedField = 1
	m.state.EditRouteChain = []config.RouteTarget{
		{Provider: "openrouter", Model: "claude-sonnet-4"},
	}
	m.state.EditRouteChainCursor = 0

	// Open provider dropdown on the completed entry (simulating re-edit)
	m.state.ShowDropdown = true

	// Press Escape to cancel
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.ShowDropdown {
		t.Error("provider dropdown should be closed")
	}
	// The completed entry should NOT be removed
	if len(m.state.EditRouteChain) != 1 {
		t.Fatalf("expected 1 item, got %d", len(m.state.EditRouteChain))
	}
	if m.state.EditRouteChain[0].Provider != "openrouter" || m.state.EditRouteChain[0].Model != "claude-sonnet-4" {
		t.Error("completed entry should remain intact after escape")
	}
}

// ---------------------------------------------------------------------------
// Main menu renumbering (9 items: 0-8)
// ---------------------------------------------------------------------------

func TestMainMenu_NavigationBounds(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMainMenu)

	// Start at 0
	m.state.ProviderCursor = 0

	// Down to 8
	for i := 0; i < 8; i++ {
		msg := tea.KeyMsg{Type: tea.KeyDown}
		m.Update(msg)
	}
	if m.state.ProviderCursor != 8 {
		t.Errorf("expected cursor 8 after 8 downs, got %d", m.state.ProviderCursor)
	}

	// Down should stay at 8
	msg := tea.KeyMsg{Type: tea.KeyDown}
	m.Update(msg)
	if m.state.ProviderCursor != 8 {
		t.Errorf("expected cursor 8 (clamped), got %d", m.state.ProviderCursor)
	}

	// Up to 0
	for i := 0; i < 8; i++ {
		msg = tea.KeyMsg{Type: tea.KeyUp}
		m.Update(msg)
	}
	if m.state.ProviderCursor != 0 {
		t.Errorf("expected cursor 0 after 8 ups, got %d", m.state.ProviderCursor)
	}

	// Up should stay at 0
	msg = tea.KeyMsg{Type: tea.KeyUp}
	m.Update(msg)
	if m.state.ProviderCursor != 0 {
		t.Errorf("expected cursor 0 (clamped), got %d", m.state.ProviderCursor)
	}
}

func TestMainMenu_EscapeJumpsToSaveQuit(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMainMenu)
	m.state.ProviderCursor = 0

	// First escape jumps to Save & Exit (index 7)
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)
	if m.state.ProviderCursor != 7 {
		t.Errorf("expected cursor 7 (Save & Exit), got %d", m.state.ProviderCursor)
	}

	// Second escape jumps to Quit (index 8)
	msg = tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)
	if m.state.ProviderCursor != 8 {
		t.Errorf("expected cursor 8 (Quit), got %d", m.state.ProviderCursor)
	}
}

func TestMainMenu_NavigateToMultiUser(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMainMenu)

	// Navigate to index 5 (Multi-User)
	m.state.ProviderCursor = 4
	msg := tea.KeyMsg{Type: tea.KeyDown}
	m.Update(msg)
	if m.state.ProviderCursor != 5 {
		t.Errorf("expected cursor 5 (Multi-User), got %d", m.state.ProviderCursor)
	}

	// Enter should navigate to ScreenMultiUser
	msg = tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)
	if m.state.CurrentScreen != ScreenMultiUser {
		t.Errorf("expected ScreenMultiUser, got %d", m.state.CurrentScreen)
	}
}

// ---------------------------------------------------------------------------
// Multi-User screen
// ---------------------------------------------------------------------------

func TestMultiUser_GetMaxFields(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)
	if m.getMaxFields() != 7 {
		t.Errorf("expected 7 fields for ScreenMultiUser, got %d", m.getMaxFields())
	}
}

func TestMultiUser_ToggleCheckbox(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)
	m.focusedField = 0

	if m.state.MultiUserEnabled {
		t.Error("should start disabled")
	}

	// Space toggles on
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
	m.Update(msg)
	if !m.state.MultiUserEnabled {
		t.Error("should be enabled after space")
	}

	// Space toggles off
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
	m.Update(msg)
	if m.state.MultiUserEnabled {
		t.Error("should be disabled after second space")
	}
}

func TestMultiUser_NumericInput(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)
	m.focusedField = 1 // Global Max Concurrency

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}}
	m.Update(msg)
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}}
	m.Update(msg)
	if m.state.MultiUserGlobalMax != "50" {
		t.Errorf("expected '50', got %q", m.state.MultiUserGlobalMax)
	}

	// Letters should be filtered
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	m.Update(msg)
	if m.state.MultiUserGlobalMax != "50" {
		t.Errorf("expected '50' (unchanged), got %q", m.state.MultiUserGlobalMax)
	}

	// Backspace
	msg = tea.KeyMsg{Type: tea.KeyBackspace}
	m.Update(msg)
	if m.state.MultiUserGlobalMax != "5" {
		t.Errorf("expected '5', got %q", m.state.MultiUserGlobalMax)
	}
}

func TestMultiUser_EscapeDiscardsChanges(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)

	// Set original values (snapshot)
	m.state.MultiUserEnabled = false
	m.state.MultiUserGlobalMax = "100"
	m.state.MultiUserWREDMin = "0.5"
	m.state.MultiUserWREDMax = "0.9"
	m.state.MultiUserOrigEnabled = false
	m.state.MultiUserOrigGlobalMax = "100"
	m.state.MultiUserOrigWREDMin = "0.5"
	m.state.MultiUserOrigWREDMax = "0.9"

	// Modify values
	m.state.MultiUserEnabled = true
	m.state.MultiUserGlobalMax = "200"

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenMainMenu {
		t.Errorf("expected ScreenMainMenu, got %d", m.state.CurrentScreen)
	}
	// Values should be restored to snapshot (discarded)
	if m.state.MultiUserEnabled != false {
		t.Error("multi-user should be restored to disabled (cancelled)")
	}
	if m.state.MultiUserGlobalMax != "100" {
		t.Errorf("expected GlobalMax restored to '100', got %q", m.state.MultiUserGlobalMax)
	}
	if m.state.HasChanges {
		t.Error("should not have unsaved changes after cancel")
	}
}

func TestMultiUser_EnterOnManageGroups(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)
	m.focusedField = 4 // Manage Groups button

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenGroups {
		t.Errorf("expected ScreenGroups, got %d", m.state.CurrentScreen)
	}
}

func TestMultiUser_SaveButton(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)
	m.focusedField = 5 // Save button

	m.state.MultiUserEnabled = true
	m.state.MultiUserGlobalMax = "200"
	m.state.MultiUserWREDMin = "0.3"
	m.state.MultiUserWREDMax = "0.8"

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenMainMenu {
		t.Errorf("expected ScreenMainMenu, got %d", m.state.CurrentScreen)
	}
	// Values should be preserved (saved in-memory)
	if !m.state.MultiUserEnabled {
		t.Error("multi-user should remain enabled after save")
	}
	if m.state.MultiUserGlobalMax != "200" {
		t.Errorf("expected GlobalMax=200 after save, got %q", m.state.MultiUserGlobalMax)
	}
	if !m.state.HasChanges {
		t.Error("should have unsaved changes flagged")
	}
}

func TestMultiUser_CancelButton(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)
	m.focusedField = 6 // Cancel button

	// Set original values (snapshot)
	m.state.MultiUserEnabled = false
	m.state.MultiUserGlobalMax = "100"
	m.state.MultiUserWREDMin = "0.5"
	m.state.MultiUserWREDMax = "0.9"
	m.state.MultiUserOrigEnabled = false
	m.state.MultiUserOrigGlobalMax = "100"
	m.state.MultiUserOrigWREDMin = "0.5"
	m.state.MultiUserOrigWREDMax = "0.9"

	// Modify values
	m.state.MultiUserEnabled = true
	m.state.MultiUserGlobalMax = "300"

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenMainMenu {
		t.Errorf("expected ScreenMainMenu, got %d", m.state.CurrentScreen)
	}
	// Values should be restored to snapshot (discarded)
	if m.state.MultiUserEnabled != false {
		t.Error("multi-user should be restored to disabled (cancelled)")
	}
	if m.state.MultiUserGlobalMax != "100" {
		t.Errorf("expected GlobalMax restored to '100', got %q", m.state.MultiUserGlobalMax)
	}
}

func TestMultiUser_TabCyclesThroughAllFields(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)
	m.focusedField = 0

	// Tab through all 7 fields: checkbox, 3 inputs, Manage Groups, Save, Cancel
	for i := 1; i <= 7; i++ {
		msg := tea.KeyMsg{Type: tea.KeyTab}
		m.Update(msg)
	}
	// After 7 tabs from field 0, should be back at field 0
	if m.focusedField != 0 {
		t.Errorf("expected to cycle back to field 0, got %d", m.focusedField)
	}
}

func TestMultiUser_EnterOnFieldDoesNothing(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenMultiUser)
	m.focusedField = 1 // Global Max Concurrency input field

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	// Should remain on the same screen (no navigation for input fields)
	if m.state.CurrentScreen != ScreenMultiUser {
		t.Errorf("expected ScreenMultiUser, got %d", m.state.CurrentScreen)
	}
}

// ---------------------------------------------------------------------------
// Groups screen
// ---------------------------------------------------------------------------

func TestGroups_Navigation(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenGroups)
	m.state.GroupsList = []*auth.GroupInfo{
		{ID: 1, Name: "alpha"},
		{ID: 2, Name: "beta"},
		{ID: 3, Name: "gamma"},
	}
	m.state.GroupsMemberCounts = map[int64]int{1: 2, 2: 5, 3: 1}

	// Down
	msg := tea.KeyMsg{Type: tea.KeyDown}
	m.Update(msg)
	if m.state.GroupsCursor != 1 {
		t.Errorf("expected cursor 1, got %d", m.state.GroupsCursor)
	}

	// Down wraps
	m.Update(msg)
	m.Update(msg)
	if m.state.GroupsCursor != 0 {
		t.Errorf("expected cursor 0 (wrap), got %d", m.state.GroupsCursor)
	}

	// Up wraps
	msg = tea.KeyMsg{Type: tea.KeyUp}
	m.Update(msg)
	if m.state.GroupsCursor != 2 {
		t.Errorf("expected cursor 2 (wrap up), got %d", m.state.GroupsCursor)
	}
}

func TestGroups_EnterOnEmptyNavigatesToCreate(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenGroups)
	m.state.GroupsList = nil

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenCreateGroup {
		t.Errorf("expected ScreenCreateGroup, got %d", m.state.CurrentScreen)
	}
	if m.state.EditingGroupID != 0 {
		t.Error("should be creating, not editing")
	}
}

func TestGroups_EnterOnItemNavigatesToEdit(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenGroups)
	m.state.GroupsList = []*auth.GroupInfo{
		{ID: 1, Name: "alpha", Profile: "standard", PriorityWeight: 0.5, MaxConcurrency: 10},
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenCreateGroup {
		t.Errorf("expected ScreenCreateGroup, got %d", m.state.CurrentScreen)
	}
	if m.state.EditingGroupID != 1 {
		t.Errorf("expected EditingGroupID=1, got %d", m.state.EditingGroupID)
	}
	if m.state.NewGroupName != "alpha" {
		t.Errorf("expected name 'alpha', got %q", m.state.NewGroupName)
	}
	// Should start at field 1 (skip locked name)
	if m.focusedField != 1 {
		t.Errorf("expected focusedField=1, got %d", m.focusedField)
	}
}

func TestGroups_EscapeGoesToMainMenu(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenGroups)

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenMultiUser {
		t.Errorf("expected ScreenMultiUser, got %d", m.state.CurrentScreen)
	}
}

// ---------------------------------------------------------------------------
// Create/Edit Group screen
// ---------------------------------------------------------------------------

func TestCreateGroup_GetMaxFields(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenCreateGroup)
	if m.getMaxFields() != 6 {
		t.Errorf("expected 6 fields for ScreenCreateGroup, got %d", m.getMaxFields())
	}
}

func TestCreateGroup_TabCycles(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenCreateGroup)
	m.focusedField = 0

	msg := tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)
	if m.focusedField != 1 {
		t.Errorf("expected 1, got %d", m.focusedField)
	}

	// Cycle through all 6
	for i := 0; i < 5; i++ {
		m.Update(msg)
	}
	// Should wrap back to 0
	if m.focusedField != 0 {
		t.Errorf("expected 0 (wrap), got %d", m.focusedField)
	}
}

func TestCreateGroup_NameInput(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenCreateGroup)
	m.focusedField = 0

	for _, r := range []rune{'d', 'e', 'v', 's'} {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		m.Update(msg)
	}
	if m.state.NewGroupName != "devs" {
		t.Errorf("expected 'devs', got %q", m.state.NewGroupName)
	}

	// Backspace
	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	m.Update(msg)
	if m.state.NewGroupName != "dev" {
		t.Errorf("expected 'dev', got %q", m.state.NewGroupName)
	}
}

func TestCreateGroup_EditModeNameLocked(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenCreateGroup)
	m.state.EditingGroupID = 1
	m.state.NewGroupName = "locked-name"
	m.focusedField = 0

	// Typing should have no effect
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	m.Update(msg)
	if m.state.NewGroupName != "locked-name" {
		t.Errorf("name should be locked, got %q", m.state.NewGroupName)
	}
}

func TestCreateGroup_CancelButton(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenCreateGroup)
	m.focusedField = 5 // Cancel button

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenGroups {
		t.Errorf("expected ScreenGroups, got %d", m.state.CurrentScreen)
	}
}

func TestCreateGroup_EscapeGoesToGroups(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenCreateGroup)

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.CurrentScreen != ScreenGroups {
		t.Errorf("expected ScreenGroups, got %d", m.state.CurrentScreen)
	}
}

func TestCreateGroup_TabHidesDropdown(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenCreateGroup)
	m.focusedField = 1
	m.state.ShowGroupProfileDropdown = true
	m.state.NewGroupProfileDropdownCursor = 2

	msg := tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)

	if m.state.ShowGroupProfileDropdown {
		t.Error("dropdown should be hidden when tabbing away")
	}
	if m.state.NewGroupProfileDropdownCursor != 0 {
		t.Errorf("dropdown cursor should be 0, got %d", m.state.NewGroupProfileDropdownCursor)
	}
}

func TestCreateGroup_ProfileDropdownNavigation(t *testing.T) {
	m := newTestModel()
	m.state.Config.Router.Profiles = map[string]config.ProfileConfig{
		"alpha": {Name: "Alpha"},
		"beta":  {Name: "Beta"},
		"gamma": {Name: "Gamma"},
	}
	toScreen(m, ScreenCreateGroup)
	m.focusedField = 1
	m.state.NewGroupProfileNames = []string{"alpha", "beta", "gamma"}
	m.state.ShowGroupProfileDropdown = true
	m.state.NewGroupProfileDropdownCursor = 0

	// Down
	msg := tea.KeyMsg{Type: tea.KeyDown}
	m.Update(msg)
	if m.state.NewGroupProfileDropdownCursor != 1 {
		t.Errorf("expected 1, got %d", m.state.NewGroupProfileDropdownCursor)
	}

	// Enter selects
	msg = tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)
	if m.state.NewGroupProfile != "beta" {
		t.Errorf("expected 'beta', got %q", m.state.NewGroupProfile)
	}
	if m.state.ShowGroupProfileDropdown {
		t.Error("dropdown should close after selection")
	}
}

func TestCreateGroup_ProfileDropdownEscapeCloses(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenCreateGroup)
	m.focusedField = 1
	m.state.ShowGroupProfileDropdown = true

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	if m.state.ShowGroupProfileDropdown {
		t.Error("dropdown should close on escape")
	}
	if m.state.CurrentScreen != ScreenCreateGroup {
		t.Error("should stay on create group screen")
	}
}

// ---------------------------------------------------------------------------
// API Keys — keybinding guard clauses
// ---------------------------------------------------------------------------

func TestAPIKeys_DeleteOnActiveKeyIsNoOp(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAPIKeys)
	m.state.KeysList = []*auth.KeyInfo{
		{KeyID: 1, KeyPrefix: "sk-cc-abc", UserName: "alice", IsActive: true},
	}
	m.state.KeysCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	m.Update(msg)

	if m.state.ShowConfirm {
		t.Error("ShowConfirm should not be set for active key with [d]")
	}
}

func TestAPIKeys_RegenerateOnActiveKeyIsNoOp(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAPIKeys)
	m.state.KeysList = []*auth.KeyInfo{
		{KeyID: 1, KeyPrefix: "sk-cc-abc", UserName: "alice", IsActive: true},
	}
	m.state.KeysCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}
	m.Update(msg)

	if m.state.CreatedRawKey != "" {
		t.Error("CreatedRawKey should not be set for active key with [r]")
	}
}

func TestAPIKeys_DeleteOnRevokedKeyShowsConfirm(t *testing.T) {
	m := newTestModel()
	toScreen(m, ScreenAPIKeys)
	m.state.KeysList = []*auth.KeyInfo{
		{KeyID: 2, KeyPrefix: "sk-cc-xyz", UserName: "bob", IsActive: false},
	}
	m.state.KeysCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	m.Update(msg)

	if !m.state.ShowConfirm {
		t.Error("ShowConfirm should be true when pressing [d] on revoked key")
	}
}

