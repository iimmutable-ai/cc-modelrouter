package configwizard

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// newProfileTestModel creates a WizardModel with profiles support.
func newProfileTestModel() *WizardModel {
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Router: config.RouterConfig{
			Routes:   make(map[string]string),
			Profiles: make(map[string]config.ProfileConfig),
		},
	}
	m := NewWizardModel(cfg, "/tmp/test-config.json")
	m.width = 80
	m.height = 40
	return m
}

// ---------------------------------------------------------------------------
// Profile Tab Tests
// ---------------------------------------------------------------------------

func TestInitProfileTabs_NoProfilesNoRoutes(t *testing.T) {
	m := newProfileTestModel()
	m.initProfileTabs()

	// No profiles, no legacy routes = empty tabs (only [+] will be rendered)
	if len(m.state.ProfileTabKeys) != 0 {
		t.Errorf("expected 0 tab keys (no profiles, no routes), got %d", len(m.state.ProfileTabKeys))
	}
	if m.state.ProfileTabIndex != 0 {
		t.Errorf("expected tab index to be 0, got %d", m.state.ProfileTabIndex)
	}
}

func TestInitProfileTabs_LegacyRoutesNoProfiles(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["default"] = "provider:model"
	m.initProfileTabs()

	// Legacy routes auto-migrated to default profile
	if len(m.state.ProfileTabKeys) != 1 {
		t.Errorf("expected 1 tab key (default profile), got %d", len(m.state.ProfileTabKeys))
	}
	if m.state.ProfileTabKeys[0] != "default" {
		t.Errorf("expected first tab to be 'default', got %s", m.state.ProfileTabKeys[0])
	}
	if m.state.ProfileTabIndex != 0 {
		t.Errorf("expected tab index to be 0, got %d", m.state.ProfileTabIndex)
	}
	// Verify routes were migrated
	if m.state.Config.Router.Profiles["default"].Routes["default"] != "provider:model" {
		t.Errorf("expected route to be migrated to profile, got %s", m.state.Config.Router.Profiles["default"].Routes["default"])
	}
	// Verify legacy routes cleared
	if len(m.state.Config.Router.Routes) != 0 {
		t.Errorf("expected legacy routes to be cleared, got %d routes", len(m.state.Config.Router.Routes))
	}
}

func TestInitProfileTabs_ProfilesExistNoLegacy(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default"}
	m.state.Config.Router.Profiles["cost-opt"] = config.ProfileConfig{Name: "Cost Optimized"}

	m.initProfileTabs()

	// Profiles exist = only profile tabs, no legacy tab
	if len(m.state.ProfileTabKeys) != 2 {
		t.Errorf("expected 2 tab keys (profiles only), got %d", len(m.state.ProfileTabKeys))
	}
	// "default" is pinned to first position, remaining tabs sorted alphabetically
	if m.state.ProfileTabKeys[0] != "default" {
		t.Errorf("expected first tab to be 'default', got %s", m.state.ProfileTabKeys[0])
	}
	if m.state.ProfileTabKeys[1] != "cost-opt" {
		t.Errorf("expected second tab to be 'cost-opt', got %s", m.state.ProfileTabKeys[1])
	}
	// Default tab should be selected by default (always index 0)
	if m.state.ProfileTabIndex != 0 {
		t.Errorf("expected tab index to be 0 (default), got %d", m.state.ProfileTabIndex)
	}
}

func TestInitProfileTabs_ProfilesExistWithLegacyRoutesIgnored(t *testing.T) {
	m := newProfileTestModel()
	// Legacy routes exist but profiles also exist = legacy routes ignored
	m.state.Config.Router.Routes["old-route"] = "provider:model"
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default"}

	m.initProfileTabs()

	// Profiles exist = only profile tabs, legacy tab not shown even with legacy routes
	if len(m.state.ProfileTabKeys) != 1 {
		t.Errorf("expected 1 tab key (profile only), got %d", len(m.state.ProfileTabKeys))
	}
	if m.state.ProfileTabKeys[0] != "default" {
		t.Errorf("expected first tab to be 'default', got %s", m.state.ProfileTabKeys[0])
	}
}

func TestGetCurrentRoutes_LegacyMode(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["test"] = "provider:model"
	m.initProfileTabs()

	// After auto-migration, routes come from profile
	routes := m.getCurrentRoutes()

	if len(routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(routes))
	}
	if routes["test"] != "provider:model" {
		t.Errorf("expected route 'test' to be 'provider:model', got %s", routes["test"])
	}
}

func TestGetCurrentRoutes_ProfileMode(t *testing.T) {
	m := newProfileTestModel()
	// Legacy routes exist but profiles also exist - profiles take precedence
	m.state.Config.Router.Routes["legacy-route"] = "provider:model"
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{
		Name: "Standard",
		Routes: map[string]string{"profile-route": "provider2:model2"},
	}
	m.initProfileTabs()

	// In profile mode, getCurrentRoutes returns profile routes
	routes := m.getCurrentRoutes()

	if len(routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(routes))
	}
	if routes["profile-route"] != "provider2:model2" {
		t.Errorf("expected route 'profile-route' to be 'provider2:model2', got %s", routes["profile-route"])
	}
}

func TestSaveCurrentRoutes_LegacyMode(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["existing"] = "provider:old"
	m.initProfileTabs()

	routes := map[string]string{"new-route": "provider:model"}
	m.saveCurrentRoutes(routes)

	// After auto-migration, routes saved to profile (not legacy routes)
	if m.state.Config.Router.Profiles["default"].Routes["new-route"] != "provider:model" {
		t.Errorf("expected route to be saved to profile, got %s", m.state.Config.Router.Profiles["default"].Routes["new-route"])
	}
	if !m.state.HasChanges {
		t.Error("expected HasChanges to be true")
	}
}

func TestSaveCurrentRoutes_ProfileMode(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{
		Name: "Standard",
		Routes: map[string]string{},
	}
	m.initProfileTabs()

	routes := map[string]string{"new-route": "provider:model"}
	m.saveCurrentRoutes(routes)

	// In profile mode, routes saved to profile
	if m.state.Config.Router.Profiles["default"].Routes["new-route"] != "provider:model" {
		t.Errorf("expected profile route to be saved, got %s", m.state.Config.Router.Profiles["default"].Routes["new-route"])
	}
	if !m.state.HasChanges {
		t.Error("expected HasChanges to be true")
	}
}

func TestIsOnAddTab(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["default"] = "provider:model"
	m.initProfileTabs()

	// After auto-migration, on default profile tab (index 0)
	if m.isOnAddTab() {
		t.Error("expected isOnAddTab to be false for index 0")
	}

	// Switch to add tab (last index)
	m.state.ProfileTabIndex = len(m.state.ProfileTabKeys)
	if !m.isOnAddTab() {
		t.Error("expected isOnAddTab to be true when on [+] tab")
	}
}

func TestGenerateProfileKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "Default", "default"},
		{"uppercase", "Cost OPTIMIZED", "cost-optimized"},
		{"special chars", "Premium! Profile", "premium-profile"},
		{"numbers", "Profile 123", "profile-123"},
		{"empty defaults", "", "default"},
		{"only special", "!@#$", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateProfileKey(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestCreateNewProfile(t *testing.T) {
	m := newProfileTestModel()

	key := m.createNewProfile("Cost Optimized", "Use cheaper models")

	if key != "cost-optimized" {
		t.Errorf("expected key to be 'cost-optimized', got '%s'", key)
	}
	if m.state.Config.Router.Profiles["cost-optimized"].Name != "Cost Optimized" {
		t.Errorf("expected profile name to be 'Cost Optimized', got '%s'", m.state.Config.Router.Profiles["cost-optimized"].Name)
	}
	if !m.state.HasChanges {
		t.Error("expected HasChanges to be true")
	}
}

func TestCreateNewProfile_UniqueKey(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["cost-optimized"] = config.ProfileConfig{Name: "Existing"}

	key := m.createNewProfile("Cost Optimized", "Another profile")

	// Should have a unique key with suffix
	if key == "cost-optimized" {
		t.Error("expected unique key, got same key as existing profile")
	}
}

func TestDeleteCurrentProfile(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["test-profile"] = config.ProfileConfig{Name: "Test"}
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default"}
	m.initProfileTabs()

	// Switch to test-profile tab
	for i, key := range m.state.ProfileTabKeys {
		if key == "test-profile" {
			m.state.ProfileTabIndex = i
			break
		}
	}

	errMsg := m.deleteCurrentProfile()

	if errMsg != "" {
		t.Errorf("expected no error, got '%s'", errMsg)
	}
	if m.state.Config.Router.Profiles["test-profile"].Name != "" {
		t.Error("expected profile to be deleted")
	}
	if !m.state.HasChanges {
		t.Error("expected HasChanges to be true")
	}
}

func TestDeleteCurrentProfile_CannotDeleteDefault(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default"}
	m.initProfileTabs()

	// Switch to default tab (should be at index 0 after sort since it's the only profile)
	m.state.ProfileTabIndex = 0

	errMsg := m.deleteCurrentProfile()

	if errMsg == "" {
		t.Error("expected error message for deleting default profile")
	}
}

func TestDeleteCurrentProfile_CannotDeleteAddTab(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default"}
	m.initProfileTabs()

	// Go to [+] tab
	m.state.ProfileTabIndex = len(m.state.ProfileTabKeys)

	errMsg := m.deleteCurrentProfile()

	if errMsg == "" {
		t.Error("expected error message for deleting [+] tab")
	}
}

// ---------------------------------------------------------------------------
// Tab Navigation Tests
// ---------------------------------------------------------------------------

func TestTabNavigation_Left(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["cost-opt"] = config.ProfileConfig{Name: "Cost"}
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default"}
	m.initProfileTabs()
	// cost-opt at index 0, default at index 1 (sorted)
	m.state.ProfileTabIndex = 1 // On default tab

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}} // 'h' for left
	m.state.CurrentScreen = ScreenRoutes
	m.Update(msg)

	if m.state.ProfileTabIndex != 0 {
		t.Errorf("expected tab index 0 after left navigation, got %d", m.state.ProfileTabIndex)
	}
}

func TestTabNavigation_Right(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["cost-opt"] = config.ProfileConfig{Name: "Cost"}
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default"}
	m.initProfileTabs()
	m.state.ProfileTabIndex = 0 // On cost-opt tab (first)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}} // 'l' for right
	m.state.CurrentScreen = ScreenRoutes
	m.Update(msg)

	if m.state.ProfileTabIndex != 1 {
		t.Errorf("expected tab index 1 after right navigation, got %d", m.state.ProfileTabIndex)
	}
}

func TestTabNavigation_WrapsAround(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default"}
	m.initProfileTabs()

	// Start on default tab (index 0), press left - should wrap to [+] tab
	m.state.ProfileTabIndex = 0
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	m.state.CurrentScreen = ScreenRoutes
	m.Update(msg)

	// Should be on [+] tab (last index = 1 for default + add)
	expectedIndex := len(m.state.ProfileTabKeys) // [+] tab
	if m.state.ProfileTabIndex != expectedIndex {
		t.Errorf("expected tab index %d after wrap, got %d", expectedIndex, m.state.ProfileTabIndex)
	}
}

// ---------------------------------------------------------------------------
// Profile Modal Tests
// ---------------------------------------------------------------------------

func TestProfileEditModal_Save(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Old Name", Description: "Old Desc"}
	m.initProfileTabs()
	m.state.CurrentScreen = ScreenRoutes

	// Switch to default tab
	for i, key := range m.state.ProfileTabKeys {
		if key == "default" {
			m.state.ProfileTabIndex = i
			break
		}
	}

	// Set up modal state
	m.state.ShowProfileEditModal = true
	m.state.IsCreatingProfile = false
	m.state.EditProfileName = "New Name"
	m.state.EditProfileDesc = "New Description"
	m.focusedField = 2 // On Save button

	// Press Enter to save
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	if m.state.Config.Router.Profiles["default"].Name != "New Name" {
		t.Errorf("expected profile name to be 'New Name', got '%s'", m.state.Config.Router.Profiles["default"].Name)
	}
	if m.state.Config.Router.Profiles["default"].Description != "New Description" {
		t.Errorf("expected profile description to be 'New Description', got '%s'", m.state.Config.Router.Profiles["default"].Description)
	}
	if m.state.ShowProfileEditModal {
		t.Error("expected modal to be closed")
	}
}

func TestProfileEditModal_Cancel(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Original Name"}
	m.initProfileTabs()
	m.state.CurrentScreen = ScreenRoutes

	// Switch to default tab
	for i, key := range m.state.ProfileTabKeys {
		if key == "default" {
			m.state.ProfileTabIndex = i
			break
		}
	}

	// Set up modal state
	m.state.ShowProfileEditModal = true
	m.state.IsCreatingProfile = false
	m.state.EditProfileName = "Changed Name"
	m.focusedField = 0

	// Press Escape to cancel
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	// Profile should not be changed
	if m.state.Config.Router.Profiles["default"].Name != "Original Name" {
		t.Errorf("expected profile name to remain 'Original Name', got '%s'", m.state.Config.Router.Profiles["default"].Name)
	}
	if m.state.ShowProfileEditModal {
		t.Error("expected modal to be closed")
	}
}

// ---------------------------------------------------------------------------
// Migration Tests (Auto-migration happens in initProfileTabs)
// ---------------------------------------------------------------------------

func TestAutoMigration_LegacyRoutesToProfile(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["default"] = "provider:model"
	m.state.Config.Router.Routes["think"] = "provider:model2"
	m.initProfileTabs()
	m.state.CurrentScreen = ScreenRoutes

	// Should have created default profile with copied routes
	if m.state.Config.Router.Profiles["default"].Routes["default"] != "provider:model" {
		t.Errorf("expected route 'default' to be copied, got '%s'", m.state.Config.Router.Profiles["default"].Routes["default"])
	}
	if m.state.Config.Router.Profiles["default"].Routes["think"] != "provider:model2" {
		t.Errorf("expected route 'think' to be copied, got '%s'", m.state.Config.Router.Profiles["default"].Routes["think"])
	}
	// Legacy routes should be cleared
	if len(m.state.Config.Router.Routes) != 0 {
		t.Errorf("expected legacy routes to be cleared, got %d routes", len(m.state.Config.Router.Routes))
	}
	// Should have HasChanges set
	if !m.state.HasChanges {
		t.Error("expected HasChanges to be true after migration")
	}
}

func TestAutoMigration_ProfileDescription(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["default"] = "provider:model"
	m.initProfileTabs()

	// Check profile has correct description
	if m.state.Config.Router.Profiles["default"].Description != "Auto-migrated from legacy routes" {
		t.Errorf("expected description 'Auto-migrated from legacy routes', got '%s'", m.state.Config.Router.Profiles["default"].Description)
	}
}

// ---------------------------------------------------------------------------
// Create Profile from [+] Tab Tests
// ---------------------------------------------------------------------------

func TestAddTab_EnterOpensModal(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["default"] = "provider:model" // Have routes so no migration modal
	m.state.Config.Router.Profiles["existing"] = config.ProfileConfig{Name: "Existing"} // Add a profile so we're not first
	m.initProfileTabs()

	// Go to [+] tab
	m.state.ProfileTabIndex = len(m.state.ProfileTabKeys)

	// Press Enter
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.state.CurrentScreen = ScreenRoutes
	m.Update(msg)

	// Should navigate to profile creation screen
	if m.state.CurrentScreen != ScreenCreateProfile {
		t.Errorf("expected current screen to be ScreenCreateProfile, got %d", m.state.CurrentScreen)
	}
	if !m.state.IsCreatingProfile {
		t.Error("expected IsCreatingProfile to be true")
	}
}

func TestAddTab_EnterWithExistingProfile(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["default"] = "provider:model"
	m.initProfileTabs()

	// After auto-migration, there's already a default profile
	// Go to [+] tab
	m.state.ProfileTabIndex = len(m.state.ProfileTabKeys)

	// Press Enter
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.state.CurrentScreen = ScreenRoutes
	m.Update(msg)

	// Should navigate to profile creation screen (not migration modal)
	if m.state.CurrentScreen != ScreenCreateProfile {
		t.Errorf("expected current screen to be ScreenCreateProfile, got %d", m.state.CurrentScreen)
	}
	if !m.state.IsCreatingProfile {
		t.Error("expected IsCreatingProfile to be true")
	}
}

// ---------------------------------------------------------------------------
// createDefaultProfile Tests
// ---------------------------------------------------------------------------

func TestCreateDefaultProfile_ClearsLegacyRoutes(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["default"] = "provider:model"
	m.state.Config.Router.Routes["think"] = "provider:model2"

	m.createDefaultProfile(true)

	// Profile should have copied routes
	if m.state.Config.Router.Profiles["default"].Routes["default"] != "provider:model" {
		t.Errorf("expected route 'default' to be copied, got '%s'", m.state.Config.Router.Profiles["default"].Routes["default"])
	}
	// Legacy routes should be cleared
	if len(m.state.Config.Router.Routes) != 0 {
		t.Errorf("expected legacy routes to be cleared, got %d routes", len(m.state.Config.Router.Routes))
	}
}

func TestCreateDefaultProfile_EmptyProfile(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Routes["default"] = "provider:model"

	m.createDefaultProfile(false)

	// Profile should have empty routes
	if len(m.state.Config.Router.Profiles["default"].Routes) != 0 {
		t.Errorf("expected empty routes, got %d routes", len(m.state.Config.Router.Profiles["default"].Routes))
	}
	// Legacy routes should still be cleared
	if len(m.state.Config.Router.Routes) != 0 {
		t.Errorf("expected legacy routes to be cleared, got %d routes", len(m.state.Config.Router.Routes))
	}
}

// ---------------------------------------------------------------------------
// ScreenCreateProfile Tests
// ---------------------------------------------------------------------------

func TestCreateProfileScreen_EscapeReturnsToRoutes(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default", Routes: map[string]string{"default": "provider:model"}}
	m.initProfileTabs()

	// Navigate to create profile screen
	m.state.CurrentScreen = ScreenCreateProfile
	m.state.IsCreatingProfile = true
	m.state.EditProfileName = "Test Profile"
	m.state.EditProfileDesc = "Test Description"
	m.focusedField = 0

	// Press Escape
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	m.Update(msg)

	// Should return to Routes screen
	if m.state.CurrentScreen != ScreenRoutes {
		t.Errorf("expected current screen to be ScreenRoutes, got %d", m.state.CurrentScreen)
	}
	if m.state.IsCreatingProfile {
		t.Error("expected IsCreatingProfile to be false")
	}
	if m.state.EditProfileName != "" {
		t.Errorf("expected EditProfileName to be cleared, got '%s'", m.state.EditProfileName)
	}
}

func TestCreateProfileScreen_EnterCreatesProfile(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default", Routes: map[string]string{"default": "provider:model"}}
	m.initProfileTabs()

	// Navigate to create profile screen with valid name
	m.state.CurrentScreen = ScreenCreateProfile
	m.state.IsCreatingProfile = true
	m.state.EditProfileName = "New Profile"
	m.state.EditProfileDesc = "A new profile"
	m.focusedField = 0

	// Press Enter
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	// Should create profile and return to Routes screen
	if m.state.CurrentScreen != ScreenRoutes {
		t.Errorf("expected current screen to be ScreenRoutes, got %d", m.state.CurrentScreen)
	}
	if _, exists := m.state.Config.Router.Profiles["new-profile"]; !exists {
		t.Error("expected profile 'new-profile' to be created")
	}
	if m.state.IsCreatingProfile {
		t.Error("expected IsCreatingProfile to be false after creation")
	}
}

func TestCreateProfileScreen_EnterEmptyNameShowsError(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default", Routes: map[string]string{"default": "provider:model"}}
	m.initProfileTabs()

	// Navigate to create profile screen with empty name
	m.state.CurrentScreen = ScreenCreateProfile
	m.state.IsCreatingProfile = true
	m.state.EditProfileName = ""
	m.state.EditProfileDesc = ""
	m.focusedField = 0

	// Press Enter
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	// Should stay on create profile screen with error
	if m.state.CurrentScreen != ScreenCreateProfile {
		t.Errorf("expected current screen to remain as ScreenCreateProfile, got %d", m.state.CurrentScreen)
	}
	if m.state.ErrorMessage == "" {
		t.Error("expected error message to be shown")
	}
}

func TestCreateProfileScreen_CancelButtonReturns(t *testing.T) {
	m := newProfileTestModel()
	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{Name: "Default", Routes: map[string]string{"default": "provider:model"}}
	m.initProfileTabs()

	// Navigate to create profile screen with Cancel button focused
	m.state.CurrentScreen = ScreenCreateProfile
	m.state.IsCreatingProfile = true
	m.state.EditProfileName = "Test"
	m.state.EditProfileDesc = ""
	m.focusedField = 3 // Cancel button

	// Press Enter
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	m.Update(msg)

	// Should return to Routes screen without creating profile
	if m.state.CurrentScreen != ScreenRoutes {
		t.Errorf("expected current screen to be ScreenRoutes, got %d", m.state.CurrentScreen)
	}
	if _, exists := m.state.Config.Router.Profiles["test"]; exists {
		t.Error("expected profile 'test' NOT to be created")
	}
}

func TestCreateProfileScreen_TabCyclesFields(t *testing.T) {
	m := newProfileTestModel()
	m.state.CurrentScreen = ScreenCreateProfile
	m.state.IsCreatingProfile = true
	m.focusedField = 0 // Name field

	// Press Tab
	msg := tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)

	if m.focusedField != 1 {
		t.Errorf("expected focusedField to be 1 (description), got %d", m.focusedField)
	}

	// Press Tab again
	m.Update(msg)
	if m.focusedField != 2 {
		t.Errorf("expected focusedField to be 2 (create button), got %d", m.focusedField)
	}

	// Press Tab again
	m.Update(msg)
	if m.focusedField != 3 {
		t.Errorf("expected focusedField to be 3 (cancel button), got %d", m.focusedField)
	}

	// Press Tab again (should cycle back to 0)
	m.Update(msg)
	if m.focusedField != 0 {
		t.Errorf("expected focusedField to cycle back to 0, got %d", m.focusedField)
	}
}