package configwizard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/auth"
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
	"github.com/iimmutable-ai/cc-modelrouter/internal/version"

	"github.com/mattn/go-runewidth"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
)

// WizardKeyMap defines the key bindings for the wizard.
type WizardKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Escape  key.Binding
	Delete  key.Binding
	Tab     key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() WizardKeyMap {
	return WizardKeyMap{
		Up:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "select")),
		Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
		Delete: key.NewBinding(key.WithKeys("del", "d"), key.WithHelp("Del", "delete")),
		Tab:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "next")),
	}
}

// WizardModel is the main Bubble Tea model for the wizard.
type WizardModel struct {
	state    *WizardState
	keys     WizardKeyMap
	width    int
	height   int
	textInput textinput.Model
	focusedField int // 0 = first input, 1 = second, etc.
}

// blankLine renders a blank line spanning the full content width.
func (m *WizardModel) blankLine() string {
	return lipgloss.NewStyle().Width(m.contentWidth()).Render(" ")
}

// divider renders a horizontal rule using box-drawing characters.
func (m *WizardModel) divider() string {
	return lipgloss.NewStyle().
		Foreground(BorderColor).
		Width(m.contentWidth()).
		Render(strings.Repeat("─", m.contentWidth()))
}

// fullWidth pads any content to fill contentWidth.
func (m *WizardModel) fullWidth(content string) string {
	return lipgloss.PlaceHorizontal(m.contentWidth(), lipgloss.Left, content)
}

// inputFieldWidth returns the Width to pass to InputFieldStyle/InputFieldFocusedStyle
// so the total rendered width (content + border + padding) fits within contentWidth.
func (m *WizardModel) inputFieldWidth() int {
	return m.contentWidth() - InputFieldStyle.GetHorizontalFrameSize()
}

// buttonFieldWidth returns the Width to pass to ButtonStyle so it fits within contentWidth.
func (m *WizardModel) buttonFieldWidth() int {
	return m.contentWidth() - ButtonStyle.GetHorizontalFrameSize()
}

// contentWidth returns the available width inside the main container,
// accounting for border, padding, and margins.
func (m *WizardModel) contentWidth() int {
	w := (m.width - 2) - MainContainerStyle.GetHorizontalFrameSize()
	if w < 20 {
		return 20
	}
	return w
}

// footline renders the screen-specific keyboard-hint line.
func (m *WizardModel) footline(help string) string {
	return HelpTextStyle.Width(m.contentWidth()).Render(help)
}

// footlineWithVersion renders help text on the left and the version label
// right-aligned. Used only on the main menu.
func (m *WizardModel) footlineWithVersion(help string) string {
	width := m.contentWidth()
	versionStr := version.String()
	helpWidth := runewidth.StringWidth(help)
	versionWidth := runewidth.StringWidth(versionStr)
	textArea := width - HelpTextStyle.GetHorizontalFrameSize()
	padding := textArea - helpWidth - versionWidth
	if padding < 1 {
		padding = 1
	}
	return HelpTextStyle.Width(width).Render(help + strings.Repeat(" ", padding) + versionStr)
}

// NewWizardModel creates a new wizard model.
func NewWizardModel(cfg *config.Config, configPath string) *WizardModel {
	state := NewWizardState(cfg, configPath)

	return &WizardModel{
		state: state,
		keys:  DefaultKeyMap(),
	}
}

// Init initializes the wizard.
func (m *WizardModel) Init() tea.Cmd {
	m.loadMultiUserSettings()
	// Snapshot for Cancel
	m.state.MultiUserOrigEnabled = m.state.MultiUserEnabled
	m.state.MultiUserOrigGlobalMax = m.state.MultiUserGlobalMax
	m.state.MultiUserOrigWREDMin = m.state.MultiUserWREDMin
	m.state.MultiUserOrigWREDMax = m.state.MultiUserWREDMax
	return tea.EnableBracketedPaste
}

// Update handles messages.
func (m *WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case TestConnectionResultMsg:
		m.state.TestStatus = "done"
		if msg.Success {
			m.state.TestStatus = "success"
			m.state.TestLatency = msg.Latency.Seconds()
		} else {
			m.state.TestStatus = "error"
			m.state.TestError = msg.Error
		}
		return m, nil

	case portTestDoneMsg:
		m.state.PortStatus = msg.status
		m.state.PortTesting = false
		return m, nil
	}

	return m, cmd
}

type TestConnectionResultMsg struct {
	Success   bool
	Latency   time.Duration
	Error     string
}

type portTestDoneMsg struct {
	status string
}

// handleKeyPress handles keyboard input.
func (m *WizardModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		if m.state.HasUnsavedChanges() {
			m.state.ShowConfirm = true
			m.state.ConfirmCursor = 0 // Default to Yes (user initiated quit)
			m.state.ConfirmMessage = "You have unsaved changes. Quit anyway?"
			m.state.ConfirmAction = func() bool {
				return true // Allow quit
			}
			return m, nil
		}
		return m, tea.Quit

	case tea.KeyEscape:
		return m.handleEscape()

	case tea.KeyUp, tea.KeyDown:
		return m.handleNavigation(msg)

	case tea.KeyEnter:
		return m.handleEnter()

	case tea.KeyLeft, tea.KeyRight:
		if m.state.ShowConfirm {
			if msg.Type == tea.KeyLeft {
				m.state.ConfirmCursor = 0 // Yes
			} else {
				m.state.ConfirmCursor = 1 // No
			}
			return m, nil
		}

	case tea.KeyTab:
		if max := m.getMaxFields(); max > 0 {
			m.focusedField = (m.focusedField + 1) % max
			// Skip Name field (0) when editing "default" profile — it's locked
			if m.state.CurrentScreen == ScreenEditProfile && m.state.EditProfileKey == "default" && m.focusedField == 0 {
				m.focusedField = 1
			}
			// Hide dropdowns when tabbing away from their fields
			if m.state.CurrentScreen == ScreenAddProvider1 {
				if m.focusedField != 0 {
					m.state.ShowDropdown = false
					m.state.DropdownCursor = 0
				}
				if m.focusedField != 2 {
					m.state.ShowModelDropdown = false
					m.state.ModelDropdownCursor = 0
				}
				if m.focusedField > 2 {
					m.state.ShowDropdown = false
					m.state.ShowModelDropdown = false
				}
			}
			if m.state.CurrentScreen == ScreenEditRoute {
				m.state.ShowRouteNameDropdown = false
				m.state.RouteNameDropdownCursor = 0
				m.state.ShowDropdown = false
				m.state.DropdownCursor = 0
				m.state.ShowModelDropdown = false
				m.state.ModelDropdownCursor = 0
			}
			if m.state.CurrentScreen == ScreenLogging {
				m.state.ShowLogLevelDropdown = false
				m.state.LogLevelDropdownCursor = 0
				m.state.ShowLogDestDropdown = false
				m.state.LogDestDropdownCursor = 0
			}
			if m.state.CurrentScreen == ScreenCreateGroup {
				m.state.ShowGroupProfileDropdown = false
				m.state.NewGroupProfileDropdownCursor = 0
			}
			if m.state.CurrentScreen == ScreenCreateAPIKey {
				m.state.ShowKeyGroupDropdown = false
				m.state.KeyGroupDropdownCursor = 0
			}
		}
		return m, nil
	}

	// Handle character keys based on screen
	switch m.state.CurrentScreen {
	case ScreenAddProvider1, ScreenAddProvider2:
		return m.handleFormInput(msg)
	case ScreenCreateProfile:
		return m.handleCreateProfileInput(msg)
	case ScreenEditProfile:
		return m.handleEditProfileInput(msg)
	case ScreenServer:
		return m.handleServerInput(msg)
	case ScreenLogging:
		return m.handleLoggingInput(msg)
	case ScreenEditRoute:
		if msg.String() == "a" && m.focusedField == 1 {
			// Insert new chain entry after the currently selected item
			if len(m.state.EditRouteChain) == 0 {
				m.state.EditRouteChain = append(m.state.EditRouteChain, config.RouteTarget{Provider: "", Model: ""})
				m.state.EditRouteChainCursor = 0
			} else {
				pos := m.state.EditRouteChainCursor + 1
				m.state.EditRouteChain = append(
					m.state.EditRouteChain[:pos],
					append([]config.RouteTarget{{Provider: "", Model: ""}}, m.state.EditRouteChain[pos:]...)...,
				)
				m.state.EditRouteChainCursor = pos
			}
			// Open provider dropdown for the new entry
			m.state.ShowDropdown = true
			m.state.DropdownCursor = 0
			m.state.ShowModelDropdown = false
			return m, nil
		}
		if (msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del") && m.focusedField == 1 {
			if len(m.state.EditRouteChain) > 0 {
				m.state.EditRouteChain = append(
					m.state.EditRouteChain[:m.state.EditRouteChainCursor],
					m.state.EditRouteChain[m.state.EditRouteChainCursor+1:]...,
				)
				if m.state.EditRouteChainCursor >= len(m.state.EditRouteChain) && len(m.state.EditRouteChain) > 0 {
					m.state.EditRouteChainCursor = len(m.state.EditRouteChain) - 1
				}
			}
			return m, nil
		}
		return m.handleRouteEditInput(msg)
	case ScreenProviders:
		if msg.String() == "a" {
			m.state.EditRouteName = ""
			m.state.NewProviderName = ""
			m.state.NewProviderBaseURL = ""
			m.state.NewProviderTransformer = ""
			m.state.NewProviderModels = ""
			m.state.NewProviderAPIKey = ""
			m.state.NewProviderDisableKeepAlives = false
			m.state.NewProviderMaxRequestBodyBytes = ""
			m.state.ProviderPreset = ""
			m.state.ShowDropdown = true
			m.state.DropdownCursor = 0
			m.state.ShowModelDropdown = false
			m.state.ModelDropdownCursor = 0
			m.state.AddToShellConfig = true
			m.state.SourceImmediately = true
			m.state.CurrentScreen = ScreenAddProvider1
			m.focusedField = 0
			return m, nil
		}
		if msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del" {
			return m.handleProvidersDelete()
		}
		if msg.String() == "t" || msg.String() == "T" {
			providers := m.getProviderList()
			if len(providers) == 0 {
				m.state.ErrorMessage = "No providers configured to test"
				return m, nil
			}
			selectedProvider := providers[m.state.ProviderCursor]
			pc := m.state.Config.Providers[selectedProvider]
			model := ""
			if len(pc.Models) > 0 {
				model = pc.Models[0]
			}
			m.state.TestProvider = selectedProvider
			m.state.TestModel = model
			m.state.TestStatus = "testing"
			m.state.TestError = ""
			m.state.TestLatency = 0
			m.state.CurrentScreen = ScreenTestConnection
			return m, m.testProviderConnectionAsync()
		}
	case ScreenRoutes:
		// Handle profile edit modal
		if m.state.ShowProfileEditModal {
			if msg.String() == "tab" {
				m.focusedField = (m.focusedField + 1) % 3
				return m, nil
			}
			if msg.String() == "enter" {
				return m.handleProfileEditSave()
			}
			if msg.String() == "esc" {
				m.state.ShowProfileEditModal = false
				m.state.IsCreatingProfile = false
				m.state.EditProfileName = ""
				m.state.EditProfileDesc = ""
				m.focusedField = 0
				return m, nil
			}
			// Handle character input for name/description fields
			if m.focusedField == 0 || m.focusedField == 1 {
				if msg.String() == "backspace" || msg.String() == "delete" {
					if m.focusedField == 0 && len(m.state.EditProfileName) > 0 {
						m.state.EditProfileName = m.state.EditProfileName[:len(m.state.EditProfileName)-1]
					} else if m.focusedField == 1 && len(m.state.EditProfileDesc) > 0 {
						m.state.EditProfileDesc = m.state.EditProfileDesc[:len(m.state.EditProfileDesc)-1]
					}
					return m, nil
				}
				// Add character
				if len(msg.String()) == 1 && msg.String() >= " " {
					if m.focusedField == 0 {
						m.state.EditProfileName += msg.String()
					} else {
						m.state.EditProfileDesc += msg.String()
					}
					return m, nil
				}
			}
			return m, nil
		}

		// Handle migration modal
		if m.state.ShowMigrationModal {
			if msg.String() == "left" || msg.String() == "h" {
				m.state.MigrationChoice = (m.state.MigrationChoice - 1 + 3) % 3
				return m, nil
			}
			if msg.String() == "right" || msg.String() == "l" {
				m.state.MigrationChoice = (m.state.MigrationChoice + 1) % 3
				return m, nil
			}
			if msg.String() == "enter" {
				return m.handleMigrationChoice()
			}
			if msg.String() == "esc" {
				m.state.ShowMigrationModal = false
				return m, nil
			}
			return m, nil
		}

		// Tab navigation
		if msg.String() == "left" || msg.String() == "h" {
			totalTabs := len(m.state.ProfileTabKeys) + 1 // +1 for [+] tab
			m.state.ProfileTabIndex = (m.state.ProfileTabIndex - 1 + totalTabs) % totalTabs
			m.state.RouteCursor = 0
			return m, nil
		}
		if msg.String() == "right" || msg.String() == "l" {
			totalTabs := len(m.state.ProfileTabKeys) + 1
			m.state.ProfileTabIndex = (m.state.ProfileTabIndex + 1) % totalTabs
			m.state.RouteCursor = 0
			return m, nil
		}

		// Add route
		if msg.String() == "a" && !m.isOnAddTab() {
			m.state.EditRouteName = ""
			m.state.EditRouteChain = nil
			m.state.EditRouteChainCursor = 0
			m.state.ShowDropdown = false
			m.state.DropdownCursor = 0
			m.state.ShowModelDropdown = false
			m.state.ModelDropdownCursor = 0
			m.state.ShowRouteNameDropdown = false
			m.state.RouteNameDropdownCursor = 0
			m.state.CurrentScreen = ScreenEditRoute
			return m, nil
		}

		// Delete route
		if (msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del") && !m.isOnAddTab() {
			return m.handleRoutesDelete()
		}

		// Edit profile (P key) - switch to full-screen edit view
		if msg.String() == "P" && m.state.ProfileTabIndex > 0 {
			profileKey := m.getCurrentProfileKey()
			if profile, ok := m.state.Config.Router.Profiles[profileKey]; ok {
				m.state.EditProfileKey = profileKey
				m.state.EditProfileName = profile.Name
				m.state.EditProfileDesc = profile.Description
				m.state.IsCreatingProfile = false
				m.state.ShowProfileEditModal = false
				m.state.ErrorMessage = ""
				m.state.CurrentScreen = ScreenEditProfile
				if profileKey == "default" {
					m.focusedField = 1 // Skip locked Name field
				} else {
					m.focusedField = 0
				}
			}
			return m, nil
		}

		// Delete profile (X key)
		if msg.String() == "X" && m.state.ProfileTabIndex > 0 {
			profileKey := m.getCurrentProfileKey()
			if profileKey == "default" {
				m.state.ErrorMessage = "Cannot delete 'default' launch profile"
				return m, nil
			}
			m.state.ShowConfirm = true
			m.state.ConfirmCursor = 1 // Default to No
			m.state.ConfirmMessage = fmt.Sprintf("Delete profile \"%s\"? This cannot be undone.", profileKey)
			m.state.ConfirmAction = func() bool {
				errMsg := m.deleteCurrentProfile()
				if errMsg != "" {
					m.state.ErrorMessage = errMsg
				}
				return false
			}
			return m, nil
		}
	case ScreenAPIKeys:
		if msg.String() == "c" || msg.String() == "C" {
			m.state.NewKeyName = ""
			m.state.NewKeyGroup = ""
			if len(m.state.NewKeyGroups) > 0 {
				m.state.NewKeyGroup = m.state.NewKeyGroups[0].Name
			}
			m.state.ShowKeyGroupDropdown = false
			m.state.KeyGroupDropdownCursor = 0
			m.focusedField = 0
			m.state.CurrentScreen = ScreenCreateAPIKey
			return m, nil
		}
		if msg.String() == "d" || msg.String() == "D" {
			return m.handleAPIKeysDelete()
		}
		if msg.String() == "r" || msg.String() == "R" {
			return m.handleAPIKeysRegenerate()
		}
	case ScreenCreateAPIKey:
		return m.handleCreateAPIKeyInput(msg)
	case ScreenMultiUser:
		return m.handleMultiUserInput(msg)
	case ScreenGroups:
		// Shortcut keys for group management
		if msg.String() == "a" || msg.String() == "A" {
			return m.handleGroupsAdd()
		}
		if msg.String() == "e" || msg.String() == "E" {
			return m.handleGroupsEdit()
		}
		if msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del" {
			return m.handleGroupsDelete()
		}
	case ScreenCreateGroup:
		return m.handleCreateGroupInput(msg)
	}

	return m, nil
}

// handleEscape handles the escape key.
func (m *WizardModel) handleEscape() (tea.Model, tea.Cmd) {
	// If showing confirmation, dismiss it
	if m.state.ShowConfirm {
		m.state.ShowConfirm = false
		m.state.ConfirmMessage = ""
		m.state.ConfirmAction = nil
		m.state.ConfirmCursor = 0
		return m, nil
	}

	switch m.state.CurrentScreen {
	case ScreenMainMenu:
		if m.state.ProviderCursor != 7 {
			m.state.ProviderCursor = 7 // Jump to "Save & Exit"
		} else {
			m.state.ProviderCursor = 8 // Already on Save & Exit, go to "Quit without saving"
		}
		return m, nil

	case ScreenAddProvider1:
		if m.state.ShowDropdown {
			m.state.ShowDropdown = false
			m.state.DropdownCursor = 0
			return m, nil
		}
		if m.state.ShowModelDropdown {
			m.state.ShowModelDropdown = false
			m.state.ModelDropdownCursor = 0
			return m, nil
		}
		// If editing an existing provider, save form changes back to in-memory config
		if m.state.EditingProvider {
			providerName := strings.TrimSpace(m.state.NewProviderName)
			modelsStr := strings.TrimSpace(m.state.NewProviderModels)
			if providerName != "" && modelsStr != "" {
				models := strings.Split(modelsStr, "\n")
				// Filter empty model entries
				var validModels []string
				for _, mdl := range models {
					if strings.TrimSpace(mdl) != "" {
						validModels = append(validModels, strings.TrimSpace(mdl))
					}
				}
				if len(validModels) > 0 {
					apiKey := strings.TrimSpace(m.state.NewProviderAPIKey)
					envVarName := GenerateEnvVarName(providerName)
					m.state.Config.Providers[providerName] = config.ProviderConfig{
						APIKey:      "${" + envVarName + "}",
						BaseURL:     strings.TrimSpace(m.state.NewProviderBaseURL),
						Transformer: strings.TrimSpace(m.state.NewProviderTransformer),
						Models:      validModels,
					}
					m.state.HasChanges = true

					// Track resolved API key
					if apiKey != "" {
						if m.state.ResolvedAPIKeys == nil {
							m.state.ResolvedAPIKeys = make(map[string]string)
						}
						m.state.ResolvedAPIKeys[providerName] = apiKey
					}

					// Shell integration is deferred to "Save & Exit"
				}
			}
		}
		m.resetAddProviderState()
		m.state.CurrentScreen = ScreenProviders

	case ScreenAddProvider2:
		m.state.CurrentScreen = ScreenAddProvider1

	case ScreenLogging:
		if m.state.ShowLogLevelDropdown {
			m.state.ShowLogLevelDropdown = false
			m.state.LogLevelDropdownCursor = 0
			return m, nil
		}
		if m.state.ShowLogDestDropdown {
			m.state.ShowLogDestDropdown = false
			m.state.LogDestDropdownCursor = 0
			return m, nil
		}
		// Sync logging settings to in-memory config
		m.state.Config.Logging.Enabled = m.state.LoggingEnabled
		m.state.Config.Logging.Level = m.state.LoggingLevel
		m.state.Config.Logging.Destination = m.state.LoggingDestination
		m.state.Config.Logging.FilePath = m.state.LoggingFilePath
		m.state.HasChanges = true
		m.state.PortStatus = ""
		m.state.ProviderCursor = m.state.MainMenuCursor
		m.state.CurrentScreen = ScreenMainMenu

	case ScreenServer:
		// Sync server settings to in-memory config (same validation as handleServerSave)
		host := strings.TrimSpace(m.state.ServerHost)
		portStr := strings.TrimSpace(m.state.ServerPort)
		if host != "" {
			if port, err := strconv.Atoi(portStr); err == nil && port >= 1024 && port <= 65535 {
				m.state.Config.Server.Host = host
				m.state.Config.Server.Port = port
				m.state.HasChanges = true
			}
		}
		if retries, err := strconv.Atoi(strings.TrimSpace(m.state.ServerMaxRetries)); err == nil && retries >= 0 {
			m.state.Config.Router.MaxRetries = retries
			m.state.HasChanges = true
		}
		if delay := strings.TrimSpace(m.state.ServerRetryDelay); delay != "" {
			if _, err := time.ParseDuration(delay); err == nil {
				m.state.Config.Router.RetryDelay = delay
				m.state.HasChanges = true
			}
		}
		if v := strings.TrimSpace(m.state.ServerAutoRestartIdle); v != "" {
			if _, err := time.ParseDuration(v); err == nil {
				m.state.Config.Server.AutoRestartIdle = v
				m.state.HasChanges = true
			}
		} else {
			m.state.Config.Server.AutoRestartIdle = ""
		}
		if v := strings.TrimSpace(m.state.ServerAutoRestartWindow); v != "" {
			parts := strings.Split(v, "-")
			if len(parts) == 2 && timeParseHHMM(parts[0]) && timeParseHHMM(parts[1]) {
				m.state.Config.Server.AutoRestartWindow = v
				m.state.HasChanges = true
			}
		} else {
			m.state.Config.Server.AutoRestartWindow = ""
		}
		if v := strings.TrimSpace(m.state.ServerAutoRestartTimezone); v != "" {
			if _, err := time.LoadLocation(v); err == nil {
				m.state.Config.Server.AutoRestartTimezone = v
				m.state.HasChanges = true
			}
		} else {
			m.state.Config.Server.AutoRestartTimezone = ""
		}
		if v := strings.TrimSpace(m.state.ServerAutoRestartBackoffMax); v != "" {
			if _, err := time.ParseDuration(v); err == nil {
				m.state.Config.Server.AutoRestartBackoffMax = v
				m.state.HasChanges = true
			}
		} else {
			m.state.Config.Server.AutoRestartBackoffMax = ""
		}
		m.state.PortStatus = ""
		m.state.ProviderCursor = m.state.MainMenuCursor
		m.state.CurrentScreen = ScreenMainMenu

	case ScreenProviders, ScreenRoutes, ScreenViewConfig:
		// Handle profile edit modal first
		if m.state.ShowProfileEditModal {
			m.state.ShowProfileEditModal = false
			m.state.IsCreatingProfile = false
			m.state.EditProfileName = ""
			m.state.EditProfileDesc = ""
			m.focusedField = 0
			return m, nil
		}
		// Handle migration modal
		if m.state.ShowMigrationModal {
			m.state.ShowMigrationModal = false
			return m, nil
		}
		m.state.PortStatus = ""
		m.state.ProviderCursor = m.state.MainMenuCursor
		m.state.CurrentScreen = ScreenMainMenu

	case ScreenCreateProfile:
		// Cancel profile creation, return to Routes screen
		m.state.IsCreatingProfile = false
		m.state.EditProfileName = ""
		m.state.EditProfileDesc = ""
		m.state.ErrorMessage = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenRoutes
		m.state.ProfileTabIndex = 0 // Return to first profile tab
		return m, nil

	case ScreenEditProfile:
		// Cancel profile editing, return to Routes screen
		m.state.EditProfileKey = ""
		m.state.EditProfileName = ""
		m.state.EditProfileDesc = ""
		m.state.ErrorMessage = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenRoutes
		return m, nil

	case ScreenEditRoute:
		if m.state.ShowRouteNameDropdown {
			m.state.ShowRouteNameDropdown = false
			m.state.RouteNameDropdownCursor = 0
			return m, nil
		}
		if m.state.ShowDropdown {
			// Cancelled before selecting a provider — remove the empty entry
			if m.state.EditRouteChainCursor < len(m.state.EditRouteChain) {
				cursor := m.state.EditRouteChainCursor
				if m.state.EditRouteChain[cursor].Provider == "" && m.state.EditRouteChain[cursor].Model == "" {
					m.state.EditRouteChain = append(
						m.state.EditRouteChain[:cursor],
						m.state.EditRouteChain[cursor+1:]...,
					)
					if m.state.EditRouteChainCursor >= len(m.state.EditRouteChain) && len(m.state.EditRouteChain) > 0 {
						m.state.EditRouteChainCursor = len(m.state.EditRouteChain) - 1
					}
				}
			}
			m.state.ShowDropdown = false
			m.state.DropdownCursor = 0
			return m, nil
		}
		if m.state.ShowModelDropdown {
			// Cancelled after selecting a provider but before selecting a model — remove partial entry
			if m.state.EditRouteChainCursor < len(m.state.EditRouteChain) {
				cursor := m.state.EditRouteChainCursor
				if m.state.EditRouteChain[cursor].Model == "" {
					m.state.EditRouteChain = append(
						m.state.EditRouteChain[:cursor],
						m.state.EditRouteChain[cursor+1:]...,
					)
					if m.state.EditRouteChainCursor >= len(m.state.EditRouteChain) && len(m.state.EditRouteChain) > 0 {
						m.state.EditRouteChainCursor = len(m.state.EditRouteChain) - 1
					}
				}
			}
			m.state.ShowModelDropdown = false
			m.state.ModelDropdownCursor = 0
			return m, nil
		}
		// Save draft chain back to config before navigating away
		routeName := strings.TrimSpace(m.state.EditRouteName)
		if routeName != "" {
			var chainParts []string
			for _, target := range m.state.EditRouteChain {
				if target.Provider != "" && target.Model != "" {
					chainParts = append(chainParts, fmt.Sprintf("%s:%s", target.Provider, target.Model))
				}
			}
			if len(chainParts) > 0 {
				m.state.Config.Router.Routes[routeName] = strings.Join(chainParts, ";")
				m.state.HasChanges = true
			} else {
				delete(m.state.Config.Router.Routes, routeName)
				m.state.HasChanges = true
			}
		}
		m.initProfileTabs()
		m.state.CurrentScreen = ScreenRoutes

	case ScreenTestConnection:
		m.state.CurrentScreen = ScreenProviders

	case ScreenAPIKeys:
		m.state.KeysCursor = 0
		m.state.ProviderCursor = m.state.MainMenuCursor
		m.state.CurrentScreen = ScreenMainMenu

	case ScreenCreateAPIKey:
		if m.state.ShowKeyGroupDropdown {
			m.state.ShowKeyGroupDropdown = false
			m.state.KeyGroupDropdownCursor = 0
			return m, nil
		}
		m.state.NewKeyName = ""
		m.state.NewKeyGroup = ""
		m.state.KeyShowConfirm = false
		m.state.CreatedRawKey = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenAPIKeys
		return m, nil

	case ScreenMultiUser:
		// Cancel — discard changes, restore snapshot
		return m.handleMultiUserCancel()

	case ScreenGroups:
		// Go back to Multi-User screen without discarding pending group ops.
		// Ops are preserved so "Save & Exit" can flush them to SQLite.
		// Discard happens only on Multi-User Cancel/Escape (explicit user intent).
		m.state.CurrentScreen = ScreenMultiUser
		return m, nil

	case ScreenCreateGroup:
		// Close profile dropdown first if open
		if m.state.ShowGroupProfileDropdown {
			m.state.ShowGroupProfileDropdown = false
			m.state.NewGroupProfileDropdownCursor = 0
			return m, nil
		}
		// Clear form, go back to groups list
		m.state.NewGroupName = ""
		m.state.NewGroupProfile = ""
		m.state.NewGroupPriority = ""
		m.state.NewGroupMaxConc = ""
		m.state.ShowGroupProfileDropdown = false
		m.state.NewGroupProfileDropdownCursor = 0
		m.state.EditingGroupID = 0
		m.state.ErrorMessage = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenGroups
		return m, nil

	default:
		m.state.ProviderCursor = m.state.MainMenuCursor
		m.state.CurrentScreen = ScreenMainMenu
	}

	m.state.ErrorMessage = ""
	return m, nil
}

// handleNavigation handles up/down navigation.
func (m *WizardModel) handleNavigation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	isUp := msg.Type == tea.KeyUp || msg.String() == "k"

	// If dropdown is visible on Add Provider screen, navigate dropdown instead
	if m.state.CurrentScreen == ScreenAddProvider1 && m.state.ShowDropdown && m.focusedField == 0 {
		matches := m.getPresetMatches()
		if len(matches) > 0 {
			if isUp {
				m.state.DropdownCursor = (m.state.DropdownCursor - 1 + len(matches)) % len(matches)
			} else {
				m.state.DropdownCursor = (m.state.DropdownCursor + 1) % len(matches)
			}
		}
		return m, nil
	}

	// If model dropdown is visible on Add Provider screen, navigate model dropdown instead
	if m.state.CurrentScreen == ScreenAddProvider1 && m.state.ShowModelDropdown && m.focusedField == 2 {
		matches := m.getModelSuggestions()
		if len(matches) > 0 {
			if isUp {
				m.state.ModelDropdownCursor = (m.state.ModelDropdownCursor - 1 + len(matches)) % len(matches)
			} else {
				m.state.ModelDropdownCursor = (m.state.ModelDropdownCursor + 1) % len(matches)
			}
		}
		return m, nil
	}

	// If logging level dropdown is visible, navigate it instead
	if m.state.CurrentScreen == ScreenLogging && m.state.ShowLogLevelDropdown && m.focusedField == 1 {
		levels := LogLevelOptions
		if len(levels) > 0 {
			if isUp {
				m.state.LogLevelDropdownCursor = (m.state.LogLevelDropdownCursor - 1 + len(levels)) % len(levels)
			} else {
				m.state.LogLevelDropdownCursor = (m.state.LogLevelDropdownCursor + 1) % len(levels)
			}
		}
		return m, nil
	}

	// If logging destination dropdown is visible, navigate it instead
	if m.state.CurrentScreen == ScreenLogging && m.state.ShowLogDestDropdown && m.focusedField == 2 {
		dests := LogDestinationOptions
		if len(dests) > 0 {
			if isUp {
				m.state.LogDestDropdownCursor = (m.state.LogDestDropdownCursor - 1 + len(dests)) % len(dests)
			} else {
				m.state.LogDestDropdownCursor = (m.state.LogDestDropdownCursor + 1) % len(dests)
			}
		}
		return m, nil
	}

	// If route name dropdown is visible on Edit Route screen, navigate it instead
	if m.state.CurrentScreen == ScreenEditRoute && m.state.ShowRouteNameDropdown && m.focusedField == 0 {
		matches := m.getRouteNameDropdownList()
		if len(matches) > 0 {
			if isUp {
				m.state.RouteNameDropdownCursor = (m.state.RouteNameDropdownCursor - 1 + len(matches)) % len(matches)
			} else {
				m.state.RouteNameDropdownCursor = (m.state.RouteNameDropdownCursor + 1) % len(matches)
			}
		}
		return m, nil
	}

	// If dropdown is visible on Edit Route screen (chain list), navigate dropdown instead
	if m.state.CurrentScreen == ScreenEditRoute && m.focusedField == 1 {
		if m.state.ShowDropdown {
			matches := m.getChainProviderList()
			if len(matches) > 0 {
				if isUp {
					m.state.DropdownCursor = (m.state.DropdownCursor - 1 + len(matches)) % len(matches)
				} else {
					m.state.DropdownCursor = (m.state.DropdownCursor + 1) % len(matches)
				}
			}
			return m, nil
		}
		if m.state.ShowModelDropdown {
			matches := m.getChainModelList()
			if len(matches) > 0 {
				if isUp {
					m.state.ModelDropdownCursor = (m.state.ModelDropdownCursor - 1 + len(matches)) % len(matches)
				} else {
					m.state.ModelDropdownCursor = (m.state.ModelDropdownCursor + 1) % len(matches)
				}
			}
			return m, nil
		}
		// No dropdown open — navigate chain items
		chainLen := len(m.state.EditRouteChain)
		if chainLen > 0 {
			if isUp {
				m.state.EditRouteChainCursor = (m.state.EditRouteChainCursor - 1 + chainLen) % chainLen
			} else {
				m.state.EditRouteChainCursor = (m.state.EditRouteChainCursor + 1) % chainLen
			}
		}
		return m, nil
	}

	switch m.state.CurrentScreen {
	case ScreenMainMenu:
		if isUp && m.state.ProviderCursor > 0 {
			m.state.ProviderCursor--
		} else if !isUp && m.state.ProviderCursor < 8 {
			m.state.ProviderCursor++
		}

	case ScreenAPIKeys:
		keyCount := len(m.state.KeysList)
		if keyCount > 0 {
			// Navigation includes keys + create button row (index = keyCount)
			totalItems := keyCount + 1
			if isUp {
				m.state.KeysCursor = (m.state.KeysCursor - 1 + totalItems) % totalItems
			} else {
				m.state.KeysCursor = (m.state.KeysCursor + 1) % totalItems
			}
		}

	case ScreenGroups:
		groupCount := len(m.state.GroupsList)
		if groupCount > 0 {
			if isUp {
				m.state.GroupsCursor = (m.state.GroupsCursor - 1 + groupCount) % groupCount
			} else {
				m.state.GroupsCursor = (m.state.GroupsCursor + 1) % groupCount
			}
		}

	case ScreenCreateGroup:
		if m.state.ShowGroupProfileDropdown {
			profileNames := m.state.NewGroupProfileNames
			if len(profileNames) > 0 {
				if isUp {
					m.state.NewGroupProfileDropdownCursor = (m.state.NewGroupProfileDropdownCursor - 1 + len(profileNames)) % len(profileNames)
				} else {
					m.state.NewGroupProfileDropdownCursor = (m.state.NewGroupProfileDropdownCursor + 1) % len(profileNames)
				}
			}
			return m, nil
		}

	case ScreenCreateAPIKey:
		if m.state.ShowKeyGroupDropdown {
			groups := m.state.NewKeyGroups
			if len(groups) > 0 {
				if isUp {
					m.state.KeyGroupDropdownCursor = (m.state.KeyGroupDropdownCursor - 1 + len(groups)) % len(groups)
				} else {
					m.state.KeyGroupDropdownCursor = (m.state.KeyGroupDropdownCursor + 1) % len(groups)
				}
			}
			return m, nil
		}

	case ScreenProviders:
		providerCount := len(m.state.Config.Providers)
		if providerCount > 0 {
			if isUp {
				m.state.ProviderCursor = (m.state.ProviderCursor - 1 + providerCount) % providerCount
			} else if !isUp {
				m.state.ProviderCursor = (m.state.ProviderCursor + 1) % providerCount
			}
		}

	case ScreenRoutes:
		// Don't navigate routes when on [+] tab
		if m.isOnAddTab() {
			return m, nil
		}
		routes := m.getRouteList()
		routeCount := len(routes)
		if routeCount > 0 {
			if isUp {
				m.state.RouteCursor = (m.state.RouteCursor - 1 + routeCount) % routeCount
			} else {
				m.state.RouteCursor = (m.state.RouteCursor + 1) % routeCount
			}
		}
	}

	return m, nil
}

// handleEnter handles the enter key.
func (m *WizardModel) handleEnter() (tea.Model, tea.Cmd) {
	// If showing confirmation, handle it
	if m.state.ShowConfirm {
		if m.state.ConfirmCursor == 0 && m.state.ConfirmAction != nil && m.state.ConfirmAction() {
			return m, tea.Quit
		}
		m.state.ShowConfirm = false
		m.state.ConfirmMessage = ""
		m.state.ConfirmAction = nil
		return m, nil
	}

	switch m.state.CurrentScreen {
	case ScreenMainMenu:
		return m.handleMainMenuEnter()

	case ScreenProviders:
		return m.handleProvidersEnter()

	case ScreenRoutes:
		// Handle profile edit modal first
		if m.state.ShowProfileEditModal {
			return m.handleProfileEditSave()
		}
		// Handle migration modal
		if m.state.ShowMigrationModal {
			return m.handleMigrationChoice()
		}
		return m.handleRoutesEnter()

	case ScreenCreateProfile:
		return m.handleCreateProfileEnter()

	case ScreenEditProfile:
		return m.handleEditProfileEnter()

	case ScreenServer:
		return m.handleServerSave()

	case ScreenLogging:
		// If log level dropdown is open, select the item
		if m.state.ShowLogLevelDropdown && m.focusedField == 1 {
			if m.state.LogLevelDropdownCursor < len(LogLevelOptions) {
				m.state.LoggingLevel = LogLevelOptions[m.state.LogLevelDropdownCursor]
			}
			m.state.ShowLogLevelDropdown = false
			m.state.LogLevelDropdownCursor = 0
			return m, nil
		}
		// If log destination dropdown is open, select the item
		if m.state.ShowLogDestDropdown && m.focusedField == 2 {
			if m.state.LogDestDropdownCursor < len(LogDestinationOptions) {
				m.state.LoggingDestination = LogDestinationOptions[m.state.LogDestDropdownCursor]
			}
			m.state.ShowLogDestDropdown = false
			m.state.LogDestDropdownCursor = 0
			return m, nil
		}
		// If focused on level field, open dropdown (only when logging is enabled)
		if m.focusedField == 1 {
			if !m.state.LoggingEnabled {
				return m, nil
			}
			m.state.ShowLogLevelDropdown = true
			// Set cursor to current level
			for i, l := range LogLevelOptions {
				if l == m.state.LoggingLevel {
					m.state.LogLevelDropdownCursor = i
					break
				}
			}
			return m, nil
		}
		// If focused on destination field, open dropdown (only when logging is enabled)
		if m.focusedField == 2 {
			if !m.state.LoggingEnabled {
				return m, nil
			}
			m.state.ShowLogDestDropdown = true
			// Set cursor to current destination
			for i, d := range LogDestinationOptions {
				if d == m.state.LoggingDestination {
					m.state.LogDestDropdownCursor = i
					break
				}
			}
			return m, nil
		}
		return m.handleLoggingSave()

	case ScreenViewConfig:
		// Export config to file
		m.exportConfig()

	case ScreenAPIKeys:
		return m.handleAPIKeysEnter()

	case ScreenCreateAPIKey:
		return m.handleCreateAPIKeyEnter()

	case ScreenMultiUser:
		switch m.focusedField {
		case 4: // Manage Groups button
			if m.state.GroupsSnapshot == nil {
				m.loadGroupsData()
			}
			m.state.GroupsList = m.effectiveGroupsList()
			m.state.GroupsCursor = 0
			m.state.CurrentScreen = ScreenGroups
			return m, nil
		case 5: // Save button
			return m.handleMultiUserSave()
		case 6: // Cancel button
			return m.handleMultiUserCancel()
		}
		// Fields 0-3: input fields, Enter does nothing

	case ScreenGroups:
		return m.handleGroupsEnter()

	case ScreenCreateGroup:
		return m.handleCreateGroupEnter()

	case ScreenAddProvider1:
		// If dropdown is visible and focused on name field, select preset
		if m.state.ShowDropdown && m.focusedField == 0 {
			matches := m.getPresetMatches()
			if len(matches) > 0 && m.state.DropdownCursor < len(matches) {
				m.applyPreset(matches[m.state.DropdownCursor])
			} else {
				m.state.ShowDropdown = false
			}
			return m, nil
		}
		// If model dropdown is visible and focused on models field, select model
		if m.state.ShowModelDropdown && m.focusedField == 2 {
			matches := m.getModelSuggestions()
			if len(matches) > 0 && m.state.ModelDropdownCursor < len(matches) {
				m.insertModelFromDropdown(matches[m.state.ModelDropdownCursor])
			} else {
				m.state.ShowModelDropdown = false
			}
			return m, nil
		}
		// If focused on name field and dropdown is not showing, open it
		if m.focusedField == 0 && !m.state.ShowDropdown {
			m.state.ShowDropdown = true
			m.state.DropdownCursor = 0
			return m, nil
		}
		// If focused on models field and current line has content, add newline instead of navigating
		if m.focusedField == 2 {
			currentLine := m.state.NewProviderModels
			if idx := strings.LastIndex(currentLine, "\n"); idx >= 0 {
				currentLine = currentLine[idx+1:]
			}
			if currentLine != "" {
				m.state.NewProviderModels += "\n"
				m.state.ShowModelDropdown = true
				m.state.ModelDropdownCursor = 0
				return m, nil
			}
		}
		return m.handleAddProvider1Enter()

	case ScreenAddProvider2:
		return m.handleAddProvider2Enter()

	case ScreenEditRoute:
		return m.handleEditRouteEnter()
	}

	return m, nil
}

func (m *WizardModel) handleMainMenuEnter() (tea.Model, tea.Cmd) {
	m.state.MainMenuCursor = m.state.ProviderCursor
	switch m.state.ProviderCursor {
	case 0: // Providers
		m.state.ProviderCursor = 0
		m.state.CurrentScreen = ScreenProviders
	case 1: // Routes
		m.state.RouteCursor = 0
		m.initProfileTabs()
		m.state.CurrentScreen = ScreenRoutes
	case 2: // Server
		m.state.ServerHost = m.state.Config.Server.Host
		m.state.ServerPort = strconv.Itoa(m.state.Config.Server.Port)
		m.state.ServerMaxRetries = strconv.Itoa(m.state.Config.Router.MaxRetries)
		m.state.ServerRetryDelay = m.state.Config.Router.RetryDelay
		m.state.ServerAutoRestartIdle = m.state.Config.Server.AutoRestartIdle
		m.state.ServerAutoRestartWindow = m.state.Config.Server.AutoRestartWindow
		m.state.ServerAutoRestartTimezone = m.state.Config.Server.AutoRestartTimezone
		m.state.ServerAutoRestartBackoffMax = m.state.Config.Server.AutoRestartBackoffMax
		m.state.PortStatus = ""
		m.state.CurrentScreen = ScreenServer
		return m, m.checkPortAvailability()
	case 3: // Logging
		m.state.LoggingEnabled = m.state.Config.Logging.Enabled
		m.state.LoggingLevel = m.state.Config.Logging.Level
		if m.state.LoggingLevel == "" {
			m.state.LoggingLevel = "info"
		}
		m.state.LoggingDestination = m.state.Config.Logging.Destination
		if m.state.LoggingDestination == "" {
			m.state.LoggingDestination = "stdout"
		}
		m.state.LoggingFilePath = m.state.Config.Logging.FilePath
		m.state.CurrentScreen = ScreenLogging
	case 4: // View Config
		m.state.CurrentScreen = ScreenViewConfig
	case 5: // Multi-User
		m.focusedField = 0
		m.state.CurrentScreen = ScreenMultiUser
	case 6: // API Keys
		m.state.KeysCursor = 0
		m.loadKeysData()
		m.state.CurrentScreen = ScreenAPIKeys
		return m, nil
	case 7: // Save & Exit
		if m.state.HasUnsavedChanges() {
			if err := m.saveConfig(); err != nil {
				m.state.ErrorMessage = fmt.Sprintf("Failed to save: %v", err)
				return m, nil
			}

			// Save multi-user settings to SQLite
			if db, err := openWizardDB(); err == nil {
				ks := auth.NewKeyStore(db)
				globalMax := 0
				if v, err := strconv.Atoi(strings.TrimSpace(m.state.MultiUserGlobalMax)); err == nil && v > 0 {
					globalMax = v
				}
				wredMin := 0.5
				if v, err := strconv.ParseFloat(strings.TrimSpace(m.state.MultiUserWREDMin), 64); err == nil && v > 0 {
					wredMin = v
				}
				wredMax := 0.9
				if v, err := strconv.ParseFloat(strings.TrimSpace(m.state.MultiUserWREDMax), 64); err == nil && v > 0 {
					wredMax = v
				}
				settings := &auth.MultiUserSettings{
					Enabled:       m.state.MultiUserEnabled,
					GlobalMaxConc: globalMax,
					WREDMinDepth:  wredMin,
					WREDMaxDepth:  wredMax,
				}
				if err := ks.UpdateSettings(settings); err != nil {
					m.state.ErrorMessage = fmt.Sprintf("Failed to save multi-user settings: %v", err)
					db.Close()
					return m, nil
				}
				// Flush pending group changes using the same KeyStore
				if len(m.state.GroupsPendingOps) > 0 {
					if err := m.flushGroupChanges(ks); err != nil {
						m.state.ErrorMessage = fmt.Sprintf("Failed to save group changes: %v", err)
						db.Close()
						return m, nil
					}
				}
				db.Close()
			}

			// Sync shell RC file to match current config
			if shellCfg, err := GetShellConfig(); err == nil && len(m.state.ResolvedAPIKeys) > 0 {
				if err := shellCfg.SyncAllShellExports(m.state.ResolvedAPIKeys); err != nil {
					logging.Warnf("failed to sync shell exports: %v", err)
				}
				shellCfg.SourceAllNow(m.state.ResolvedAPIKeys)
				shellCfg.WriteEnvFile(m.state.ResolvedAPIKeys)
			}
			m.state.HasChanges = false
			m.state.OriginalCfg = deepCopyConfig(m.state.Config)
			// Snapshot resolved keys so future edits are detected
			m.state.OriginalResolvedKeys = make(map[string]string, len(m.state.ResolvedAPIKeys))
			for k, v := range m.state.ResolvedAPIKeys {
				m.state.OriginalResolvedKeys[k] = v
			}
		}
		return m, tea.Quit
	case 8: // Quit without saving
		if m.state.HasUnsavedChanges() {
			m.state.ShowConfirm = true
			m.state.ConfirmCursor = 0 // Default to Yes (user initiated quit)
			m.state.ConfirmMessage = "You have unsaved changes. Quit without saving?"
			m.state.ConfirmAction = func() bool {
				return true // Allow quit
			}
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m *WizardModel) handleProvidersEnter() (tea.Model, tea.Cmd) {
	// Get provider at cursor position
	providers := m.getProviderList()
	if m.state.ProviderCursor < len(providers) {
		providerName := providers[m.state.ProviderCursor]
		m.state.EditRouteName = providerName
		if providerCfg, ok := m.state.Config.Providers[providerName]; ok {
			m.state.NewProviderName = providerName
			m.state.NewProviderBaseURL = providerCfg.BaseURL
			m.state.NewProviderTransformer = providerCfg.Transformer
			m.state.NewProviderModels = strings.Join(providerCfg.Models, "\n")
			m.state.NewProviderDisableKeepAlives = providerCfg.DisableKeepAlives
			if providerCfg.MaxRequestBodyBytes > 0 {
				m.state.NewProviderMaxRequestBodyBytes = strconv.FormatInt(providerCfg.MaxRequestBodyBytes, 10)
			}
			expanded := os.ExpandEnv(providerCfg.APIKey)
			if expanded == "" && strings.Contains(providerCfg.APIKey, "${") {
				// Env var not set — show the placeholder so user knows which var is needed
				m.state.NewProviderAPIKey = providerCfg.APIKey
			} else {
				m.state.NewProviderAPIKey = expanded
			}
		}
		m.state.CurrentScreen = ScreenAddProvider1
		m.state.ProviderPreset = "custom"
		m.state.EditingProvider = true
		m.state.AddToShellConfig = true
		m.state.SourceImmediately = true
	}
	return m, nil
}

func (m *WizardModel) handleProvidersDelete() (tea.Model, tea.Cmd) {
	providers := m.getProviderList()
	if len(providers) == 0 || m.state.ProviderCursor >= len(providers) {
		return m, nil
	}

	providerName := providers[m.state.ProviderCursor]
	m.state.ShowConfirm = true
	m.state.ConfirmCursor = 1 // Default to No (safer)
	m.state.ConfirmMessage = fmt.Sprintf("Delete provider \"%s\"?", providerName)
	m.state.ConfirmAction = func() bool {
		delete(m.state.Config.Providers, providerName)
		delete(m.state.ResolvedAPIKeys, providerName)

		// Remove from shell RC file
		if shellCfg, err := GetShellConfig(); err == nil {
			if err := shellCfg.RemoveFromShellConfig(providerName); err != nil {
				logging.Warnf("failed to remove shell config for %s: %v", providerName, err)
			}
		}

		// Clamp cursor
		newProviders := make([]string, 0, len(m.state.Config.Providers))
		for name := range m.state.Config.Providers {
			newProviders = append(newProviders, name)
		}
		sort.Strings(newProviders)
		if m.state.ProviderCursor >= len(newProviders) {
			m.state.ProviderCursor = len(newProviders) - 1
		}

		// Clean up routes that reference the deleted provider
		for routeName, routeStr := range m.state.Config.Router.Routes {
			targets := config.ParseRoute(routeStr)
			remaining := make([]config.RouteTarget, 0, len(targets))
			for _, t := range targets {
				if t.Provider != providerName {
					remaining = append(remaining, t)
				}
			}
			if len(remaining) == 0 {
				delete(m.state.Config.Router.Routes, routeName)
			} else {
				parts := make([]string, 0, len(remaining))
				for _, t := range remaining {
					parts = append(parts, t.Provider+":"+t.Model)
				}
				m.state.Config.Router.Routes[routeName] = strings.Join(parts, ";")
			}
		}

		m.state.HasChanges = true
		return false
	}
	return m, nil
}

func (m *WizardModel) handleRoutesEnter() (tea.Model, tea.Cmd) {
	// Handle [+] tab - create new profile
	if m.isOnAddTab() {
		// Check if we need to show migration modal
		if m.hasLegacyRoutes() && !m.hasProfiles() {
			m.state.ShowMigrationModal = true
			m.state.MigrationChoice = 0 // Default to copy routes
			m.state.EditProfileName = "Default"
			m.state.EditProfileDesc = "Launch profile for router"
			m.state.IsCreatingProfile = true
			return m, nil
		}
		// Navigate to full-screen profile creation screen
		m.state.EditProfileName = ""
		m.state.EditProfileDesc = ""
		m.state.IsCreatingProfile = true
		m.state.ShowProfileEditModal = false // Ensure modal flag is off
		m.state.CurrentScreen = ScreenCreateProfile
		m.focusedField = 0
		return m, nil
	}

	// Edit existing route
	routes := m.getRouteList()
	currentRoutes := m.getCurrentRoutes()
	if m.state.RouteCursor < len(routes) {
		routeName := routes[m.state.RouteCursor]
		m.state.EditRouteName = routeName
		m.state.EditRouteChain = config.ParseRoute(currentRoutes[routeName])
		m.state.EditRouteChainCursor = 0
		m.state.ShowDropdown = false
		m.state.DropdownCursor = 0
		m.state.ShowModelDropdown = false
		m.state.ModelDropdownCursor = 0
		m.state.ShowRouteNameDropdown = false
		m.state.RouteNameDropdownCursor = 0
		m.state.CurrentScreen = ScreenEditRoute
	}
	return m, nil
}

func (m *WizardModel) handleRoutesDelete() (tea.Model, tea.Cmd) {
	if m.isOnAddTab() {
		return m, nil // Can't delete from [+] tab
	}

	routes := m.getRouteList()
	if len(routes) == 0 || m.state.RouteCursor >= len(routes) {
		return m, nil
	}

	routeName := routes[m.state.RouteCursor]
	m.state.ShowConfirm = true
	m.state.ConfirmCursor = 1 // Default to No (safer)
	m.state.ConfirmMessage = fmt.Sprintf("Delete route \"%s\"? This cannot be undone.", routeName)
	m.state.ConfirmAction = func() bool {
		currentRoutes := m.getCurrentRoutes()
		delete(currentRoutes, routeName)
		m.saveCurrentRoutes(currentRoutes)

		// Clamp cursor
		remaining := m.getRouteList()
		if m.state.RouteCursor >= len(remaining) && len(remaining) > 0 {
			m.state.RouteCursor = len(remaining) - 1
		}

		return false
	}
	return m, nil
}

func (m *WizardModel) handleProfileEditSave() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.state.EditProfileName)
	if name == "" {
		m.state.ErrorMessage = "Profile name is required"
		return m, nil
	}

	if m.state.IsCreatingProfile {
		// Create new profile
		key := m.createNewProfile(name, m.state.EditProfileDesc)
		// Reinitialize tabs and switch to new profile
		m.initProfileTabs()
		// Find the new profile tab
		for i, k := range m.state.ProfileTabKeys {
			if k == key {
				m.state.ProfileTabIndex = i
				break
			}
		}
	} else {
		// Update existing profile
		profileKey := m.getCurrentProfileKey()
		if profileKey != "" {
			profile := m.state.Config.Router.Profiles[profileKey]
			profile.Name = name
			profile.Description = m.state.EditProfileDesc
			m.state.Config.Router.Profiles[profileKey] = profile
			m.state.HasChanges = true
		}
	}

	// Close modal
	m.state.ShowProfileEditModal = false
	m.state.IsCreatingProfile = false
	m.state.EditProfileName = ""
	m.state.EditProfileDesc = ""
	m.focusedField = 0
	m.state.ErrorMessage = ""
	return m, nil
}

func (m *WizardModel) handleMigrationChoice() (tea.Model, tea.Cmd) {
	if m.state.MigrationChoice == 2 {
		// Cancel
		m.state.ShowMigrationModal = false
		return m, nil
	}

	// Create default profile
	copyRoutes := m.state.MigrationChoice == 0
	m.createDefaultProfile(copyRoutes)

	// Reinitialize tabs and switch to default profile
	m.initProfileTabs()
	for i, k := range m.state.ProfileTabKeys {
		if k == "default" {
			m.state.ProfileTabIndex = i
			break
		}
	}

	// Close modal
	m.state.ShowMigrationModal = false
	m.state.RouteCursor = 0
	return m, nil
}

func (m *WizardModel) checkPortAvailability() tea.Cmd {
	m.state.PortStatus = ""
	host := strings.TrimSpace(m.state.ServerHost)
	portStr := strings.TrimSpace(m.state.ServerPort)
	if host != "localhost" && host != "127.0.0.1" {
		return nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil
	}
	m.state.PortTesting = true
	return func() tea.Msg {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			return portTestDoneMsg{status: "Port is already in use"}
		}
		listener.Close()
		return portTestDoneMsg{status: "Port availability test PASS"}
	}
}

func (m *WizardModel) testProviderConnectionAsync() tea.Cmd {
	providerName := m.state.TestProvider
	model := m.state.TestModel
	pc := m.state.Config.Providers[providerName]
	return func() tea.Msg {
		result := TestProviderConnection(providerName, pc, model)
		return TestConnectionResultMsg{
			Success: result.Success,
			Latency: result.Latency,
			Error:   result.Error,
		}
	}
}

func (m *WizardModel) handleServerSave() (tea.Model, tea.Cmd) {
	host := strings.TrimSpace(m.state.ServerHost)
	portStr := strings.TrimSpace(m.state.ServerPort)

	if host == "" {
		m.state.ErrorMessage = "Host cannot be empty"
		return m, nil
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1024 || port > 65535 {
		m.state.ErrorMessage = "Port must be between 1024 and 65535"
		return m, nil
	}

	m.state.Config.Server.Host = host
	m.state.Config.Server.Port = port

	// Save retry settings
	if retries, err := strconv.Atoi(strings.TrimSpace(m.state.ServerMaxRetries)); err == nil && retries >= 0 {
		m.state.Config.Router.MaxRetries = retries
	}
	if delay := strings.TrimSpace(m.state.ServerRetryDelay); delay != "" {
		if _, err := time.ParseDuration(delay); err == nil {
			m.state.Config.Router.RetryDelay = delay
		}
	}

	// Save auto-restart settings (non-fatal: invalid values are skipped with a message)
	if v := strings.TrimSpace(m.state.ServerAutoRestartIdle); v != "" {
		if _, err := time.ParseDuration(v); err == nil {
			m.state.Config.Server.AutoRestartIdle = v
		} else {
			m.state.ErrorMessage = "Auto-restart idle is not a valid duration; skipped"
		}
	} else {
		m.state.Config.Server.AutoRestartIdle = ""
	}
	if v := strings.TrimSpace(m.state.ServerAutoRestartWindow); v != "" {
		parts := strings.Split(v, "-")
		if len(parts) == 2 &&
			timeParseHHMM(parts[0]) && timeParseHHMM(parts[1]) {
			m.state.Config.Server.AutoRestartWindow = v
		} else {
			m.state.ErrorMessage = "Auto-restart window must be HH:MM-HH:MM; skipped"
		}
	} else {
		m.state.Config.Server.AutoRestartWindow = ""
	}
	if v := strings.TrimSpace(m.state.ServerAutoRestartTimezone); v != "" {
		if _, err := time.LoadLocation(v); err == nil {
			m.state.Config.Server.AutoRestartTimezone = v
		} else {
			m.state.ErrorMessage = "Auto-restart timezone is not a known IANA zone; skipped"
		}
	} else {
		m.state.Config.Server.AutoRestartTimezone = ""
	}
	if v := strings.TrimSpace(m.state.ServerAutoRestartBackoffMax); v != "" {
		if _, err := time.ParseDuration(v); err == nil {
			m.state.Config.Server.AutoRestartBackoffMax = v
		} else {
			m.state.ErrorMessage = "Auto-restart backoff max is not a valid duration; skipped"
		}
	} else {
		m.state.Config.Server.AutoRestartBackoffMax = ""
	}

	m.state.HasChanges = true
	m.state.CurrentScreen = ScreenMainMenu
	m.state.ErrorMessage = ""
	return m, nil
}

// timeParseHHMM reports whether s parses as a "HH:MM" time-of-day.
func timeParseHHMM(s string) bool {
	_, err := time.Parse("15:04", strings.TrimSpace(s))
	return err == nil
}

func (m *WizardModel) handleLoggingSave() (tea.Model, tea.Cmd) {
	m.state.Config.Logging.Enabled = m.state.LoggingEnabled
	m.state.Config.Logging.Level = m.state.LoggingLevel
	m.state.Config.Logging.Destination = m.state.LoggingDestination
	m.state.Config.Logging.FilePath = m.state.LoggingFilePath
	m.state.HasChanges = true
	m.state.CurrentScreen = ScreenMainMenu
	m.state.ErrorMessage = ""
	return m, nil
}

func (m *WizardModel) handleAddProvider1Enter() (tea.Model, tea.Cmd) {
	// Validate input
	name := strings.TrimSpace(m.state.NewProviderName)
	baseURL := strings.TrimSpace(m.state.NewProviderBaseURL)
	models := strings.TrimSpace(m.state.NewProviderModels)

	if name == "" {
		m.state.ErrorMessage = "Provider name is required"
		return m, nil
	}
	if baseURL == "" {
		m.state.ErrorMessage = "Base URL is required"
		return m, nil
	}
	if models == "" {
		m.state.ErrorMessage = "At least one model is required"
		return m, nil
	}

	// Parse models (one per line)
	modelList := strings.Split(models, "\n")
	var validModels []string
	for _, m := range modelList {
		m = strings.TrimSpace(m)
		if m != "" {
			validModels = append(validModels, m)
		}
	}

	if len(validModels) == 0 {
		m.state.ErrorMessage = "At least one model is required"
		return m, nil
	}

	m.state.NewProviderModels = strings.Join(validModels, "\n")
	if !m.state.EditingProvider {
		m.state.NewProviderAPIKey = "" // Clear for new providers to prevent stale data
	}
	m.state.CurrentScreen = ScreenAddProvider2
	m.state.ErrorMessage = ""
	return m, nil
}

func (m *WizardModel) handleAddProvider2Enter() (tea.Model, tea.Cmd) {
	// Save the provider
	providerName := strings.TrimSpace(m.state.NewProviderName)
	apiKey := m.state.NewProviderAPIKey

	if apiKey == "" {
		m.state.ErrorMessage = "API key is required"
		return m, nil
	}

	// Generate env var name
	envVarName := GenerateEnvVarName(providerName)

	// Create provider config (preserve advanced settings if editing)
	pc := config.ProviderConfig{
		APIKey:      "${" + envVarName + "}",
		BaseURL:     strings.TrimSpace(m.state.NewProviderBaseURL),
		Transformer: strings.TrimSpace(m.state.NewProviderTransformer),
		Models:      strings.Split(strings.TrimSpace(m.state.NewProviderModels), "\n"),
	}
	if m.state.NewProviderDisableKeepAlives {
		pc.DisableKeepAlives = true
	}
	if bytes, err := strconv.ParseInt(strings.TrimSpace(m.state.NewProviderMaxRequestBodyBytes), 10, 64); err == nil && bytes > 0 {
		pc.MaxRequestBodyBytes = bytes
	}
	m.state.Config.Providers[providerName] = pc

	m.state.HasChanges = true

	// Track resolved API key
	if m.state.ResolvedAPIKeys == nil {
		m.state.ResolvedAPIKeys = make(map[string]string)
	}
	m.state.ResolvedAPIKeys[providerName] = apiKey

	// Shell integration is deferred to "Save & Exit" (SyncAllShellExports/SourceAllNow)

	// Reset state and go back to providers
	m.resetAddProviderState()
	m.state.CurrentScreen = ScreenProviders
	m.state.ErrorMessage = ""
	return m, nil
}

func (m *WizardModel) handleEditRouteEnter() (tea.Model, tea.Cmd) {
	// When route name field is focused (field 0), handle dropdown
	if m.focusedField == 0 {
		if m.state.ShowRouteNameDropdown {
			// Select the highlighted route name and close dropdown
			matches := m.getRouteNameDropdownList()
			if m.state.RouteNameDropdownCursor < len(matches) {
				m.state.EditRouteName = matches[m.state.RouteNameDropdownCursor]
			}
			m.state.ShowRouteNameDropdown = false
			m.state.RouteNameDropdownCursor = 0
			return m, nil
		}
		// Dropdown not open — open it
		m.state.ShowRouteNameDropdown = true
		m.state.RouteNameDropdownCursor = 0
		return m, nil
	}

	// When chain list is focused (field 1), handle dropdown selection
	if m.focusedField == 1 {
		// If provider dropdown is open, select provider and open model dropdown
		if m.state.ShowDropdown {
			providers := m.getChainProviderList()
			if m.state.DropdownCursor < len(providers) {
				selectedProvider := providers[m.state.DropdownCursor]
				cursor := m.state.EditRouteChainCursor
				if cursor < len(m.state.EditRouteChain) {
					m.state.EditRouteChain[cursor].Provider = selectedProvider
					m.state.EditRouteChain[cursor].Model = ""
				}
				m.state.ShowDropdown = false
				m.state.DropdownCursor = 0
				// Open model dropdown for selected provider
				m.state.ShowModelDropdown = true
				m.state.ModelDropdownCursor = 0
			}
			return m, nil
		}
		// If model dropdown is open, select model and close
		if m.state.ShowModelDropdown {
			models := m.getChainModelList()
			if m.state.ModelDropdownCursor < len(models) {
				selectedModel := models[m.state.ModelDropdownCursor]
				cursor := m.state.EditRouteChainCursor
				if cursor < len(m.state.EditRouteChain) {
					m.state.EditRouteChain[cursor].Model = selectedModel
				}
			}
			m.state.ShowModelDropdown = false
			m.state.ModelDropdownCursor = 0
			return m, nil
		}
		// No dropdown open — save the route
		return m.saveRouteFromEdit()

	}

	// focusedField == 0: save the route (fallback — combobox Enter now opens dropdown)
	return m.saveRouteFromEdit()
}

// saveRouteFromEdit validates and saves the current route being edited.
func (m *WizardModel) saveRouteFromEdit() (tea.Model, tea.Cmd) {
	routeName := strings.TrimSpace(m.state.EditRouteName)
	if routeName == "" {
		m.state.ErrorMessage = "Route name is required"
		return m, nil
	}

	var chainParts []string
	for _, target := range m.state.EditRouteChain {
		if target.Provider != "" && target.Model != "" {
			chainParts = append(chainParts, fmt.Sprintf("%s:%s", target.Provider, target.Model))
		}
	}

	if len(chainParts) == 0 {
		m.state.ErrorMessage = "At least one provider:model is required"
		return m, nil
	}

	chainStr := strings.Join(chainParts, ";")

	// Save to correct location based on current tab
	currentRoutes := m.getCurrentRoutes()
	currentRoutes[routeName] = chainStr
	m.saveCurrentRoutes(currentRoutes)

	m.state.CurrentScreen = ScreenRoutes
	m.state.ErrorMessage = ""
	return m, nil
}

func (m *WizardModel) handleFormInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle input based on focused field
	switch m.state.CurrentScreen {
	case ScreenAddProvider1:
		switch m.focusedField {
		case 0: // Provider name
			if msg.String() == "backspace" && len(m.state.NewProviderName) > 0 {
				m.state.NewProviderName = m.state.NewProviderName[:len(m.state.NewProviderName)-1]
				m.state.ShowDropdown = true
				m.state.DropdownCursor = 0
			} else if msg.Paste {
				m.state.NewProviderName += string(msg.Runes)
				m.state.ShowDropdown = true
				m.state.DropdownCursor = 0
			} else if len(msg.String()) == 1 {
				m.state.NewProviderName += msg.String()
				m.state.ShowDropdown = true
				m.state.DropdownCursor = 0
			}
		case 1: // Base URL
			if msg.String() == "backspace" && len(m.state.NewProviderBaseURL) > 0 {
				m.state.NewProviderBaseURL = m.state.NewProviderBaseURL[:len(m.state.NewProviderBaseURL)-1]
			} else if msg.Paste {
				m.state.NewProviderBaseURL += string(msg.Runes)
			} else if len(msg.String()) == 1 {
				m.state.NewProviderBaseURL += msg.String()
			}
		case 2: // Models (textarea)
			removedNewline := false
			if msg.String() == "backspace" && len(m.state.NewProviderModels) > 0 {
				if m.state.NewProviderModels[len(m.state.NewProviderModels)-1] == '\n' {
					removedNewline = true
				}
				m.state.NewProviderModels = m.state.NewProviderModels[:len(m.state.NewProviderModels)-1]
			} else if msg.Paste {
				m.state.NewProviderModels += string(msg.Runes)
			} else if len(msg.String()) == 1 {
				m.state.NewProviderModels += msg.String()
			}
			if !removedNewline {
				m.state.ShowModelDropdown = true
				m.state.ModelDropdownCursor = 0
			}
		case 3: // DisableKeepAlives toggle
			if msg.String() == " " {
				m.state.NewProviderDisableKeepAlives = !m.state.NewProviderDisableKeepAlives
			}
		case 4: // MaxRequestBodyBytes
			if msg.String() == "backspace" && len(m.state.NewProviderMaxRequestBodyBytes) > 0 {
				m.state.NewProviderMaxRequestBodyBytes = m.state.NewProviderMaxRequestBodyBytes[:len(m.state.NewProviderMaxRequestBodyBytes)-1]
			} else if msg.Paste {
				for _, r := range msg.Runes {
					if r >= '0' && r <= '9' {
						m.state.NewProviderMaxRequestBodyBytes += string(r)
					}
				}
			} else if len(msg.String()) == 1 && msg.String() >= "0" && msg.String() <= "9" {
				m.state.NewProviderMaxRequestBodyBytes += msg.String()
			}
		}

	case ScreenAddProvider2:
		if msg.String() == "backspace" && len(m.state.NewProviderAPIKey) > 0 {
			m.state.NewProviderAPIKey = m.state.NewProviderAPIKey[:len(m.state.NewProviderAPIKey)-1]
		} else if msg.Paste {
			m.state.NewProviderAPIKey += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.NewProviderAPIKey += msg.String()
		}
	}
	return m, nil
}

func (m *WizardModel) handleServerInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.focusedField {
	case 0: // Host
		if msg.String() == "backspace" && len(m.state.ServerHost) > 0 {
			m.state.ServerHost = m.state.ServerHost[:len(m.state.ServerHost)-1]
		} else if msg.Paste {
			m.state.ServerHost += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.ServerHost += msg.String()
		}
	case 1: // Port
		if msg.String() == "backspace" && len(m.state.ServerPort) > 0 {
			m.state.ServerPort = m.state.ServerPort[:len(m.state.ServerPort)-1]
		} else if msg.Paste {
			for _, r := range msg.Runes {
				if r >= '0' && r <= '9' {
					m.state.ServerPort += string(r)
				}
			}
		} else if len(msg.String()) == 1 && msg.String() >= "0" && msg.String() <= "9" {
			m.state.ServerPort += msg.String()
		}
		return m, m.checkPortAvailability()
	case 2: // MaxRetries
		if msg.String() == "backspace" && len(m.state.ServerMaxRetries) > 0 {
			m.state.ServerMaxRetries = m.state.ServerMaxRetries[:len(m.state.ServerMaxRetries)-1]
		} else if msg.Paste {
			for _, r := range msg.Runes {
				if r >= '0' && r <= '9' {
					m.state.ServerMaxRetries += string(r)
				}
			}
		} else if len(msg.String()) == 1 && msg.String() >= "0" && msg.String() <= "9" {
			m.state.ServerMaxRetries += msg.String()
		}
	case 3: // RetryDelay
		if msg.String() == "backspace" && len(m.state.ServerRetryDelay) > 0 {
			m.state.ServerRetryDelay = m.state.ServerRetryDelay[:len(m.state.ServerRetryDelay)-1]
		} else if msg.Paste {
			m.state.ServerRetryDelay += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.ServerRetryDelay += msg.String()
		}
	case 4: // AutoRestartIdle
		if msg.String() == "backspace" && len(m.state.ServerAutoRestartIdle) > 0 {
			m.state.ServerAutoRestartIdle = m.state.ServerAutoRestartIdle[:len(m.state.ServerAutoRestartIdle)-1]
		} else if msg.Paste {
			m.state.ServerAutoRestartIdle += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.ServerAutoRestartIdle += msg.String()
		}
	case 5: // AutoRestartWindow
		if msg.String() == "backspace" && len(m.state.ServerAutoRestartWindow) > 0 {
			m.state.ServerAutoRestartWindow = m.state.ServerAutoRestartWindow[:len(m.state.ServerAutoRestartWindow)-1]
		} else if msg.Paste {
			m.state.ServerAutoRestartWindow += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.ServerAutoRestartWindow += msg.String()
		}
	case 6: // AutoRestartTimezone
		if msg.String() == "backspace" && len(m.state.ServerAutoRestartTimezone) > 0 {
			m.state.ServerAutoRestartTimezone = m.state.ServerAutoRestartTimezone[:len(m.state.ServerAutoRestartTimezone)-1]
		} else if msg.Paste {
			m.state.ServerAutoRestartTimezone += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.ServerAutoRestartTimezone += msg.String()
		}
	case 7: // AutoRestartBackoffMax
		if msg.String() == "backspace" && len(m.state.ServerAutoRestartBackoffMax) > 0 {
			m.state.ServerAutoRestartBackoffMax = m.state.ServerAutoRestartBackoffMax[:len(m.state.ServerAutoRestartBackoffMax)-1]
		} else if msg.Paste {
			m.state.ServerAutoRestartBackoffMax += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.ServerAutoRestartBackoffMax += msg.String()
		}
	}
	return m, nil
}

func (m *WizardModel) handleLoggingInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.focusedField {
	case 0: // Toggle enabled
		if msg.String() == " " {
			m.state.LoggingEnabled = !m.state.LoggingEnabled
		}
	case 1: // Level - dropdown handled by handleNavigation/handleEnter
	case 2: // Destination - dropdown handled by handleNavigation/handleEnter
	case 3: // File path
		if !m.state.LoggingEnabled {
			return m, nil
		}
		if msg.String() == "backspace" && len(m.state.LoggingFilePath) > 0 {
			m.state.LoggingFilePath = m.state.LoggingFilePath[:len(m.state.LoggingFilePath)-1]
		} else if msg.Paste {
			m.state.LoggingFilePath += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.LoggingFilePath += msg.String()
		}
	}
	return m, nil
}

func (m *WizardModel) handleRouteEditInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.focusedField == 0 {
		// Route name input
		if msg.String() == "backspace" && len(m.state.EditRouteName) > 0 {
			m.state.EditRouteName = m.state.EditRouteName[:len(m.state.EditRouteName)-1]
		} else if msg.Paste {
			m.state.EditRouteName += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.EditRouteName += msg.String()
		}
		// If dropdown is open, filter and reset cursor
		if m.state.ShowRouteNameDropdown {
			m.state.RouteNameDropdownCursor = 0
		}
	}
	// focusedField == 1 (chain list): handled by character key cases for 'a' and backspace
	return m, nil
}

func (m *WizardModel) cycleLoggingLevel(delta int) {
	levels := LogLevelOptions
	currentIdx := 0
	for i, l := range levels {
		if l == m.state.LoggingLevel {
			currentIdx = i
			break
		}
	}
	newIdx := (currentIdx + delta + len(levels)) % len(levels)
	m.state.LoggingLevel = levels[newIdx]
}

func (m *WizardModel) cycleLoggingDestination(delta int) {
	dests := LogDestinationOptions
	currentIdx := 0
	for i, d := range dests {
		if d == m.state.LoggingDestination {
			currentIdx = i
			break
		}
	}
	newIdx := (currentIdx + delta + len(dests)) % len(dests)
	m.state.LoggingDestination = dests[newIdx]
}

func (m *WizardModel) getMaxFields() int {
	switch m.state.CurrentScreen {
	case ScreenAddProvider1:
		return 5
	case ScreenAddProvider2:
		return 1
	case ScreenServer:
		return 8
	case ScreenLogging:
		return 4
	case ScreenEditRoute:
		return 2
	case ScreenCreateProfile:
		return 4 // Name, Description, Create button, Cancel button
	case ScreenEditProfile:
		return 4 // Name, Description, Save button, Cancel button
	case ScreenCreateAPIKey:
		return 3 // Name, Group, Create button
	case ScreenMultiUser:
		return 7 // Checkbox, MaxConc, WRED Min, WRED Max, Manage Groups, Save, Cancel
	case ScreenCreateGroup:
		return 6 // Name, Profile, Priority, MaxConc, Save button, Cancel button
	default:
		return 0
	}
}

// Helper methods

func (m *WizardModel) getProviderList() []string {
	providers := make([]string, 0, len(m.state.Config.Providers))
	for name := range m.state.Config.Providers {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	return providers
}

func (m *WizardModel) getRouteList() []string {
	routes := m.getCurrentRoutes()
	routeNames := make([]string, 0, len(routes))
	for name := range routes {
		routeNames = append(routeNames, name)
	}
	sort.Strings(routeNames)
	return routeNames
}

// getChainProviderList returns configured provider names for the chain dropdown.
func (m *WizardModel) getChainProviderList() []string {
	return m.getProviderList()
}

// getChainModelList returns models for the currently selected chain item's provider.
func (m *WizardModel) getChainModelList() []string {
	if m.state.EditRouteChainCursor >= len(m.state.EditRouteChain) {
		return nil
	}
	providerName := m.state.EditRouteChain[m.state.EditRouteChainCursor].Provider
	if providerName == "" {
		return nil
	}
	// Check config providers first
	if p, ok := m.state.Config.Providers[providerName]; ok {
		models := make([]string, len(p.Models))
		copy(models, p.Models)
		sort.Strings(models)
		return models
	}
	// Fall back to preset models
	if preset, ok := ProviderPresets[providerName]; ok {
		models := make([]string, len(preset.Models))
		copy(models, preset.Models)
		sort.Strings(models)
		return models
	}
	return nil
}

// getRouteNameDropdownList returns predefined route names filtered by the current input.
// If EditRouteName is empty, all predefined names are returned.
func (m *WizardModel) getRouteNameDropdownList() []string {
	input := strings.ToLower(m.state.EditRouteName)
	var result []string
	for _, name := range PredefinedRouteNames {
		if input == "" || strings.HasPrefix(strings.ToLower(name), input) {
			result = append(result, name)
		}
	}
	return result
}

// isValidProviderModel checks if a provider:model pair exists in the config.
// Empty provider or model strings return false (placeholder entries are not valid).
func (m *WizardModel) isValidProviderModel(provider, model string) bool {
	if provider == "" || model == "" {
		return false
	}
	p, ok := m.state.Config.Providers[provider]
	if !ok {
		return false
	}
	for _, m := range p.Models {
		if m == model {
			return true
		}
	}
	return false
}

// renderChainStyled renders a route chain string with styling based on selection and validity.
// selected indicates whether the overall row is selected (affects background).
func (m *WizardModel) renderChainStyled(chain string, width int, selected bool) string {
	targets := config.ParseRoute(chain)
	if len(targets) == 0 {
		if selected {
			return ListItemSelectedStyle.Width(width).Render("")
		}
		return ListItemStyle.Width(width).Render("")
	}

	// Build plain text first (no ANSI codes)
	parts := make([]string, len(targets))
	for i, t := range targets {
		parts[i] = t.Provider + ":" + t.Model
	}
	plainText := strings.Join(parts, ";")

	// Truncate plain text (no ANSI codes to corrupt width calculation)
	truncatedText := truncate(plainText, width)

	// Determine style: if any target is invalid, use invalid style
	hasInvalid := false
	for _, t := range targets {
		if !m.isValidProviderModel(t.Provider, t.Model) {
			hasInvalid = true
			break
		}
	}

	var style lipgloss.Style
	switch {
	case selected && hasInvalid:
		style = ListItemInvalidSelectedStyle
	case selected:
		style = ListItemSelectedStyle
	case hasInvalid:
		style = ListItemInvalidStyle
	default:
		style = ListItemStyle
	}

	return style.Width(width).Render(truncatedText)
}

// initProfileTabs initializes the profile tab keys when entering Routes screen.
// Legacy routes are auto-migrated to a "default" profile when no profiles exist.
func (m *WizardModel) initProfileTabs() {
	m.state.ProfileTabKeys = []string{}

	// Ensure Profiles map is initialized
	if m.state.Config.Router.Profiles == nil {
		m.state.Config.Router.Profiles = make(map[string]config.ProfileConfig)
	}

	// Auto-migrate legacy routes to default profile when no profiles exist
	if len(m.state.Config.Router.Profiles) == 0 && len(m.state.Config.Router.Routes) > 0 {
		// Create default profile with legacy routes
		routes := make(map[string]string)
		for k, v := range m.state.Config.Router.Routes {
			routes[k] = v
		}
		m.state.Config.Router.Profiles["default"] = config.ProfileConfig{
			Name:        "Default",
			Description: "Auto-migrated from legacy routes",
			Routes:      routes,
		}
		// Clear legacy routes (migration complete)
		m.state.Config.Router.Routes = make(map[string]string)
		m.state.HasChanges = true
	}

	// Add all profile keys
	for key := range m.state.Config.Router.Profiles {
		m.state.ProfileTabKeys = append(m.state.ProfileTabKeys, key)
	}
	sort.Strings(m.state.ProfileTabKeys)

	// Pin "default" to first position
	for i, k := range m.state.ProfileTabKeys {
		if k == "default" {
			m.state.ProfileTabKeys = append(
				m.state.ProfileTabKeys[:i],
				m.state.ProfileTabKeys[i+1:]...,
			)
			m.state.ProfileTabKeys = append([]string{"default"}, m.state.ProfileTabKeys...)
			break
		}
	}

	// Default to "default" profile (always at index 0 if it exists)
	m.state.ProfileTabIndex = 0
}

// getCurrentRoutes returns the routes map for the currently selected tab.
// If profiles exist, always use profile routes. Otherwise use legacy routes.
func (m *WizardModel) getCurrentRoutes() map[string]string {
	// If profiles exist, always use profile routes
	if m.hasProfiles() {
		key := m.getCurrentProfileKey()
		if profile, ok := m.state.Config.Router.Profiles[key]; ok {
			return profile.Routes
		}
		// Fallback to default profile
		if profile, ok := m.state.Config.Router.Profiles["default"]; ok {
			return profile.Routes
		}
	}
	// No profiles - use legacy routes
	return m.state.Config.Router.Routes
}

// saveCurrentRoutes saves routes to the correct location based on current tab.
// If profiles exist, always save to profile. Otherwise save to legacy routes.
func (m *WizardModel) saveCurrentRoutes(routes map[string]string) {
	// If profiles exist, always save to profile
	if m.hasProfiles() {
		key := m.getCurrentProfileKey()
		if profile, ok := m.state.Config.Router.Profiles[key]; ok {
			profile.Routes = routes
			m.state.Config.Router.Profiles[key] = profile
		} else {
			// Fallback to default profile
			profile := m.state.Config.Router.Profiles["default"]
			profile.Routes = routes
			m.state.Config.Router.Profiles["default"] = profile
		}
	} else {
		// No profiles - save to legacy routes
		m.state.Config.Router.Routes = routes
	}
	m.state.HasChanges = true
}

// getCurrentProfileKey returns the profile key for the current tab, or "" for legacy tab.
// When profiles exist, returns the profile key from the tab index.
func (m *WizardModel) getCurrentProfileKey() string {
	// Legacy mode: no profiles exist
	if !m.hasProfiles() {
		return "" // indicates legacy routes
	}
	// Profiles exist - get key from tab
	if m.state.ProfileTabIndex < len(m.state.ProfileTabKeys) {
		return m.state.ProfileTabKeys[m.state.ProfileTabIndex]
	}
	return "default" // fallback
}

// isDefaultProfile returns true if the current tab is the "default" profile.
func (m *WizardModel) isDefaultProfile() bool {
	return m.getCurrentProfileKey() == "default"
}

// isOnAddTab returns true if the cursor is on the [+] add profile tab.
func (m *WizardModel) isOnAddTab() bool {
	return m.state.ProfileTabIndex == len(m.state.ProfileTabKeys)
}

// generateProfileKey generates a profile key from a display name.
// Converts to lowercase, replaces spaces with hyphens, removes special chars.
func generateProfileKey(name string) string {
	// Convert to lowercase
	key := strings.ToLower(name)
	// Replace spaces with hyphens
	key = strings.ReplaceAll(key, " ", "-")
	// Remove special characters (keep alphanumeric and hyphens)
	var result strings.Builder
	for _, c := range key {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result.WriteRune(c)
		}
	}
	key = result.String()
	// Remove consecutive hyphens
	key = strings.Join(strings.FieldsFunc(key, func(c rune) bool { return c == '-' }), "-")
	// Default to "default" if empty
	if key == "" {
		key = "default"
	}
	return key
}

// hasLegacyRoutes returns true if there are routes in Router.Routes.
func (m *WizardModel) hasLegacyRoutes() bool {
	return len(m.state.Config.Router.Routes) > 0
}

// hasProfiles returns true if any profiles exist.
func (m *WizardModel) hasProfiles() bool {
	return len(m.state.Config.Router.Profiles) > 0
}

// createDefaultProfile creates the "default" profile with optional route migration.
// Always clears legacy routes when creating the profile to ensure clean migration.
func (m *WizardModel) createDefaultProfile(copyRoutes bool) {
	routes := make(map[string]string)
	if copyRoutes {
		// Copy legacy routes to profile
		for k, v := range m.state.Config.Router.Routes {
			routes[k] = v
		}
	}
	// Always clear legacy routes when profiles are introduced
	m.state.Config.Router.Routes = make(map[string]string)

	m.state.Config.Router.Profiles["default"] = config.ProfileConfig{
		Name:        "Default",
		Description: "Launch profile for router",
		Routes:      routes,
	}
	m.state.HasChanges = true
}

// createNewProfile creates a new profile with the given name and description.
func (m *WizardModel) createNewProfile(name, description string) string {
	key := generateProfileKey(name)
	// If this is the first profile and key is "default", ensure we handle it correctly
	if len(m.state.Config.Router.Profiles) == 0 && key == "default" {
		m.createDefaultProfile(false)
		return "default"
	}
	// Ensure key is unique
	if _, exists := m.state.Config.Router.Profiles[key]; exists {
		// Append number to make unique
		i := 1
		for {
			newKey := fmt.Sprintf("%s-%d", key, i)
			if _, exists := m.state.Config.Router.Profiles[newKey]; !exists {
				key = newKey
				break
			}
			i++
		}
	}
	m.state.Config.Router.Profiles[key] = config.ProfileConfig{
		Name:        name,
		Description: description,
		Routes:      make(map[string]string),
	}
	m.state.HasChanges = true
	return key
}

// deleteCurrentProfile deletes the profile for the current tab.
// Returns an error message if deletion is not allowed.
func (m *WizardModel) deleteCurrentProfile() string {
	profileKey := m.getCurrentProfileKey()
	if profileKey == "" {
		return "Cannot delete legacy routes tab"
	}
	if profileKey == "default" {
		return "Cannot delete 'default' launch profile"
	}
	delete(m.state.Config.Router.Profiles, profileKey)
	m.state.HasChanges = true
	// Reinitialize tabs
	m.initProfileTabs()
	// Clamp to valid tab
	if m.state.ProfileTabIndex >= len(m.state.ProfileTabKeys) {
		m.state.ProfileTabIndex = len(m.state.ProfileTabKeys) - 1
	}
	return ""
}

func (m *WizardModel) resetAddProviderState() {
	m.state.NewProviderName = ""
	m.state.NewProviderBaseURL = ""
	m.state.NewProviderTransformer = "anthropic"
	m.state.NewProviderModels = ""
	m.state.NewProviderAPIKey = ""
	m.state.AddToShellConfig = true
	m.state.SourceImmediately = true
	m.state.ProviderPreset = "anthropic"
	m.state.EditingProvider = false
	m.state.ShowDropdown = false
	m.state.DropdownCursor = 0
	m.state.ShowModelDropdown = false
	m.state.ModelDropdownCursor = 0
	m.focusedField = 0
}

func (m *WizardModel) saveConfig() error {
	return config.Save(m.state.Config, m.state.ConfigPath)
}

func (m *WizardModel) exportConfig() error {
	data, err := json.MarshalIndent(m.state.Config, "", "  ")
	if err != nil {
		return err
	}
	exportPath := m.state.ConfigPath + ".export"
	return os.WriteFile(exportPath, data, 0644)
}

// View renders the wizard UI.
func (m *WizardModel) View() string {
	// Ensure minimum dimensions
	if m.width < 64 {
		m.width = 64
	}
	if m.height < 20 {
		m.height = 20
	}

	// Render based on current screen
	switch m.state.CurrentScreen {
	case ScreenMainMenu:
		return m.renderMainMenu()
	case ScreenProviders:
		return m.renderProviders()
	case ScreenAddProvider1:
		return m.renderAddProvider1()
	case ScreenAddProvider2:
		return m.renderAddProvider2()
	case ScreenRoutes:
		return m.renderRoutes()
	case ScreenEditRoute:
		return m.renderEditRoute()
	case ScreenCreateProfile:
		return m.renderCreateProfile()
	case ScreenEditProfile:
		return m.renderEditProfile()
	case ScreenServer:
		return m.renderServer()
	case ScreenLogging:
		return m.renderLogging()
	case ScreenViewConfig:
		return m.renderViewConfig()
	case ScreenTestConnection:
		return m.renderTestConnection()
	case ScreenAPIKeys:
		return m.renderAPIKeys()
	case ScreenCreateAPIKey:
		return m.renderCreateAPIKey()
	case ScreenMultiUser:
		return m.renderMultiUser()
	case ScreenGroups:
		return m.renderGroups()
	case ScreenCreateGroup:
		return m.renderCreateGroup()
	default:
		return m.renderMainMenu()
	}
}

// View with modal overlay
func (m *WizardModel) renderWithModal(content string) string {
	if m.state.ShowConfirm {
		modal := m.renderConfirmModal()
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			modal,
			lipgloss.WithWhitespaceBackground(PanelBackground),
			lipgloss.WithWhitespaceChars(" "),
		)
	}
	if m.state.ShowProfileEditModal {
		modal := m.renderProfileEditModal()
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			modal,
			lipgloss.WithWhitespaceBackground(PanelBackground),
			lipgloss.WithWhitespaceChars(" "),
		)
	}
	if m.state.ShowMigrationModal {
		modal := m.renderMigrationModal()
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			modal,
			lipgloss.WithWhitespaceBackground(PanelBackground),
			lipgloss.WithWhitespaceChars(" "),
		)
	}
	return content
}

// renderConfirmModal renders the confirmation modal.
func (m *WizardModel) renderConfirmModal() string {
	modalWidth := 50
	modalHeight := 9
	contentWidth := modalWidth - 4 // 46: fills content area after padding

	// Override modal alignment to Left — prevents modal-level centering
	modal := ModalStyle.Width(modalWidth).Height(modalHeight).Align(lipgloss.Left)

	yesBtn := ButtonStyle.Render(" Yes ")
	noBtn := ButtonStyle.Render(" No ")
	if m.state.ConfirmCursor == 0 {
		yesBtn = ButtonActiveStyle.Render(" Yes ")
	} else {
		noBtn = ButtonActiveStyle.Render(" No ")
	}

	// Title: left-aligned, fills full content width
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(HeaderText).
		Width(contentWidth).
		Align(lipgloss.Left).
		Render(m.state.ConfirmMessage)

	// Buttons row: centered within content width
	buttonsRow := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Render(lipgloss.JoinHorizontal(lipgloss.Center, yesBtn, noBtn))

	// Help row: centered within content width
	helpRow := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Render(HelpTextStyle.Render("[←/→] Choose   [Enter] Confirm   [Esc] Cancel"))

	// Push content to top, help to bottom — modal content area is ~5 lines (9 height - 2 padding - 2 border)
	spacer := lipgloss.NewStyle().Width(contentWidth).Render("")
	content := lipgloss.JoinVertical(lipgloss.Left, title, spacer, buttonsRow, spacer, helpRow)

	return modal.Render(content)
}

// Main menu rendering
func (m *WizardModel) renderMainMenu() string {
	providerCount := len(m.state.Config.Providers)
	routeCount := len(m.state.Config.Router.Routes)
	_ = routeCount
	serverInfo := fmt.Sprintf("%s:%d", m.state.Config.Server.Host, m.state.Config.Server.Port)
	logLevel := m.state.Config.Logging.Level
	if logLevel == "" {
		logLevel = "info"
	}
	logDest := m.state.Config.Logging.Destination
	if logDest == "" {
		logDest = "stdout"
	}

	menuItems := []struct {
		label   string
		info    string
		cursor  int
	}{
		{"[1] Providers", fmt.Sprintf("Manage API providers (%d configured)", providerCount), 0},
		{"[2] Routes", "Configure routing rules", 1},
		{"[3] Proxy", fmt.Sprintf("Host: %s", serverInfo), 2},
		{"[4] Logging", fmt.Sprintf("Level: %s, Destination: %s", logLevel, logDest), 3},
		{"[5] View Config", "Browse current configuration", 4},
		{"[6] Multi-User", "Manage multi-user settings and groups", 5},
		{"[7] API Keys", "Manage API keys for multi-user mode", 6},
		{"[8] Save & Exit", "Write changes to disk", 7},
		{"[9] Quit without saving", "Exit without saving changes", 8},
	}

	var menuLines []string
	const labelColumnWidth = 28 // Fixed width for menu label column

	for _, item := range menuItems {
		// Render label with fixed width to prevent word wrap
		var labelStr string
		if item.cursor == m.state.ProviderCursor {
			labelStr = MenuItemSelectedStyle.Width(labelColumnWidth).Render(item.label)
		} else {
			labelStr = MenuItemStyle.Width(labelColumnWidth).Render(item.label)
		}

		// Always render both label and info for consistent spacing
		infoWidth := m.contentWidth() - labelColumnWidth
		if infoWidth < 10 {
			infoWidth = 10
		}

		var infoStr string
		if item.cursor == m.state.ProviderCursor {
			// Selected: show actual info
			infoStr = MenuItemDescriptionStyle.Width(infoWidth).Render(item.info)
		} else {
			// Unselected: show empty placeholder to maintain consistent spacing
			infoStr = MenuItemDimmedStyle.Width(infoWidth).Render("")
		}
		line := lipgloss.JoinHorizontal(lipgloss.Top, labelStr, infoStr)
		menuLines = append(menuLines, line)
	}

	title := TitleStyle.Width(m.contentWidth()).Render("Configuration Wizard")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		m.blankLine(),
		lipgloss.JoinVertical(lipgloss.Left, menuLines...),
		m.blankLine(),
		m.footlineWithVersion("[↑/↓] Navigate   [Enter] Select"),
	)

	if m.state.HasUnsavedChanges() {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			ErrorStyle.Width(m.contentWidth()).Render("⚠ Unsaved changes"),
		)
	}

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// Providers screen rendering
func (m *WizardModel) renderProviders() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Providers")
	var providerLines []string
	providers := m.getProviderList()

	for i, name := range providers {
		pc := m.state.Config.Providers[name]
		models := strings.Join(pc.Models, ", ")

		var line string
		if i == m.state.ProviderCursor {
			line = ListItemSelectedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("▶ %s", name))
		} else {
			line = ListItemStyle.Width(m.contentWidth()).Render(fmt.Sprintf("  %s", name))
		}
		providerLines = append(providerLines, line)
		providerLines = append(providerLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("   ├─ "+pc.BaseURL))
		providerLines = append(providerLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("   └─ "+models))
	}

	if len(providers) == 0 {
		providerLines = append(providerLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("No providers configured"))
	}

	providerLines = append(providerLines, m.blankLine())
	providerLines = append(providerLines, m.footline("[↑/↓] Navigate   [Enter] Edit   [a] Add   [T] Test   [⌫] Delete   [Esc] Back"))

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.fullWidth(title),
		m.blankLine(),
		lipgloss.JoinVertical(lipgloss.Left, providerLines...),
	)

	if m.state.ErrorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage),
		)
	}

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// renderTestConnection renders the provider connection test screen.
func (m *WizardModel) renderTestConnection() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Test Connection")
	var lines []string

	lines = append(lines, m.blankLine())
	lines = append(lines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("Provider: "+m.state.TestProvider))
	lines = append(lines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("Model: "+m.state.TestModel))
	lines = append(lines, m.blankLine())

	switch m.state.TestStatus {
	case "testing":
		lines = append(lines, m.footline("Testing connection..."))
	case "success":
		latency := m.state.TestLatency * 1000
		lines = append(lines, ErrorStyle.Width(m.contentWidth()).Render("Connection successful!"))
		lines = append(lines, MenuItemDimmedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("Latency: %.0fms", latency)))
	case "error":
		lines = append(lines, ErrorStyle.Width(m.contentWidth()).Render("Connection failed!"))
		lines = append(lines, ErrorStyle.Width(m.contentWidth()).Render(m.state.TestError))
	}

	lines = append(lines, m.blankLine())
	lines = append(lines, m.footline("[Esc] Back to Providers"))

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.fullWidth(title),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return mainBox
}

// getPresetMatches returns preset names matching the current provider name input.
func (m *WizardModel) getPresetMatches() []string {
	input := strings.ToLower(m.state.NewProviderName)
	var matches []string
	for name := range ProviderPresets {
		if _, exists := m.state.Config.Providers[name]; exists {
			continue
		}
		if input == "" || strings.HasPrefix(strings.ToLower(name), input) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	if len(matches) > 4 {
		matches = matches[:4]
	}
	return matches
}

// applyPreset fills in provider fields from a preset and hides the dropdown.
func (m *WizardModel) applyPreset(name string) {
	if preset, ok := ProviderPresets[name]; ok {
		m.state.NewProviderName = name
		m.state.NewProviderBaseURL = preset.BaseURL
		m.state.NewProviderTransformer = preset.Transformer
		// Don't auto-populate models — users can use the suggestions dropdown
	}
	m.state.ShowDropdown = false
	m.state.DropdownCursor = 0
}

// getModelSuggestions returns model names matching the current provider preset
// filtered by the current line prefix in the models field.
func (m *WizardModel) getModelSuggestions() []string {
	// Find the preset matching the current provider name
	providerName := strings.TrimSpace(strings.ToLower(m.state.NewProviderName))
	var models []string
	for key, preset := range ProviderPresets {
		if strings.ToLower(key) == providerName {
			models = preset.Models
			break
		}
	}
	if len(models) == 0 {
		return nil
	}

	// Get the current line (text after last newline) for prefix filtering
	text := m.state.NewProviderModels
	currentLine := text
	if idx := strings.LastIndex(text, "\n"); idx >= 0 {
		currentLine = text[idx+1:]
	}
	prefix := strings.ToLower(currentLine)

	// Filter models by prefix
	var matches []string
	for _, model := range models {
		if prefix == "" || strings.HasPrefix(strings.ToLower(model), prefix) {
			matches = append(matches, model)
		}
	}

	// Exclude models already present in the text (full lines)
	existingLines := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			existingLines[strings.ToLower(line)] = true
		}
	}

	var filtered []string
	for _, match := range matches {
		if !existingLines[strings.ToLower(match)] {
			filtered = append(filtered, match)
		}
	}

	if len(filtered) > 6 {
		filtered = filtered[:6]
	}
	return filtered
}

// insertModelFromDropdown replaces the current line in the models field with the
// selected model and appends a newline, then closes the model dropdown.
func (m *WizardModel) insertModelFromDropdown(model string) {
	text := m.state.NewProviderModels
	// Find the current line (text after last newline)
	var prefix string
	if idx := strings.LastIndex(text, "\n"); idx >= 0 {
		prefix = text[:idx+1]
	}
	// Replace current line with the selected model and add a newline
	m.state.NewProviderModels = prefix + model + "\n"
	// Keep dropdown open for next model suggestion (reset cursor to top)
	m.state.ShowModelDropdown = true
	m.state.ModelDropdownCursor = 0
}

// Add Provider Step 1 rendering
func (m *WizardModel) renderAddProvider1() string {
	titleText := "Add Provider (1/2)"
	if m.state.EditingProvider {
		titleText = "Edit Provider (1/2)"
	}
	title := SectionHeaderStyle.Width(m.contentWidth()).Render(titleText)

	// Input fields
	nameLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Provider Name:")
	nameInput := m.state.NewProviderName
	if m.focusedField == 0 {
		nameInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(nameInput + "_")
	} else {
		nameInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(nameInput)
	}

	urlLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Base URL:")
	urlInput := m.state.NewProviderBaseURL
	if m.focusedField == 1 {
		urlInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(urlInput + "_")
	} else {
		urlInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(urlInput)
	}

	modelsLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Models (one per line):")
	modelsInput := m.state.NewProviderModels
	if m.focusedField == 2 {
		modelsInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Height(1).Render(modelsInput + "_")
	} else {
		modelsInput = InputFieldStyle.Width(m.inputFieldWidth()).Height(1).Render(modelsInput)
	}

	// Build content — always show all fields, dropdown inserted inline when visible
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title, m.blankLine(),
		nameLabel, m.fullWidth(nameInput),
	)

	// Insert dropdown between name field and URL field when visible
	if m.state.ShowDropdown && m.focusedField == 0 {
		matches := m.getPresetMatches()
		var dropdownItems []string
		dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
		for i, match := range matches {
			if i == m.state.DropdownCursor {
				dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(match))
			} else {
				dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(match))
			}
		}
		dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
			lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
		)
		content = lipgloss.JoinVertical(lipgloss.Left, content, dropdown)
	}

	// Append remaining fields
	content = lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		urlLabel, m.fullWidth(urlInput),
		modelsLabel, m.fullWidth(modelsInput),
	)

	// Insert model dropdown below models field when visible
	if m.state.ShowModelDropdown && m.focusedField == 2 {
		modelMatches := m.getModelSuggestions()
		if len(modelMatches) > 0 {
			var dropdownItems []string
			dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
			for i, match := range modelMatches {
				if i == m.state.ModelDropdownCursor {
					dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(match))
				} else {
					dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(match))
				}
			}
			dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
				lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
			)
			content = lipgloss.JoinVertical(lipgloss.Left, content, dropdown)
		}
	}

	// Advanced settings
	keepAliveLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Disable Keep-Alive (connection issues):")
	keepAliveStatus := "off"
	if m.state.NewProviderDisableKeepAlives {
		keepAliveStatus = "on"
	}
	keepAliveInput := keepAliveStatus
	if m.focusedField == 3 {
		keepAliveInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render("[Space] " + keepAliveStatus)
	} else {
		keepAliveInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(keepAliveStatus)
	}

	maxBodyLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Max Request Body (bytes, 0=unlimited):")
	maxBodyInput := m.state.NewProviderMaxRequestBodyBytes
	if m.state.NewProviderMaxRequestBodyBytes == "" {
		maxBodyInput = "0"
	}
	if m.focusedField == 4 {
		maxBodyInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(maxBodyInput + "_")
	} else {
		maxBodyInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(maxBodyInput)
	}

	content = lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		m.blankLine(),
		keepAliveLabel, m.fullWidth(keepAliveInput),
		m.blankLine(),
		maxBodyLabel, m.fullWidth(maxBodyInput),
	)

	content = lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		m.blankLine(),
	)

	// Append help text based on dropdown state
	if m.state.ShowDropdown && m.focusedField == 0 {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.footline("[↑/↓] Select preset   [Enter] Apply   [Esc] Close"),
		)
	} else if m.state.ShowModelDropdown && m.focusedField == 2 {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.footline("[↑/↓] Select model   [Enter] Insert   [Esc] Close"),
		)
	} else {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.footline("[Esc] Cancel   [Tab] Next field   [Enter] Next →"),
		)
	}

	if m.state.ErrorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage),
		)
	}

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// Add Provider Step 2 rendering
func (m *WizardModel) renderAddProvider2() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Environment Setup (2/2)")

	// Input field
	apiKeyLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Enter API Key:")
	var maskedKey string
	if strings.HasPrefix(m.state.NewProviderAPIKey, "${") && strings.HasSuffix(m.state.NewProviderAPIKey, "}") {
		maskedKey = ""
	} else {
		maskedKey = strings.Repeat("*", len(m.state.NewProviderAPIKey))
	}
	if maskedKey == "" {
		maskedKey = "________________________________"
	}
	apiKeyInput := InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(maskedKey)

	// Checkboxes (read-only indicators — shell integration happens on "Save & Exit")
	addToShell := CheckboxCheckedStyle.Render()
	sourceNow := CheckboxCheckedStyle.Render()

	// Preview
	preview := GetExportPreview(m.state.NewProviderName, m.state.NewProviderAPIKey)

	// Buttons
	backBtn := ButtonStyle.Render("[← Back]")
	saveBtn := ButtonPrimaryStyle.Render("[Save Provider]")

	// Side-by-side: Shell Configuration | Preview
	shellWidth := m.contentWidth() * 55 / 100
	previewWidth := m.contentWidth() - shellWidth - 2 // -2 for gap
	leftCol := lipgloss.JoinVertical(
		lipgloss.Left,
		MenuItemDimmedStyle.Width(shellWidth).Render("Shell Configuration:"),
		lipgloss.JoinHorizontal(lipgloss.Left, addToShell, " Add to shell config (~/.zshrc)"),
		lipgloss.JoinHorizontal(lipgloss.Left, sourceNow, " Source environment now"),
	)
	rightCol := lipgloss.JoinVertical(
		lipgloss.Left,
		MenuItemDimmedStyle.Width(previewWidth).Render("Preview:"),
		InputFieldStyle.Width(previewWidth).Height(2).Render(preview),
	)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.fullWidth(title),
		m.blankLine(),
		MenuItemDimmedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("Provider: %s", m.state.NewProviderName)),
		m.blankLine(),
		apiKeyLabel,
		m.fullWidth(apiKeyInput),
		m.blankLine(),
		lipgloss.JoinHorizontal(lipgloss.Top, leftCol, "  ", rightCol),
		m.blankLine(),
		m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, backBtn, saveBtn)),
		m.blankLine(),
		m.renderAddProvider2Hints(),
	)

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// renderAddProvider2Hints builds the hints bar for Add Provider step 2.
// When an error is present, it is displayed right-aligned in the hints bar.
func (m *WizardModel) renderAddProvider2Hints() string {
	const hintsText = "[Esc] Back   [Enter] Save   [⌘V/Ctrl+V] Paste"
	if m.state.ErrorMessage == "" {
		return m.footline(hintsText)
	}
	hintsLeft := HelpTextStyle.Render(hintsText)
	errorHint := ErrorStyle.Render(m.state.ErrorMessage)
	return lipgloss.NewStyle().Width(m.contentWidth()).Render(
		lipgloss.JoinHorizontal(lipgloss.Left, hintsLeft,
			lipgloss.NewStyle().Width(m.contentWidth()-runewidth.StringWidth(hintsText)-HelpTextStyle.GetHorizontalFrameSize()).Align(lipgloss.Right).Render(errorHint)),
	)
}

// Routes screen rendering
func (m *WizardModel) renderRoutes() string {
	// Determine current context for title
	profileKey := m.getCurrentProfileKey()
	contextText := ""
	if profileKey == "" {
		if m.hasProfiles() {
			contextText = "Legacy Routes (fallback)"
		}
	} else if profileKey == "default" {
		contextText = "default - launch profile"
	} else {
		if profile, ok := m.state.Config.Router.Profiles[profileKey]; ok {
			contextText = profileKey + " - " + profile.Name
		} else {
			contextText = profileKey
		}
	}

	// Title with context on the right
	titleText := "Routes"
	if contextText != "" {
		titleSpacing := m.contentWidth() - len("Routes") - len(contextText) - 4
		if titleSpacing > 0 {
			titleText = "Routes" + strings.Repeat(" ", titleSpacing) + contextText
		}
	}
	title := SectionHeaderStyle.Width(m.contentWidth()).Render(titleText)

	// Tab bar
	tabs := m.renderProfileTabs()
	tabDivider := m.divider()

	// Table header
	headerRow := lipgloss.JoinHorizontal(
		lipgloss.Left,
		SectionHeaderStyle.Width(20).Render("Route"),
		SectionHeaderStyle.Width(m.contentWidth() - 20).Render("Chain"),
	)

	// Route list from current tab
	var routeLines []string
	routes := m.getRouteList()
	currentRoutes := m.getCurrentRoutes()

	for i, name := range routes {
		chain := currentRoutes[name]
		selected := i == m.state.RouteCursor

		var line string
		if selected {
			line = lipgloss.JoinHorizontal(
				lipgloss.Left,
				ListItemSelectedStyle.Width(20).Render(name),
				m.renderChainStyled(chain, m.contentWidth()-20, true),
			)
		} else {
			line = lipgloss.JoinHorizontal(
				lipgloss.Left,
				ListItemStyle.Width(20).Render(name),
				m.renderChainStyled(chain, m.contentWidth()-20, false),
			)
		}
		routeLines = append(routeLines, m.fullWidth(line))
	}

	if len(routes) == 0 {
		if m.isOnAddTab() {
			routeLines = append(routeLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("Press [Enter] to create new profile"))
		} else {
			routeLines = append(routeLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("No routes configured"))
		}
	}

	// Build hints based on context
	var hints string
	if m.isOnAddTab() {
		hints = "[Enter] Create new profile   [←] Previous tab   [Esc] Back"
	} else if m.state.ProfileTabIndex == 0 {
		// Legacy tab
		hints = "[↑/↓] Navigate   [Enter] Edit   [a] Add   [⌫] Delete   [←/→] Switch Tab   [Esc] Back"
	} else {
		// Profile tab
		profileKey := m.getCurrentProfileKey()
		if profileKey == "default" {
			hints = "[↑/↓] Navigate   [Enter] Edit   [a] Add   [⌫] Delete   [P] Edit Profile   [←/→] Switch Tab   [Esc] Back"
		} else {
			hints = "[↑/↓] Navigate   [Enter] Edit   [a] Add   [⌫] Delete   [P] Edit Profile   [X] Delete Profile   [←/→] Switch Tab   [Esc] Back"
		}
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.fullWidth(title),
		m.blankLine(),
		m.fullWidth(tabs),
		m.fullWidth(tabDivider),
		headerRow,
		m.divider(),
		lipgloss.JoinVertical(lipgloss.Left, routeLines...),
		m.blankLine(),
		m.footline(hints),
	)

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// renderProfileTabs renders the profile tab bar.
// Legacy routes are auto-migrated to default profile in initProfileTabs.
func (m *WizardModel) renderProfileTabs() string {
	var tabs []string

	// Profile tabs (no separate legacy tab - legacy routes are auto-migrated to default)
	for i, key := range m.state.ProfileTabKeys {
		displayName := key
		if key == "default" {
			displayName = "default" + LaunchProfileIndicator.Render()
		}
		if i == m.state.ProfileTabIndex {
			tabs = append(tabs, TabActiveStyle.Render("["+displayName+"]"))
		} else {
			tabs = append(tabs, TabStyle.Render("["+displayName+"]"))
		}
	}

	// Add profile tab [+]
	addTabIndex := len(m.state.ProfileTabKeys)
	if m.state.ProfileTabIndex == addTabIndex {
		tabs = append(tabs, TabAddActiveStyle.Render("[+]"))
	} else {
		tabs = append(tabs, TabAddStyle.Render("[+]"))
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, tabs...)
}

// renderProfileEditModal renders the profile edit/create modal.
func (m *WizardModel) renderProfileEditModal() string {
	var title string
	if m.state.IsCreatingProfile {
		title = "Create New Profile"
	} else {
		profileKey := m.getCurrentProfileKey()
		title = "Edit Profile: " + profileKey
	}

	nameLabel := MenuItemDimmedStyle.Render("Name:")
	nameInput := m.state.EditProfileName
	if m.focusedField == 0 {
		nameInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(nameInput + "_")
	} else {
		nameInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(nameInput)
	}

	descLabel := MenuItemDimmedStyle.Render("Description:")
	descInput := m.state.EditProfileDesc
	if m.focusedField == 1 {
		descInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(descInput + "_")
	} else {
		descInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(descInput)
	}

	// Profile key info
	keyInfo := ""
	if m.state.IsCreatingProfile {
		previewKey := generateProfileKey(m.state.EditProfileName)
		keyInfo = MenuItemDimmedStyle.Render("(Profile key will be: " + previewKey + ")")
	} else {
		profileKey := m.getCurrentProfileKey()
		if profileKey == "default" {
			keyInfo = MenuItemDimmedStyle.Render("(Profile key: \"default\" - cannot be changed)")
		} else {
			keyInfo = MenuItemDimmedStyle.Render("(Profile key: " + profileKey + " - cannot be changed)")
		}
	}

	// Buttons
	var buttons string
	if m.focusedField == 2 {
		if m.state.IsCreatingProfile {
			buttons = lipgloss.JoinHorizontal(lipgloss.Left,
				ButtonSaveActiveStyle.Render("[Create]"),
				"  ",
				ButtonCancelStyle.Render("[Cancel]"),
			)
		} else {
			buttons = lipgloss.JoinHorizontal(lipgloss.Left,
				ButtonSaveActiveStyle.Render("[Save]"),
				"  ",
				ButtonCancelStyle.Render("[Cancel]"),
			)
		}
	} else {
		if m.state.IsCreatingProfile {
			buttons = lipgloss.JoinHorizontal(lipgloss.Left,
				ButtonSaveStyle.Render("[Create]"),
				"  ",
				ButtonCancelStyle.Render("[Cancel]"),
			)
		} else {
			buttons = lipgloss.JoinHorizontal(lipgloss.Left,
				ButtonSaveStyle.Render("[Save]"),
				"  ",
				ButtonCancelStyle.Render("[Cancel]"),
			)
		}
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		SectionHeaderStyle.Render(title),
		m.blankLine(),
		nameLabel,
		m.fullWidth(nameInput),
		m.blankLine(),
		descLabel,
		m.fullWidth(descInput),
		m.blankLine(),
		keyInfo,
		m.blankLine(),
		buttons,
		m.blankLine(),
		HelpTextStyle.Render("[Tab] Next field   [Enter] Confirm   [Esc] Cancel"),
	)

	return ProfileModalStyle.Render(content)
}

// renderMigrationModal renders the migration confirmation modal.
func (m *WizardModel) renderMigrationModal() string {
	title := "Create \"default\" Profile with Legacy Routes?"
	legacyCount := len(m.state.Config.Router.Routes)

	description := fmt.Sprintf("You have %d legacy routes. Creating \"default\" profile will", legacyCount)
	description2 := "copy these routes to the launch profile."

	// Buttons
	var buttons string
	if m.state.MigrationChoice == 0 {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonPrimaryStyle.Render("[Yes, copy routes]"),
			"  ",
			ButtonStyle.Render("[No, start empty]"),
			"  ",
			ButtonStyle.Render("[Cancel]"),
		)
	} else if m.state.MigrationChoice == 1 {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonStyle.Render("[Yes, copy routes]"),
			"  ",
			ButtonPrimaryStyle.Render("[No, start empty]"),
			"  ",
			ButtonStyle.Render("[Cancel]"),
		)
	} else {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonStyle.Render("[Yes, copy routes]"),
			"  ",
			ButtonStyle.Render("[No, start empty]"),
			"  ",
			ButtonPrimaryStyle.Render("[Cancel]"),
		)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		SectionHeaderStyle.Render(title),
		m.blankLine(),
		MenuItemDimmedStyle.Width(52).Render(description),
		MenuItemDimmedStyle.Width(52).Render(description2),
		m.blankLine(),
		buttons,
		m.blankLine(),
		HelpTextStyle.Render("[←/→] Choose   [Enter] Confirm"),
	)

	return ProfileModalStyle.Render(content)
}

// renderCreateProfile renders the full-screen profile creation view.
func (m *WizardModel) renderCreateProfile() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Create New Profile")

	// Name field
	nameLabel := MenuItemDimmedStyle.Render("Name:")
	nameInput := m.state.EditProfileName
	if m.focusedField == 0 {
		nameInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(nameInput + "_")
	} else {
		nameInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(nameInput)
	}

	// Description field
	descLabel := MenuItemDimmedStyle.Render("Description:")
	descInput := m.state.EditProfileDesc
	if m.focusedField == 1 {
		descInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(descInput + "_")
	} else {
		descInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(descInput)
	}

	// Profile key preview
	previewKey := generateProfileKey(m.state.EditProfileName)
	if previewKey == "" && m.state.EditProfileName != "" {
		previewKey = "(invalid name)"
	} else if previewKey == "" {
		previewKey = "(enter name to generate key)"
	}
	keyInfo := MenuItemDimmedStyle.Width(m.contentWidth()).Render("(Profile key will be: " + previewKey + ")")

	// Error message if any
	var errorLine string
	if m.state.ErrorMessage != "" {
		errorLine = ErrorStyle.Width(m.contentWidth()).Render("⚠ " + m.state.ErrorMessage)
	}

	// Buttons
	var buttons string
	if m.focusedField == 2 {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonSaveActiveStyle.Render("[Create]"),
			"  ",
			ButtonCancelStyle.Render("[Cancel]"),
		)
	} else if m.focusedField == 3 {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonSaveStyle.Render("[Create]"),
			"  ",
			ButtonCancelActiveStyle.Render("[Cancel]"),
		)
	} else {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonSaveStyle.Render("[Create]"),
			"  ",
			ButtonCancelStyle.Render("[Cancel]"),
		)
	}

	// Build content using responsive width helpers
	contentParts := []string{
		title,
		m.blankLine(),
		nameLabel,
		m.fullWidth(nameInput),
		m.blankLine(),
		descLabel,
		m.fullWidth(descInput),
		m.blankLine(),
		keyInfo,
	}

	if errorLine != "" {
		contentParts = append(contentParts,
			m.blankLine(),
			errorLine,
		)
	}

	contentParts = append(contentParts,
		m.blankLine(),
		m.blankLine(),
		m.fullWidth(buttons),
		m.blankLine(),
		m.footline("[Tab] Next   [Enter] Confirm   [Esc] Cancel"),
	)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)

	// Wrap in MainContainerStyle (responsive)
	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)

	return m.renderWithModal(mainBox)
}

// handleCreateProfileInput handles text input for profile creation.
func (m *WizardModel) handleCreateProfileInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle button navigation with left/right keys
	if m.focusedField == 2 {
		if msg.String() == "left" || msg.String() == "h" {
			// Already on Create button (first), stay there
			return m, nil
		}
		if msg.String() == "right" || msg.String() == "l" {
			// Move to Cancel button (second) - use focusedField 3 for cancel
			m.focusedField = 3
			return m, nil
		}
	}
	if m.focusedField == 3 {
		if msg.String() == "left" || msg.String() == "h" {
			m.focusedField = 2
			return m, nil
		}
		if msg.String() == "right" || msg.String() == "l" {
			// Already on Cancel button (second), stay there
			return m, nil
		}
	}

	// Handle text input for name field
	if m.focusedField == 0 {
		if msg.String() == "backspace" && len(m.state.EditProfileName) > 0 {
			m.state.EditProfileName = m.state.EditProfileName[:len(m.state.EditProfileName)-1]
		} else if msg.Paste {
			m.state.EditProfileName += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.EditProfileName += msg.String()
		}
	}

	// Handle text input for description field
	if m.focusedField == 1 {
		if msg.String() == "backspace" && len(m.state.EditProfileDesc) > 0 {
			m.state.EditProfileDesc = m.state.EditProfileDesc[:len(m.state.EditProfileDesc)-1]
		} else if msg.Paste {
			m.state.EditProfileDesc += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.EditProfileDesc += msg.String()
		}
	}

	return m, nil
}

// handleCreateProfileEnter handles Enter key for profile creation screen.
func (m *WizardModel) handleCreateProfileEnter() (tea.Model, tea.Cmd) {
	// Handle cancel button
	if m.focusedField == 3 {
		// Cancel profile creation, return to Routes screen
		m.state.IsCreatingProfile = false
		m.state.EditProfileName = ""
		m.state.EditProfileDesc = ""
		m.state.ErrorMessage = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenRoutes
		m.state.ProfileTabIndex = 0 // Return to first profile tab
		return m, nil
	}

	// Handle create button (focusedField 0, 1, or 2)
	name := strings.TrimSpace(m.state.EditProfileName)
	if name == "" {
		m.state.ErrorMessage = "Profile name is required"
		m.focusedField = 0 // Focus name field
		return m, nil
	}

	// Generate profile key and check for duplicates
	key := generateProfileKey(name)
	if key == "" {
		m.state.ErrorMessage = "Invalid profile name (must contain at least one alphanumeric character)"
		m.focusedField = 0
		return m, nil
	}

	// Check if profile key already exists
	if _, exists := m.state.Config.Router.Profiles[key]; exists {
		m.state.ErrorMessage = "Profile '" + key + "' already exists"
		m.focusedField = 0
		return m, nil
	}

	// Create new profile
	key = m.createNewProfile(name, m.state.EditProfileDesc)
	m.state.HasChanges = true

	// Reinitialize tabs and switch to new profile
	m.initProfileTabs()
	for i, k := range m.state.ProfileTabKeys {
		if k == key {
			m.state.ProfileTabIndex = i
			break
		}
	}

	// Clear state and return to Routes screen
	m.state.IsCreatingProfile = false
	m.state.EditProfileName = ""
	m.state.EditProfileDesc = ""
	m.state.ErrorMessage = ""
	m.focusedField = 0
	m.state.CurrentScreen = ScreenRoutes

	return m, nil
}

// renderEditProfile renders the full-screen profile edit view.
func (m *WizardModel) renderEditProfile() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Edit Profile: " + m.state.EditProfileKey)

	// Name field
	nameLabel := MenuItemDimmedStyle.Render("Name:")
	var nameInput string
	if m.state.EditProfileKey == "default" {
		nameInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(MenuItemDimmedStyle.Render("Default") + "  (locked)")
	} else if m.focusedField == 0 {
		nameInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(m.state.EditProfileName + "_")
	} else {
		nameInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(m.state.EditProfileName)
	}

	// Description field
	descLabel := MenuItemDimmedStyle.Render("Description:")
	descInput := m.state.EditProfileDesc
	if m.focusedField == 1 {
		descInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(descInput + "_")
	} else {
		descInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(descInput)
	}

	// Profile key info (live preview of derived key)
	previewKey := generateProfileKey(m.state.EditProfileName)
	if previewKey == "" && m.state.EditProfileName != "" {
		previewKey = "(invalid name)"
	} else if previewKey == "" {
		previewKey = m.state.EditProfileKey
	}
	keyInfo := MenuItemDimmedStyle.Width(m.contentWidth()).Render("(Profile key: " + previewKey + ")")

	// Error message if any
	var errorLine string
	if m.state.ErrorMessage != "" {
		errorLine = ErrorStyle.Width(m.contentWidth()).Render("⚠ " + m.state.ErrorMessage)
	}

	// Buttons
	var buttons string
	if m.focusedField == 2 {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonSaveActiveStyle.Render("[Save]"),
			"  ",
			ButtonCancelStyle.Render("[Cancel]"),
		)
	} else if m.focusedField == 3 {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonSaveStyle.Render("[Save]"),
			"  ",
			ButtonCancelActiveStyle.Render("[Cancel]"),
		)
	} else {
		buttons = lipgloss.JoinHorizontal(lipgloss.Left,
			ButtonSaveStyle.Render("[Save]"),
			"  ",
			ButtonCancelStyle.Render("[Cancel]"),
		)
	}

	// Build content using responsive width helpers
	contentParts := []string{
		title,
		m.blankLine(),
		nameLabel,
		m.fullWidth(nameInput),
		m.blankLine(),
		descLabel,
		m.fullWidth(descInput),
		m.blankLine(),
		keyInfo,
	}

	if errorLine != "" {
		contentParts = append(contentParts,
			m.blankLine(),
			errorLine,
		)
	}

	contentParts = append(contentParts,
		m.blankLine(),
		m.blankLine(),
		m.fullWidth(buttons),
		m.blankLine(),
		m.footline("[Tab] Next   [Enter] Confirm   [Esc] Cancel"),
	)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)

	// Wrap in MainContainerStyle (responsive)
	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)

	return m.renderWithModal(mainBox)
}

// handleEditProfileInput handles text input for profile edit screen.
func (m *WizardModel) handleEditProfileInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle button navigation with left/right keys
	if m.focusedField == 2 {
		if msg.String() == "left" || msg.String() == "h" {
			// Already on Save button (first), stay there
			return m, nil
		}
		if msg.String() == "right" || msg.String() == "l" {
			// Move to Cancel button (second) - use focusedField 3 for cancel
			m.focusedField = 3
			return m, nil
		}
	}
	if m.focusedField == 3 {
		if msg.String() == "left" || msg.String() == "h" {
			m.focusedField = 2
			return m, nil
		}
		if msg.String() == "right" || msg.String() == "l" {
			// Already on Cancel button (second), stay there
			return m, nil
		}
	}

	// Handle text input for name field
	if m.focusedField == 0 {
		if msg.String() == "backspace" && len(m.state.EditProfileName) > 0 {
			m.state.EditProfileName = m.state.EditProfileName[:len(m.state.EditProfileName)-1]
		} else if msg.Paste {
			m.state.EditProfileName += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.EditProfileName += msg.String()
		}
	}

	// Handle text input for description field
	if m.focusedField == 1 {
		if msg.String() == "backspace" && len(m.state.EditProfileDesc) > 0 {
			m.state.EditProfileDesc = m.state.EditProfileDesc[:len(m.state.EditProfileDesc)-1]
		} else if msg.Paste {
			m.state.EditProfileDesc += string(msg.Runes)
		} else if len(msg.String()) == 1 {
			m.state.EditProfileDesc += msg.String()
		}
	}

	return m, nil
}

// handleEditProfileEnter handles Enter key for profile edit screen.
func (m *WizardModel) handleEditProfileEnter() (tea.Model, tea.Cmd) {
	// Handle cancel button
	if m.focusedField == 3 {
		// Cancel profile editing, return to Routes screen
		m.state.EditProfileKey = ""
		m.state.EditProfileName = ""
		m.state.EditProfileDesc = ""
		m.state.ErrorMessage = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenRoutes
		return m, nil
	}

	// Handle save button (focusedField 0, 1, or 2)
	name := strings.TrimSpace(m.state.EditProfileName)
	profileKey := m.state.EditProfileKey

	// For "default" profile, name is immutable — always use "Default"
	if profileKey == "default" {
		name = "Default"
	} else if name == "" {
		m.state.ErrorMessage = "Profile name is required"
		m.focusedField = 0 // Focus name field
		return m, nil
	}

	// Update existing profile
	if profileKey != "" {
		profile := m.state.Config.Router.Profiles[profileKey]
		profile.Name = name
		profile.Description = m.state.EditProfileDesc

		// Derive new key from the updated name
		newKey := generateProfileKey(name)
		if newKey == "" {
			m.state.ErrorMessage = "Invalid profile name"
			m.focusedField = 0
			return m, nil
		}

		if newKey != profileKey {
			// Block renaming the "default" profile key
			if profileKey == "default" {
				m.state.ErrorMessage = "Cannot change the 'default' profile key"
				m.focusedField = 0
				return m, nil
			}
			// Check for duplicate key
			if _, exists := m.state.Config.Router.Profiles[newKey]; exists {
				m.state.ErrorMessage = "Profile '" + newKey + "' already exists"
				m.focusedField = 0
				return m, nil
			}
			// Re-key: delete old, insert under new key
			delete(m.state.Config.Router.Profiles, profileKey)
			profileKey = newKey
		}
		m.state.Config.Router.Profiles[profileKey] = profile
		m.state.HasChanges = true
	}

	// Reinitialize tabs to reflect any name changes
	m.initProfileTabs()
	// Find the current profile tab
	for i, k := range m.state.ProfileTabKeys {
		if k == profileKey {
			m.state.ProfileTabIndex = i
			break
		}
	}

	// Clear state and return to Routes screen
	m.state.EditProfileKey = ""
	m.state.EditProfileName = ""
	m.state.EditProfileDesc = ""
	m.state.ErrorMessage = ""
	m.focusedField = 0
	m.state.CurrentScreen = ScreenRoutes

	return m, nil
}

// Edit Route rendering
func (m *WizardModel) renderEditRoute() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Add/Edit Route")

	routeNameLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Route Name:")
	routeNameInput := m.state.EditRouteName
	if m.focusedField == 0 {
		indicator := " ▼"
		routeNameInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(routeNameInput + "_" + indicator)
	} else {
		routeNameInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(routeNameInput)
	}

	// Chain list
	var chainLines []string
	for i, target := range m.state.EditRouteChain {
		num := fmt.Sprintf("[%d]", i+1)
		display := fmt.Sprintf("%s:%s", target.Provider, target.Model)
		if target.Provider == "" {
			display = "(select provider)"
		} else if target.Model == "" {
			display = target.Provider + ": (select model)"
		}
		isSelected := m.focusedField == 1 && i == m.state.EditRouteChainCursor
		isInvalid := target.Provider != "" && target.Model != "" && !m.isValidProviderModel(target.Provider, target.Model)
		var style lipgloss.Style
		switch {
		case isSelected && isInvalid:
			style = ListItemInvalidSelectedStyle
		case isSelected:
			style = ListItemSelectedStyle
		case isInvalid:
			style = ListItemInvalidStyle
		default:
			style = ListItemStyle
		}
		item := lipgloss.JoinHorizontal(
			lipgloss.Left,
			style.Width(5).Render(num),
			style.Width(m.contentWidth() - 5).Render(display),
		)
		chainLines = append(chainLines, m.fullWidth(item))
	}

	if len(chainLines) == 0 {
		chainLines = append(chainLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("No providers in chain — press [a] to add"))
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		m.blankLine(),
		routeNameLabel,
		m.fullWidth(routeNameInput),
	)

	// Route name dropdown
	if m.state.ShowRouteNameDropdown && m.focusedField == 0 {
		routeNames := m.getRouteNameDropdownList()
		if len(routeNames) > 0 {
			var dropdownItems []string
			dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
			for i, name := range routeNames {
				if i == m.state.RouteNameDropdownCursor {
					dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(name))
				} else {
					dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(name))
				}
			}
			dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
				lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
			)
			content = lipgloss.JoinVertical(lipgloss.Left, content, dropdown)
		}
	}

	content = lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		m.blankLine(),
		m.divider(),
		m.blankLine(),
		MenuItemDimmedStyle.Width(m.contentWidth()).Render("Failover Chain:"),
		lipgloss.JoinVertical(lipgloss.Left, chainLines...),
	)

	// Provider dropdown
	if m.state.ShowDropdown && m.focusedField == 1 {
		providers := m.getChainProviderList()
		if len(providers) > 0 {
			var dropdownItems []string
			dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
			for i, p := range providers {
				if i == m.state.DropdownCursor {
					dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(p))
				} else {
					dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(p))
				}
			}
			dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
				lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
			)
			content = lipgloss.JoinVertical(lipgloss.Left, content, dropdown)
		}
	}

	// Model dropdown
	if m.state.ShowModelDropdown && m.focusedField == 1 {
		models := m.getChainModelList()
		if len(models) > 0 {
			var dropdownItems []string
			dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
			for i, model := range models {
				if i == m.state.ModelDropdownCursor {
					dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(model))
				} else {
					dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(model))
				}
			}
			dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
				lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
			)
			content = lipgloss.JoinVertical(lipgloss.Left, content, dropdown)
		}
	}

	// Hints bar
	content = lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		m.blankLine(),
	)

	if m.state.ShowRouteNameDropdown && m.focusedField == 0 {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.footline("[↑/↓] Select   [Enter] Pick   [Esc] Close   [Type] Filter"),
		)
	} else if m.state.ShowDropdown && m.focusedField == 1 {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.footline("[↑/↓] Select model   [Enter] Select   [Esc] Close"),
		)
	} else if m.focusedField == 1 {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.footline("[Esc] Back   [Tab] Next   [Enter] Save   [a] Add   [⌫] Delete"),
		)
	} else if m.focusedField == 0 {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.footline("[Esc] Back   [Tab] Next   [Enter] Show options"),
		)
	} else {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.footline("[Esc] Back   [Tab] Next   [Enter] Save"),
		)
	}

	if m.state.ErrorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage),
		)
	}

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// Server settings rendering
func (m *WizardModel) renderServer() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Proxy Settings")

	hostLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Host:")
	hostInput := m.state.ServerHost
	if m.focusedField == 0 {
		hostInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(hostInput + "_")
	} else {
		hostInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(hostInput)
	}

	portLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Port:")
	portInput := m.state.ServerPort
	if m.focusedField == 1 {
		portInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(portInput + "_")
	} else {
		portInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(portInput)
	}

	retriesLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Max Retries (failover):")
	retriesInput := m.state.ServerMaxRetries
	if m.focusedField == 2 {
		retriesInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(retriesInput + "_")
	} else {
		retriesInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(retriesInput)
	}

	retryDelayLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Retry Delay (e.g. 500ms, 1s):")
	retryDelayInput := m.state.ServerRetryDelay
	if m.focusedField == 3 {
		retryDelayInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(retryDelayInput + "_")
	} else {
		retryDelayInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(retryDelayInput)
	}

	autoRestartIdleLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Auto-Restart Idle (e.g. 30m, 2h; empty=off):")
	autoRestartIdleInput := m.state.ServerAutoRestartIdle
	if m.focusedField == 4 {
		autoRestartIdleInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(autoRestartIdleInput + "_")
	} else {
		autoRestartIdleInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(autoRestartIdleInput)
	}

	autoRestartWindowLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Auto-Restart Window (HH:MM-HH:MM; empty=always):")
	autoRestartWindowInput := m.state.ServerAutoRestartWindow
	if m.focusedField == 5 {
		autoRestartWindowInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(autoRestartWindowInput + "_")
	} else {
		autoRestartWindowInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(autoRestartWindowInput)
	}

	autoRestartTimezoneLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Auto-Restart Timezone (IANA, e.g. Asia/Shanghai; empty=Local):")
	autoRestartTimezoneInput := m.state.ServerAutoRestartTimezone
	if m.focusedField == 6 {
		autoRestartTimezoneInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(autoRestartTimezoneInput + "_")
	} else {
		autoRestartTimezoneInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(autoRestartTimezoneInput)
	}

	autoRestartBackoffLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Auto-Restart Backoff Max (e.g. 10m; empty=none):")
	autoRestartBackoffInput := m.state.ServerAutoRestartBackoffMax
	if m.focusedField == 7 {
		autoRestartBackoffInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(autoRestartBackoffInput + "_")
	} else {
		autoRestartBackoffInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(autoRestartBackoffInput)
	}

	note := MenuItemDimmedStyle.Render("Note: Port 1024-65535 · Durations use Go format · Window is HH:MM-HH:MM in configured tz")
	if m.state.PortTesting {
		note = lipgloss.JoinHorizontal(lipgloss.Left, note, "  ", StatusPendingStyle.Render("Testing port..."))
	} else if m.state.PortStatus != "" {
		var statusMsg string
		if strings.Contains(m.state.PortStatus, "PASS") {
			statusMsg = StatusOKStyle.Render("✓ " + m.state.PortStatus)
		} else {
			statusMsg = WarningStyle.Render("⚠ " + m.state.PortStatus)
		}
		note = lipgloss.JoinHorizontal(lipgloss.Left, note, "  ", statusMsg)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		m.blankLine(),
		hostLabel, m.fullWidth(hostInput),
		m.blankLine(),
		portLabel, m.fullWidth(portInput),
		m.blankLine(),
		retriesLabel, m.fullWidth(retriesInput),
		m.blankLine(),
		retryDelayLabel, m.fullWidth(retryDelayInput),
		m.blankLine(),
		autoRestartIdleLabel, m.fullWidth(autoRestartIdleInput),
		m.blankLine(),
		autoRestartWindowLabel, m.fullWidth(autoRestartWindowInput),
		m.blankLine(),
		autoRestartTimezoneLabel, m.fullWidth(autoRestartTimezoneInput),
		m.blankLine(),
		autoRestartBackoffLabel, m.fullWidth(autoRestartBackoffInput),
		m.blankLine(),
		note,
		m.blankLine(),
		m.footline("[Esc/Enter] Apply & Back   [Tab] Next field"),
	)

	if m.state.ErrorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage),
		)
	}

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// Logging settings rendering
func (m *WizardModel) renderLogging() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Logging Settings")

	// Enable Logging checkbox — use focused styles when field 0 is focused
	enabledCheckbox := CheckboxUncheckedStyle.Render()
	if m.state.LoggingEnabled {
		enabledCheckbox = CheckboxCheckedStyle.Render()
	}
	checkboxFocused := m.focusedField == 0
	if checkboxFocused {
		if m.state.LoggingEnabled {
			enabledCheckbox = CheckboxCheckedFocusedStyle.Render()
		} else {
			enabledCheckbox = CheckboxUncheckedFocusedStyle.Render()
		}
	}

	loggingDisabled := !m.state.LoggingEnabled

	// Level field — dimmed when logging is disabled
	levelLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Level:")
	levelValue := m.state.LoggingLevel
	if loggingDisabled {
		levelValue = InputFieldDisabledStyle.Width(m.inputFieldWidth()).Render(levelValue)
	} else if m.focusedField == 1 {
		levelValue = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(levelValue + " ▾")
	} else {
		levelValue = InputFieldStyle.Width(m.inputFieldWidth()).Render(levelValue)
	}

	// Destination field — dimmed when logging is disabled
	destLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Destination:")
	destValue := m.state.LoggingDestination
	if loggingDisabled {
		destValue = InputFieldDisabledStyle.Width(m.inputFieldWidth()).Render(destValue)
	} else if m.focusedField == 2 {
		destValue = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(destValue + " ▾")
	} else {
		destValue = InputFieldStyle.Width(m.inputFieldWidth()).Render(destValue)
	}

	// Build base content
	checkboxRow := lipgloss.JoinHorizontal(lipgloss.Left, enabledCheckbox, " Enable Logging")
	if checkboxFocused {
		checkboxRow = FocusedRowStyle.Width(m.contentWidth()).Render(checkboxRow)
	} else {
		checkboxRow = lipgloss.NewStyle().Padding(0, 1).Width(m.contentWidth()).Render(checkboxRow)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		m.blankLine(),
		checkboxRow,
		m.blankLine(),
		levelLabel,
		m.fullWidth(levelValue),
	)

	// Level dropdown
	if m.state.ShowLogLevelDropdown {
		var dropdownItems []string
		dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
		for i, level := range LogLevelOptions {
			if i == m.state.LogLevelDropdownCursor {
				dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(level))
			} else {
				dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(level))
			}
		}
		dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
			lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
		)
		content = lipgloss.JoinVertical(lipgloss.Left, content, dropdown)
	}

	content = lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		m.blankLine(),
		destLabel,
		m.fullWidth(destValue),
	)

	// Destination dropdown
	if m.state.ShowLogDestDropdown {
		var dropdownItems []string
		dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
		for i, dest := range LogDestinationOptions {
			if i == m.state.LogDestDropdownCursor {
				dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(dest))
			} else {
				dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(dest))
			}
		}
		dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
			lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
		)
		content = lipgloss.JoinVertical(lipgloss.Left, content, dropdown)
	}

	if m.state.LoggingDestination == "file" || m.focusedField == 3 {
		filePathValue := m.state.LoggingFilePath
		if filePathValue == "" {
			filePathValue = "0 (auto-generate)"
		}
		var filePathInput string
		if loggingDisabled {
			filePathInput = InputFieldDisabledStyle.Width(m.inputFieldWidth()).Render(filePathValue)
		} else if m.focusedField == 3 {
			filePathInput = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(filePathValue + "_")
		} else {
			filePathInput = InputFieldStyle.Width(m.inputFieldWidth()).Render(filePathValue)
		}
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			MenuItemDimmedStyle.Width(m.contentWidth()).Render("File Path (empty = auto):"),
			m.fullWidth(filePathInput),
			m.blankLine(),
			MenuItemDimmedStyle.Width(m.contentWidth()).Render("Instance logs: ~/.cc-modelrouter/logs/inst_<timestamp>.log"),
		)
	}

	// Context-sensitive hints
	var hints string
	if m.state.ShowLogLevelDropdown || m.state.ShowLogDestDropdown {
		hints = "[↑/↓] Select   [Enter] Apply   [Esc] Close"
	} else {
		hints = "[Esc/Enter] Apply & Back   [Tab] Next field   [Space] Toggle"
	}

	content = lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		m.blankLine(),
		m.footline(hints),
	)

	if m.state.ErrorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage),
		)
	}

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// View Config rendering
func (m *WizardModel) renderViewConfig() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Current Configuration (Read-only)")
	backHint := HelpTextStyle.Render("[Esc] Back")

	var configLines []string

	// Server
	configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("Server:"))
	configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  ├─ Host: "+m.state.Config.Server.Host))
	configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  └─ Port: "+strconv.Itoa(m.state.Config.Server.Port)))

	// Providers
	providerCount := len(m.state.Config.Providers)
	configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("Providers (%d):", providerCount)))
	for name, pc := range m.state.Config.Providers {
		models := strings.Join(pc.Models, ", ")
		configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  ├─ "+name))
		configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  │   ├─ URL: "+pc.BaseURL))
		configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  │   ├─ Transformer: "+pc.Transformer))
		configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  │   └─ Models: "+models))
	}

	// Profiles
	profileCount := len(m.state.Config.Router.Profiles)
	if profileCount > 0 {
		launchProfile := m.state.Config.GetDefaultProfile()
		if launchProfile == "" {
			launchProfile = "default"
		}
		if _, hasDefault := m.state.Config.Router.Profiles["default"]; hasDefault {
			launchProfile = "default" + LaunchProfileIndicator.Render()
		}
		configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("Profiles (%d):", profileCount)))
		profileNames := m.state.Config.GetProfileNames()
		sort.Strings(profileNames)
		for _, name := range profileNames {
			profile := m.state.Config.Router.Profiles[name]
			routeCount := len(profile.Routes)
			displayName := name
			if name == "default" {
				displayName = "default" + LaunchProfileIndicator.Render()
			}
			configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  ├─ "+displayName+": "+profile.Name))
			if profile.Description != "" {
				configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  │   ├─ Description: "+profile.Description))
				configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("  │   └─ Routes: %d configured", routeCount)))
			} else {
				configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("  │   └─ Routes: %d configured", routeCount)))
			}
		}
	}

	// Legacy Routes (only shown when no profiles or as fallback)
	routeCount := len(m.state.Config.Router.Routes)
	if routeCount > 0 {
		if profileCount > 0 {
			configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("Legacy Routes (%d): (fallback)", routeCount)))
		} else {
			configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render(fmt.Sprintf("Routes (%d):", routeCount)))
		}
		for name, chain := range m.state.Config.Router.Routes {
			configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  ├─ "+name+" → "+chain))
		}
	} else if profileCount == 0 {
		configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("Routes: (none configured)"))
	}

	// Logging
	configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("Logging:"))
	configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  ├─ Enabled: "+strconv.FormatBool(m.state.Config.Logging.Enabled)))
	configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  ├─ Level: "+m.state.Config.Logging.Level))
	configLines = append(configLines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("  └─ Destination: "+m.state.Config.Logging.Destination))

	closeBtn := ButtonStyle.Render("[Close]")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.fullWidth(title+"  "+backHint),
		m.blankLine(),
		lipgloss.JoinVertical(lipgloss.Left, configLines...),
		m.blankLine(),
		m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, closeBtn)),
		m.blankLine(),
		m.footline("[P] Export to file   [Esc] Close"),
	)

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// --- API Keys Screen ---

// openWizardDB opens the usage database for the wizard.
func openWizardDB() (*sql.DB, error) {
	dbPath, err := usage.DBPath()
	if err != nil {
		return nil, err
	}
	return usage.InitDB(dbPath)
}

// loadMultiUserSettings reads multi-user settings from SQLite into wizard state.
func (m *WizardModel) loadMultiUserSettings() {
	db, err := openWizardDB()
	if err != nil {
		// Fallback to defaults
		m.state.MultiUserEnabled = false
		m.state.MultiUserGlobalMax = "100"
		m.state.MultiUserWREDMin = "0.50"
		m.state.MultiUserWREDMax = "0.90"
		return
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	s, err := ks.GetSettings()
	if err != nil {
		m.state.MultiUserEnabled = false
		m.state.MultiUserGlobalMax = "100"
		m.state.MultiUserWREDMin = "0.50"
		m.state.MultiUserWREDMax = "0.90"
		return
	}

	m.state.MultiUserEnabled = s.Enabled
	if s.GlobalMaxConc > 0 {
		m.state.MultiUserGlobalMax = strconv.Itoa(s.GlobalMaxConc)
	} else {
		m.state.MultiUserGlobalMax = "100"
	}
	m.state.MultiUserWREDMin = fmt.Sprintf("%.2f", s.WREDMinDepth)
	if m.state.MultiUserWREDMin == "0.00" {
		m.state.MultiUserWREDMin = "0.50"
	}
	m.state.MultiUserWREDMax = fmt.Sprintf("%.2f", s.WREDMaxDepth)
	if m.state.MultiUserWREDMax == "0.00" {
		m.state.MultiUserWREDMax = "0.90"
	}
}

// loadKeysData loads API keys and groups from the database.
func (m *WizardModel) loadKeysData() {
	db, err := openWizardDB()
	if err != nil {
		m.state.KeysList = nil
		return
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	keys, err := ks.ListKeys()
	if err != nil {
		m.state.KeysList = nil
		return
	}
	m.state.KeysList = keys

	groups, err := ks.ListGroups()
	if err != nil {
		m.state.NewKeyGroups = nil
		return
	}
	m.state.NewKeyGroups = groups
}

// loadGroupsData loads user groups and their member counts from the database.
func (m *WizardModel) loadGroupsData() {
	db, err := openWizardDB()
	if err != nil {
		m.state.GroupsList = nil
		m.state.GroupsMemberCounts = nil
		return
	}
	defer db.Close()

	ks := auth.NewKeyStore(db)
	groupsWithCounts, err := ks.ListGroupsWithMemberCounts()
	if err != nil {
		m.state.GroupsList = nil
		m.state.GroupsMemberCounts = nil
		return
	}

	groups := make([]*auth.GroupInfo, len(groupsWithCounts))
	memberCounts := make(map[int64]int, len(groupsWithCounts))
	for i, gc := range groupsWithCounts {
		groups[i] = &gc.GroupInfo
		memberCounts[gc.ID] = gc.MemberCount
	}
	m.state.GroupsList = groups
	m.state.GroupsMemberCounts = memberCounts

	// Snapshot base state for two-phase commit
	m.state.GroupsSnapshot = make([]*auth.GroupInfo, len(groups))
	for i, g := range groups {
		cp := *g
		m.state.GroupsSnapshot[i] = &cp
	}
	m.state.GroupsPendingOps = nil
}

// groupsNextTempID returns the next negative temporary ID for in-memory creates.
func (m *WizardModel) groupsNextTempID() int64 {
	min := int64(-1)
	for _, op := range m.state.GroupsPendingOps {
		if op.OpType == 0 && op.ID < min {
			min = op.ID
		}
	}
	return min - 1
}

// effectiveGroupsList applies pending ops on the snapshot and returns the result sorted by name.
func (m *WizardModel) effectiveGroupsList() []*auth.GroupInfo {
	// Build a map from snapshot for O(1) lookup
	base := make(map[int64]*auth.GroupInfo)
	for _, g := range m.state.GroupsSnapshot {
		cp := *g
		base[cp.ID] = &cp
	}

	// Collect final IDs and their resolved GroupInfo
	result := make(map[int64]*auth.GroupInfo)
	deleted := make(map[int64]bool)

	for _, op := range m.state.GroupsPendingOps {
		switch op.OpType {
		case 0: // create
			result[op.ID] = &auth.GroupInfo{
				ID:             op.ID,
				Name:           op.Name,
				Profile:        op.Profile,
				PriorityWeight: op.PriorityWeight,
				MaxConcurrency: op.MaxConcurrency,
			}
		case 1: // update
			if existing, ok := result[op.ID]; ok {
				existing.Profile = op.Profile
				existing.PriorityWeight = op.PriorityWeight
				existing.MaxConcurrency = op.MaxConcurrency
			} else if snap, ok := base[op.ID]; ok {
				cp := *snap
				cp.Profile = op.Profile
				cp.PriorityWeight = op.PriorityWeight
				cp.MaxConcurrency = op.MaxConcurrency
				result[op.ID] = &cp
			}
		case 2: // delete
			delete(result, op.ID)
			deleted[op.ID] = true
		}
	}

	// Add snapshot items not touched by any op
	for id, g := range base {
		if _, wasDeleted := deleted[id]; !wasDeleted {
			if _, hasOp := result[id]; !hasOp {
				cp := *g
				result[id] = &cp
			}
		}
	}

	// Sort by name
	out := make([]*auth.GroupInfo, 0, len(result))
	for _, g := range result {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// groupsHavePendingChanges returns true if there are staged group operations.
func (m *WizardModel) groupsHavePendingChanges() bool {
	return len(m.state.GroupsPendingOps) > 0
}

// flushGroupChanges writes all pending group operations to SQLite and clears the list.
func (m *WizardModel) flushGroupChanges(ks *auth.KeyStore) error {
	for _, op := range m.state.GroupsPendingOps {
		switch op.OpType {
		case 0: // create
			if _, err := ks.CreateGroup(op.Name, op.Profile, op.PriorityWeight, op.MaxConcurrency); err != nil {
				return fmt.Errorf("create group %q: %w", op.Name, err)
			}
		case 1: // update
			if err := ks.UpdateGroup(op.ID, op.Profile, op.PriorityWeight, op.MaxConcurrency); err != nil {
				return fmt.Errorf("update group (id %d): %w", op.ID, err)
			}
		case 2: // delete
			if err := ks.DeleteGroup(op.ID); err != nil {
				return fmt.Errorf("delete group (id %d): %w", op.ID, err)
			}
		}
	}
	m.state.GroupsPendingOps = nil
	return nil
}

// discardGroupChanges clears pending ops and rebuilds GroupsList from the snapshot.
func (m *WizardModel) discardGroupChanges() {
	m.state.GroupsPendingOps = nil
	if m.state.GroupsSnapshot != nil {
		m.state.GroupsList = make([]*auth.GroupInfo, len(m.state.GroupsSnapshot))
		for i, g := range m.state.GroupsSnapshot {
			cp := *g
			m.state.GroupsList[i] = &cp
		}
		sort.Slice(m.state.GroupsList, func(i, j int) bool {
			return m.state.GroupsList[i].Name < m.state.GroupsList[j].Name
		})
		m.state.GroupsCursor = 0
	} else {
		m.state.GroupsList = nil
	}
}

// refreshGroupsDisplay rebuilds GroupsList from effective state and clamps cursor.
func (m *WizardModel) refreshGroupsDisplay() {
	m.state.GroupsList = m.effectiveGroupsList()
	if m.state.GroupsCursor >= len(m.state.GroupsList) {
		m.state.GroupsCursor = 0
		if len(m.state.GroupsList) > 0 {
			m.state.GroupsCursor = len(m.state.GroupsList) - 1
		}
	}
}

// loadGroupProfileNames reads profile names from config into a sorted slice.
func (m *WizardModel) loadGroupProfileNames() {
	m.state.NewGroupProfileNames = m.state.Config.GetProfileNames()
}

func (m *WizardModel) handleAPIKeysEnter() (tea.Model, tea.Cmd) {
	if m.state.CreatedRawKey != "" {
		// Dismiss the "key created" display and reload
		m.state.CreatedRawKey = ""
		m.state.KeyShowConfirm = false
		m.loadKeysData()
		return m, nil
	}

	if len(m.state.KeysList) == 0 {
		// No keys — go to create screen
		m.state.NewKeyName = ""
		m.state.NewKeyGroup = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenCreateAPIKey
		return m, nil
	}

	// Create button row selected
	if m.state.KeysCursor == len(m.state.KeysList) {
		m.state.NewKeyName = ""
		m.state.NewKeyGroup = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenCreateAPIKey
		return m, nil
	}

	// Revoke selected key
	selected := m.state.KeysList[m.state.KeysCursor]
	if !selected.IsActive {
		return m, nil
	}

	m.state.ShowConfirm = true
	m.state.ConfirmCursor = 1 // Default to No (destructive)
	m.state.ConfirmMessage = fmt.Sprintf("Revoke key %s (%s)?", selected.KeyPrefix, selected.UserName)
	m.state.ConfirmAction = func() bool {
		db, err := openWizardDB()
		if err != nil {
			m.state.ErrorMessage = fmt.Sprintf("Failed to open DB: %v", err)
			return false
		}
		defer db.Close()
		ks := auth.NewKeyStore(db)
		if err := ks.RevokeKey(selected.KeyID); err != nil {
			m.state.ErrorMessage = fmt.Sprintf("Failed to revoke: %v", err)
			return false
		}
		m.loadKeysData()
		// Clamp cursor after list refresh
		if m.state.KeysCursor >= len(m.state.KeysList) && len(m.state.KeysList) > 0 {
			m.state.KeysCursor = len(m.state.KeysList) - 1
		}
		return false
	}
	return m, nil
}

func (m *WizardModel) handleAPIKeysDelete() (tea.Model, tea.Cmd) {
	if len(m.state.KeysList) == 0 {
		return m, nil
	}
	onCreateBtn := m.state.KeysCursor == len(m.state.KeysList)
	if onCreateBtn {
		return m, nil
	}
	selected := m.state.KeysList[m.state.KeysCursor]
	if selected.IsActive {
		return m, nil
	}

	m.state.ShowConfirm = true
	m.state.ConfirmCursor = 1 // Default to No (destructive)
	m.state.ConfirmMessage = fmt.Sprintf("Permanently delete key %s (%s)?", selected.KeyPrefix, selected.UserName)
	m.state.ConfirmAction = func() bool {
		db, err := openWizardDB()
		if err != nil {
			m.state.ErrorMessage = fmt.Sprintf("Failed to open DB: %v", err)
			return false
		}
		defer db.Close()
		ks := auth.NewKeyStore(db)
		if err := ks.DeleteKey(selected.KeyID); err != nil {
			m.state.ErrorMessage = fmt.Sprintf("Failed to delete: %v", err)
			return false
		}
		m.loadKeysData()
		// Clamp cursor after list refresh
		if m.state.KeysCursor >= len(m.state.KeysList) && len(m.state.KeysList) > 0 {
			m.state.KeysCursor = len(m.state.KeysList) - 1
		}
		return false
	}
	return m, nil
}

func (m *WizardModel) handleAPIKeysRegenerate() (tea.Model, tea.Cmd) {
	if len(m.state.KeysList) == 0 {
		return m, nil
	}
	onCreateBtn := m.state.KeysCursor == len(m.state.KeysList)
	if onCreateBtn {
		return m, nil
	}
	selected := m.state.KeysList[m.state.KeysCursor]
	if selected.IsActive {
		return m, nil
	}

	db, err := openWizardDB()
	if err != nil {
		m.state.ErrorMessage = fmt.Sprintf("Failed to open DB: %v", err)
		return m, nil
	}
	defer db.Close()
	ks := auth.NewKeyStore(db)

	group, err := ks.GetGroupByName(selected.GroupName)
	if err != nil {
		m.state.ErrorMessage = fmt.Sprintf("Failed to find group: %v", err)
		return m, nil
	}
	if group == nil {
		m.state.ErrorMessage = fmt.Sprintf("Group %q not found", selected.GroupName)
		return m, nil
	}

	rawKey, _, err := ks.CreateKey(selected.UserName, group.ID)
	if err != nil {
		m.state.ErrorMessage = fmt.Sprintf("Failed to create key: %v", err)
		return m, nil
	}

	// Remove the old revoked key — the new one replaces it
	ks.DeleteKey(selected.KeyID)

	m.state.CreatedRawKey = rawKey
	m.state.KeyShowConfirm = true
	m.loadKeysData()
	return m, nil
}

func (m *WizardModel) renderAPIKeys() string {
	title := TitleStyle.Width(m.contentWidth()).Render("Users API Keys")

	var lines []string

	if m.state.CreatedRawKey != "" {
		lines = append(lines, "")
		lines = append(lines, StatusOKStyle.Bold(true).Render("API Key Created"))
		lines = append(lines, "")
		lines = append(lines, HelpTextStyle.Render("Save this key now — it cannot be retrieved again:"))
		lines = append(lines, "")
		raw := m.state.CreatedRawKey
		for i := 0; i < len(raw); i += 32 {
			end := i + 32
			if end > len(raw) {
				end = len(raw)
			}
			lines = append(lines, MenuItemSelectedStyle.Bold(true).Render(raw[i:end]))
		}
		lines = append(lines, "")
		closeBtn := ButtonPrimaryStyle.Render(" [OK] ")
		lines = append(lines, m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, closeBtn)))
	} else if len(m.state.KeysList) == 0 {
		lines = append(lines, "")
		lines = append(lines, MenuItemDimmedStyle.Render("No API keys found."))
		lines = append(lines, "")
		createBtn := ButtonPrimaryStyle.Render("[Create Key]")
		lines = append(lines, m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, createBtn)))
	} else {
		leftHeader := fmt.Sprintf("%-12s %-14s %-14s", "PREFIX", "USER", "GROUP")
		rightHeader := fmt.Sprintf("%6s   %-16s", "ACTIVE", "LAST USED")
		innerW := m.contentWidth() - 2
		gap := innerW - lipgloss.Width(leftHeader) - lipgloss.Width(rightHeader)
		if gap < 4 {
			gap = 4
		}
		header := TableHeaderStyle.Width(m.contentWidth()).Render(
			leftHeader + strings.Repeat(" ", gap) + rightHeader,
		)
		lines = append(lines, header)

		for i, k := range m.state.KeysList {
			lastUsed := "never"
			if k.LastUsed != nil {
				lastUsed = k.LastUsed.Format("2006-01-02 15:04")
			}
			active := "yes"
			if !k.IsActive {
				active = "no"
			}
			name := k.UserName
			if name == "" {
				name = "(unnamed)"
			}
			group := k.GroupName
			if group == "" {
				group = "(none)"
			}
			leftData := fmt.Sprintf("%-12s %-14s %-14s", k.KeyPrefix, name, group)
			rightData := fmt.Sprintf("%6s   %-16s", active, lastUsed)
			dataGap := innerW - lipgloss.Width(leftData) - lipgloss.Width(rightData)
			if dataGap < 4 {
				dataGap = 4
			}
			row := leftData + strings.Repeat(" ", dataGap) + rightData

			if i == m.state.KeysCursor {
				row = ListItemSelectedStyle.Width(m.contentWidth()).Render(row)
			} else if !k.IsActive {
				row = ListItemInvalidStyle.Width(m.contentWidth()).Render(row)
			} else {
				row = ListItemStyle.Width(m.contentWidth()).Render(row)
			}
			lines = append(lines, row)
		}

		lines = append(lines, "")
		// Create button row — active when cursor is on it
		var createBtn string
		if m.state.KeysCursor == len(m.state.KeysList) {
			createBtn = ButtonPrimaryStyle.Render("[Create Key]")
		} else {
			createBtn = ButtonStyle.Render("[Create Key]")
		}
		lines = append(lines, m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, createBtn)))
		lines = append(lines, "")
	}

	// Context-sensitive hint bar
	var hint string
	onCreateBtn := len(m.state.KeysList) > 0 && m.state.KeysCursor == len(m.state.KeysList)
	if onCreateBtn {
		hint = "[Enter] Create   [↑/↓] Navigate   [Esc] Back"
	} else if len(m.state.KeysList) > 0 && m.state.KeysCursor < len(m.state.KeysList) {
		selected := m.state.KeysList[m.state.KeysCursor]
		if selected.IsActive {
			hint = "[Enter] Revoke   [c] Create   [↑/↓] Navigate   [Esc] Back"
		} else {
			hint = "[d] Delete   [r] Regenerate   [c] Create   [↑/↓] Navigate   [Esc] Back"
		}
	} else {
		hint = "[Enter] Select   [Esc] Back"
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.fullWidth(title),
		m.blankLine(),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
		m.blankLine(),
		m.footline(hint),
	)

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// --- Create API Key Screen ---

func (m *WizardModel) handleCreateAPIKeyEnter() (tea.Model, tea.Cmd) {
	switch m.focusedField {
	case 1: // Group dropdown
		if m.state.ShowKeyGroupDropdown {
			if m.state.KeyGroupDropdownCursor < len(m.state.NewKeyGroups) {
				m.state.NewKeyGroup = m.state.NewKeyGroups[m.state.KeyGroupDropdownCursor].Name
			}
			m.state.ShowKeyGroupDropdown = false
			m.state.KeyGroupDropdownCursor = 0
			return m, nil
		}
		m.state.ShowKeyGroupDropdown = true
		m.state.KeyGroupDropdownCursor = 0
		for i, g := range m.state.NewKeyGroups {
			if g.Name == m.state.NewKeyGroup {
				m.state.KeyGroupDropdownCursor = i
				break
			}
		}
		return m, nil
	case 2: // Create button
		if m.state.NewKeyName == "" {
			m.state.ErrorMessage = "Key name is required"
			return m, nil
		}
		for _, k := range m.state.KeysList {
			if strings.EqualFold(k.UserName, m.state.NewKeyName) {
				m.state.ErrorMessage = "A key with this user name already exists"
				return m, nil
			}
		}
		if m.state.NewKeyGroup == "" {
			m.state.ErrorMessage = "Group is required"
			return m, nil
		}

		db, err := openWizardDB()
		if err != nil {
			m.state.ErrorMessage = fmt.Sprintf("Failed to open DB: %v", err)
			return m, nil
		}
		defer db.Close()

		ks := auth.NewKeyStore(db)
		g, err := ks.GetGroupByName(m.state.NewKeyGroup)
		if err != nil {
			m.state.ErrorMessage = fmt.Sprintf("Failed to look up group: %v", err)
			return m, nil
		}
		if g == nil {
			m.state.ErrorMessage = fmt.Sprintf("Group not found: %s", m.state.NewKeyGroup)
			return m, nil
		}

		rawKey, _, err := ks.CreateKey(m.state.NewKeyName, g.ID)
		if err != nil {
			m.state.ErrorMessage = fmt.Sprintf("Failed to create key: %v", err)
			return m, nil
		}

		m.state.CreatedRawKey = rawKey
		m.state.KeyShowConfirm = true
		m.state.ErrorMessage = ""
		m.state.CurrentScreen = ScreenAPIKeys
		m.loadKeysData()
		return m, nil
	}
	return m, nil
}

func (m *WizardModel) handleCreateAPIKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.focusedField {
	case 0: // Name
		if msg.Type == tea.KeyBackspace {
			if len(m.state.NewKeyName) > 0 {
				m.state.NewKeyName = m.state.NewKeyName[:len(m.state.NewKeyName)-1]
			}
			return m, nil
		}
		if len(msg.String()) == 1 && len(m.state.NewKeyName) < 64 {
			m.state.NewKeyName += msg.String()
		}
	case 1: // Group dropdown
		if m.state.ShowKeyGroupDropdown {
			if len(msg.String()) == 1 {
				for i, g := range m.state.NewKeyGroups {
					if len(g.Name) > 0 && strings.EqualFold(string(g.Name[0]), msg.String()) {
						m.state.KeyGroupDropdownCursor = i
						return m, nil
					}
				}
			}
		}
	}
	return m, nil
}

func (m *WizardModel) renderCreateAPIKey() string {
	title := TitleStyle.Width(m.contentWidth()).Render("Create API Key")

	var lines []string
	lines = append(lines, "")

	// Name field
	nameLabel := "User Name:"
	nameStyle := InputFieldStyle
	if m.focusedField == 0 {
		nameStyle = InputFieldFocusedStyle
	}
	lines = append(lines, nameLabel)
	lines = append(lines, nameStyle.Width(m.inputFieldWidth()).Render(m.state.NewKeyName))
	if m.focusedField == 0 {
		lines = append(lines, HelpTextStyle.Render("Type a descriptive name for this key"))
	}
	lines = append(lines, "")

	// Group selector
	groupLabel := "Group:"
	groupStyle := InputFieldStyle
	if m.focusedField == 1 {
		groupStyle = InputFieldFocusedStyle
	}
	lines = append(lines, groupLabel)

	if len(m.state.NewKeyGroups) > 0 {
		indicator := "  "
		if m.focusedField == 1 {
			indicator = "▾ "
		}
		lines = append(lines, groupStyle.Width(m.inputFieldWidth()).Render(indicator+m.state.NewKeyGroup))

		if m.state.ShowKeyGroupDropdown && m.focusedField == 1 {
			var dropdownItems []string
			dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
			for i, g := range m.state.NewKeyGroups {
				if i == m.state.KeyGroupDropdownCursor {
					dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(g.Name))
				} else {
					dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(g.Name))
				}
			}
			dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
				lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
			)
			lines = append(lines, dropdown)
		}
	} else {
		lines = append(lines, groupStyle.Width(m.inputFieldWidth()).Render("No groups — run 'ccrouter groups create' first"))
	}
	if m.focusedField == 1 {
		lines = append(lines, HelpTextStyle.Render("[Enter] Select group"))
	}
	lines = append(lines, "")

	// Create button
	createStyle := ButtonStyle
	if m.focusedField == 2 {
		createStyle = ButtonActiveStyle
	}
	createBtn := createStyle.Render(" [Create] ")
	lines = append(lines, m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, createBtn)))

	if m.state.ErrorMessage != "" {
		lines = append(lines, "")
		lines = append(lines, ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage))
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.fullWidth(title),
		m.blankLine(),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
		m.blankLine(),
		m.footline("[Tab] Next field   [Enter] Action   [Esc] Back"),
	)

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// --- Multi-User Screen ---

func (m *WizardModel) handleMultiUserInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.focusedField {
	case 0: // Checkbox
		if msg.String() == " " {
			m.state.MultiUserEnabled = !m.state.MultiUserEnabled
		}
	case 1: // Global Max Concurrency
		if msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del" {
			if len(m.state.MultiUserGlobalMax) > 0 {
				m.state.MultiUserGlobalMax = m.state.MultiUserGlobalMax[:len(m.state.MultiUserGlobalMax)-1]
			}
			return m, nil
		}
		if len(msg.String()) == 1 && msg.String() >= "0" && msg.String() <= "9" {
			if len(m.state.MultiUserGlobalMax) < 6 {
				m.state.MultiUserGlobalMax += msg.String()
			}
		}
	case 2: // WRED Min Depth
		if msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del" {
			if len(m.state.MultiUserWREDMin) > 0 {
				m.state.MultiUserWREDMin = m.state.MultiUserWREDMin[:len(m.state.MultiUserWREDMin)-1]
			}
			return m, nil
		}
		if len(msg.String()) == 1 && ((msg.String() >= "0" && msg.String() <= "9") || msg.String() == ".") {
			if len(m.state.MultiUserWREDMin) < 4 {
				m.state.MultiUserWREDMin += msg.String()
			}
		}
	case 3: // WRED Max Depth
		if msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del" {
			if len(m.state.MultiUserWREDMax) > 0 {
				m.state.MultiUserWREDMax = m.state.MultiUserWREDMax[:len(m.state.MultiUserWREDMax)-1]
			}
			return m, nil
		}
		if len(msg.String()) == 1 && ((msg.String() >= "0" && msg.String() <= "9") || msg.String() == ".") {
			if len(m.state.MultiUserWREDMax) < 4 {
				m.state.MultiUserWREDMax += msg.String()
			}
		}
	}
	return m, nil
}

func (m *WizardModel) handleMultiUserSave() (tea.Model, tea.Cmd) {
	// Settings stay in-memory; written to SQLite on "Save & Exit"
	m.state.HasChanges = true
	// Update snapshot so Cancel restores to these values
	m.state.MultiUserOrigEnabled = m.state.MultiUserEnabled
	m.state.MultiUserOrigGlobalMax = m.state.MultiUserGlobalMax
	m.state.MultiUserOrigWREDMin = m.state.MultiUserWREDMin
	m.state.MultiUserOrigWREDMax = m.state.MultiUserWREDMax
	m.state.ProviderCursor = m.state.MainMenuCursor
	m.state.ErrorMessage = ""
	m.state.CurrentScreen = ScreenMainMenu
	return m, nil
}

func (m *WizardModel) handleMultiUserCancel() (tea.Model, tea.Cmd) {
	// Restore snapshot values — discard any edits
	m.state.MultiUserEnabled = m.state.MultiUserOrigEnabled
	m.state.MultiUserGlobalMax = m.state.MultiUserOrigGlobalMax
	m.state.MultiUserWREDMin = m.state.MultiUserOrigWREDMin
	m.state.MultiUserWREDMax = m.state.MultiUserOrigWREDMax
	m.discardGroupChanges()
	m.state.ProviderCursor = m.state.MainMenuCursor
	m.state.ErrorMessage = ""
	m.state.CurrentScreen = ScreenMainMenu
	return m, nil
}

// --- Groups List Screen ---

func (m *WizardModel) handleGroupsEnter() (tea.Model, tea.Cmd) {
	if len(m.state.GroupsList) > 0 {
		return m.handleGroupsEdit()
	}
	// No groups — go to create screen
	return m.handleGroupsAdd()
}

func (m *WizardModel) handleGroupsAdd() (tea.Model, tea.Cmd) {
	m.state.NewGroupName = ""
	m.state.NewGroupProfile = ""
	m.state.NewGroupPriority = "0.50"
	m.state.NewGroupMaxConc = "10"
	m.loadGroupProfileNames()
	if len(m.state.NewGroupProfileNames) > 0 {
		m.state.NewGroupProfile = m.state.NewGroupProfileNames[0]
	}
	m.state.ShowGroupProfileDropdown = false
	m.state.NewGroupProfileDropdownCursor = 0
	m.state.EditingGroupID = 0
	m.state.ErrorMessage = ""
	m.focusedField = 0
	m.state.CurrentScreen = ScreenCreateGroup
	return m, nil
}

func (m *WizardModel) handleGroupsEdit() (tea.Model, tea.Cmd) {
	if len(m.state.GroupsList) == 0 {
		return m, nil
	}
	selected := m.state.GroupsList[m.state.GroupsCursor]
	m.state.NewGroupName = selected.Name
	m.state.NewGroupProfile = selected.Profile
	m.state.NewGroupPriority = fmt.Sprintf("%.2f", selected.PriorityWeight)
	m.state.NewGroupMaxConc = strconv.Itoa(selected.MaxConcurrency)
	m.loadGroupProfileNames()
	m.state.ShowGroupProfileDropdown = false
	m.state.NewGroupProfileDropdownCursor = 0
	m.state.EditingGroupID = selected.ID
	m.state.ErrorMessage = ""
	// Start at field 1 (skip locked name)
	m.focusedField = 1
	m.state.CurrentScreen = ScreenCreateGroup
	return m, nil
}

func (m *WizardModel) handleGroupsDelete() (tea.Model, tea.Cmd) {
	if len(m.state.GroupsList) == 0 {
		return m, nil
	}
	selected := m.state.GroupsList[m.state.GroupsCursor]

	// Refuse to delete groups that have members
	if m.state.GroupsMemberCounts != nil && m.state.GroupsMemberCounts[selected.ID] > 0 {
		m.state.ErrorMessage = "Cannot delete group with assigned keys"
		return m, nil
	}

	m.state.ShowConfirm = true
	m.state.ConfirmCursor = 1 // Default to No (destructive)
	m.state.ConfirmMessage = fmt.Sprintf("Delete group \"%s\"?", selected.Name)
	groupID := selected.ID
	m.state.ConfirmAction = func() bool {
		// If a pending create exists for this ID, just remove it (never hit SQLite)
		created := false
		for i, op := range m.state.GroupsPendingOps {
			if op.OpType == 0 && op.ID == groupID {
				m.state.GroupsPendingOps = append(m.state.GroupsPendingOps[:i], m.state.GroupsPendingOps[i+1:]...)
				created = true
				break
			}
		}
		if !created {
			// Remove any pending update for this ID (superseded by delete)
			for i := len(m.state.GroupsPendingOps) - 1; i >= 0; i-- {
				if m.state.GroupsPendingOps[i].OpType == 1 && m.state.GroupsPendingOps[i].ID == groupID {
					m.state.GroupsPendingOps = append(m.state.GroupsPendingOps[:i], m.state.GroupsPendingOps[i+1:]...)
				}
			}
			// Append delete op
			m.state.GroupsPendingOps = append(m.state.GroupsPendingOps, PendingGroupOp{
				OpType: 2,
				ID:     groupID,
			})
		}
		m.state.HasChanges = true
		m.refreshGroupsDisplay()
		return false
	}
	return m, nil
}

// --- Create/Edit Group Screen ---

func (m *WizardModel) handleCreateGroupInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Profile dropdown navigation
	if m.state.ShowGroupProfileDropdown {
		if msg.Type == tea.KeyUp || msg.String() == "k" {
			if len(m.state.NewGroupProfileNames) > 0 {
				m.state.NewGroupProfileDropdownCursor = (m.state.NewGroupProfileDropdownCursor - 1 + len(m.state.NewGroupProfileNames)) % len(m.state.NewGroupProfileNames)
			}
			return m, nil
		}
		if msg.Type == tea.KeyDown || msg.String() == "j" {
			if len(m.state.NewGroupProfileNames) > 0 {
				m.state.NewGroupProfileDropdownCursor = (m.state.NewGroupProfileDropdownCursor + 1) % len(m.state.NewGroupProfileNames)
			}
			return m, nil
		}
		if msg.Type == tea.KeyEnter {
			if m.state.NewGroupProfileDropdownCursor < len(m.state.NewGroupProfileNames) {
				m.state.NewGroupProfile = m.state.NewGroupProfileNames[m.state.NewGroupProfileDropdownCursor]
			}
			m.state.ShowGroupProfileDropdown = false
			m.state.NewGroupProfileDropdownCursor = 0
			return m, nil
		}
		if msg.Type == tea.KeyEscape {
			m.state.ShowGroupProfileDropdown = false
			m.state.NewGroupProfileDropdownCursor = 0
			return m, nil
		}
		return m, nil
	}

	// Button navigation (left/right on Save/Cancel buttons)
	if m.focusedField == 4 || m.focusedField == 5 {
		if msg.String() == "left" || msg.String() == "h" {
			m.focusedField = 4
			return m, nil
		}
		if msg.String() == "right" || msg.String() == "l" {
			m.focusedField = 5
			return m, nil
		}
		if msg.Type == tea.KeyEnter {
			return m.handleCreateGroupEnter()
		}
		return m, nil
	}

	switch m.focusedField {
	case 0: // Group name (locked when editing)
		if m.state.EditingGroupID != 0 {
			return m, nil
		}
		if msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del" {
			if len(m.state.NewGroupName) > 0 {
				m.state.NewGroupName = m.state.NewGroupName[:len(m.state.NewGroupName)-1]
			}
			return m, nil
		}
		if len(msg.String()) == 1 && len(m.state.NewGroupName) < 32 {
			m.state.NewGroupName += msg.String()
		}
	case 1: // Profile (opens dropdown on enter, handled in handleEnter)
		return m, nil
	case 2: // Priority weight
		if msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del" {
			if len(m.state.NewGroupPriority) > 0 {
				m.state.NewGroupPriority = m.state.NewGroupPriority[:len(m.state.NewGroupPriority)-1]
			}
			return m, nil
		}
		if len(msg.String()) == 1 && ((msg.String() >= "0" && msg.String() <= "9") || msg.String() == ".") {
			if len(m.state.NewGroupPriority) < 4 {
				m.state.NewGroupPriority += msg.String()
			}
		}
	case 3: // Max concurrency
		if msg.String() == "backspace" || msg.String() == "delete" || msg.String() == "del" {
			if len(m.state.NewGroupMaxConc) > 0 {
				m.state.NewGroupMaxConc = m.state.NewGroupMaxConc[:len(m.state.NewGroupMaxConc)-1]
			}
			return m, nil
		}
		if len(msg.String()) == 1 && msg.String() >= "0" && msg.String() <= "9" {
			if len(m.state.NewGroupMaxConc) < 4 {
				m.state.NewGroupMaxConc += msg.String()
			}
		}
	}
	return m, nil
}

func (m *WizardModel) handleCreateGroupEnter() (tea.Model, tea.Cmd) {
	// Profile dropdown toggle
	if m.focusedField == 1 {
		if m.state.ShowGroupProfileDropdown {
			if m.state.NewGroupProfileDropdownCursor < len(m.state.NewGroupProfileNames) {
				m.state.NewGroupProfile = m.state.NewGroupProfileNames[m.state.NewGroupProfileDropdownCursor]
			}
			m.state.ShowGroupProfileDropdown = false
			m.state.NewGroupProfileDropdownCursor = 0
			return m, nil
		}
		m.state.ShowGroupProfileDropdown = true
		m.state.NewGroupProfileDropdownCursor = 0
		for i, name := range m.state.NewGroupProfileNames {
			if name == m.state.NewGroupProfile {
				m.state.NewGroupProfileDropdownCursor = i
				break
			}
		}
		return m, nil
	}

	// Save button
	if m.focusedField == 4 {
		return m.handleCreateGroupSave()
	}

	// Cancel button
	if m.focusedField == 5 {
		m.state.NewGroupName = ""
		m.state.NewGroupProfile = ""
		m.state.NewGroupPriority = ""
		m.state.NewGroupMaxConc = ""
		m.state.ShowGroupProfileDropdown = false
		m.state.EditingGroupID = 0
		m.state.ErrorMessage = ""
		m.focusedField = 0
		m.state.CurrentScreen = ScreenGroups
		return m, nil
	}

	return m, nil
}

func (m *WizardModel) handleCreateGroupSave() (tea.Model, tea.Cmd) {
	// Validate
	if strings.TrimSpace(m.state.NewGroupName) == "" {
		m.state.ErrorMessage = "Group name is required"
		return m, nil
	}
	priority, err := strconv.ParseFloat(strings.TrimSpace(m.state.NewGroupPriority), 64)
	if err != nil || priority < 0 || priority > 1 {
		m.state.ErrorMessage = "Priority must be between 0.00 and 1.00"
		return m, nil
	}
	maxConc, err := strconv.Atoi(strings.TrimSpace(m.state.NewGroupMaxConc))
	if err != nil || maxConc < 1 {
		m.state.ErrorMessage = "Max concurrency must be a positive integer"
		return m, nil
	}

	name := strings.TrimSpace(m.state.NewGroupName)
	profile := strings.TrimSpace(m.state.NewGroupProfile)

	if m.state.EditingGroupID != 0 {
		// Update path — find existing pending op with same ID and update in-place
		updated := false
		for i := range m.state.GroupsPendingOps {
			if m.state.GroupsPendingOps[i].ID == m.state.EditingGroupID && (m.state.GroupsPendingOps[i].OpType == 0 || m.state.GroupsPendingOps[i].OpType == 1) {
				m.state.GroupsPendingOps[i].Profile = profile
				m.state.GroupsPendingOps[i].PriorityWeight = priority
				m.state.GroupsPendingOps[i].MaxConcurrency = maxConc
				updated = true
				break
			}
		}
		if !updated {
			// No existing pending op — append an update
			m.state.GroupsPendingOps = append(m.state.GroupsPendingOps, PendingGroupOp{
				OpType:         1,
				ID:             m.state.EditingGroupID,
				Profile:        profile,
				PriorityWeight: priority,
				MaxConcurrency: maxConc,
			})
		}
	} else {
		// Create path — check duplicate name against effective list
		for _, g := range m.effectiveGroupsList() {
			if g.Name == name {
				m.state.ErrorMessage = "A group with this name already exists"
				return m, nil
			}
		}
		tempID := m.groupsNextTempID()
		m.state.GroupsPendingOps = append(m.state.GroupsPendingOps, PendingGroupOp{
			OpType:         0,
			ID:             tempID,
			Name:           name,
			Profile:        profile,
			PriorityWeight: priority,
			MaxConcurrency: maxConc,
		})
		// Set member count for new group
		if m.state.GroupsMemberCounts == nil {
			m.state.GroupsMemberCounts = make(map[int64]int)
		}
		m.state.GroupsMemberCounts[tempID] = 0
	}

	m.state.HasChanges = true
	m.refreshGroupsDisplay()
	m.state.NewGroupName = ""
	m.state.NewGroupProfile = ""
	m.state.NewGroupPriority = ""
	m.state.NewGroupMaxConc = ""
	m.state.ShowGroupProfileDropdown = false
	m.state.EditingGroupID = 0
	m.state.ErrorMessage = ""
	m.focusedField = 0
	m.state.CurrentScreen = ScreenGroups
	return m, nil
}

// --- Multi-User Render ---

func (m *WizardModel) renderMultiUser() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("Multi-User Settings")

	// Enable checkbox
	enabledCheckbox := CheckboxUncheckedStyle.Render()
	if m.state.MultiUserEnabled {
		enabledCheckbox = CheckboxCheckedStyle.Render()
	}
	checkboxFocused := m.focusedField == 0
	if checkboxFocused {
		if m.state.MultiUserEnabled {
			enabledCheckbox = CheckboxCheckedFocusedStyle.Render()
		} else {
			enabledCheckbox = CheckboxUncheckedFocusedStyle.Render()
		}
	}
	checkboxRow := lipgloss.JoinHorizontal(lipgloss.Left, enabledCheckbox, " Enable Multi-User Mode")
	if checkboxFocused {
		checkboxRow = FocusedRowStyle.Width(m.contentWidth()).Render(checkboxRow)
	} else {
		checkboxRow = lipgloss.NewStyle().Padding(0, 1).Width(m.contentWidth()).Render(checkboxRow)
	}

	// Global Max Concurrency
	maxConcLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("Global Max Concurrency:")
	maxConcValue := m.state.MultiUserGlobalMax
	if m.focusedField == 1 {
		maxConcValue = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(maxConcValue + "_")
	} else {
		maxConcValue = InputFieldStyle.Width(m.inputFieldWidth()).Render(maxConcValue)
	}

	// WRED Min Depth
	wredMinLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("WRED Min Depth:")
	wredMinValue := m.state.MultiUserWREDMin
	if m.focusedField == 2 {
		wredMinValue = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(wredMinValue + "_")
	} else {
		wredMinValue = InputFieldStyle.Width(m.inputFieldWidth()).Render(wredMinValue)
	}

	// WRED Max Depth
	wredMaxLabel := MenuItemDimmedStyle.Width(m.contentWidth()).Render("WRED Max Depth:")
	wredMaxValue := m.state.MultiUserWREDMax
	if m.focusedField == 3 {
		wredMaxValue = InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(wredMaxValue + "_")
	} else {
		wredMaxValue = InputFieldStyle.Width(m.inputFieldWidth()).Render(wredMaxValue)
	}

	// Manage Groups button
	manageGroupsText := " Manage Groups \u2192 "
	if m.groupsHavePendingChanges() {
		manageGroupsText = " * Manage Groups \u2192 "
	}
	manageBtn := ButtonStyle.Render(manageGroupsText)
	if m.focusedField == 4 {
		manageBtn = ButtonActiveStyle.Render(manageGroupsText)
	}

	// Save button (green)
	saveBtn := ButtonSaveStyle.Render(" Ok ")
	if m.focusedField == 5 {
		saveBtn = ButtonSaveActiveStyle.Render(" Ok ")
	}

	// Cancel button (red)
	cancelBtn := ButtonCancelStyle.Render(" Cancel ")
	if m.focusedField == 6 {
		cancelBtn = ButtonCancelActiveStyle.Render(" Cancel ")
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title, m.blankLine(),
		checkboxRow, m.blankLine(),
		maxConcLabel, m.fullWidth(maxConcValue),
		wredMinLabel, m.fullWidth(wredMinValue),
		wredMaxLabel, m.fullWidth(wredMaxValue),
		m.blankLine(),
		m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, manageBtn)),
		m.blankLine(),
		m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, saveBtn, "  ", cancelBtn)),
		m.blankLine(),
		m.divider(),
		m.footline("[Tab] Next   [Enter] Action   [Esc] Cancel & Back   [Space] Toggle"),
	)

	if m.state.ErrorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage),
		)
	}

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// --- Groups List Render ---

func (m *WizardModel) renderGroups() string {
	title := SectionHeaderStyle.Width(m.contentWidth()).Render("User Groups")

	var lines []string
	lines = append(lines, "")

	if len(m.state.GroupsList) == 0 {
		lines = append(lines, MenuItemDimmedStyle.Width(m.contentWidth()).Render("No groups configured."))
		lines = append(lines, "")
		addBtn := ButtonPrimaryStyle.Render("[Create Group]")
		lines = append(lines, m.fullWidth(lipgloss.JoinHorizontal(lipgloss.Center, addBtn)))
	} else {
		header := TableHeaderStyle.Width(m.contentWidth()).Render(
			fmt.Sprintf("%-16s %-14s %-10s %-8s %-7s", "NAME", "PROFILE", "PRIORITY", "MAXCONC", "MEMBERS"),
		)
		lines = append(lines, header)

		for i, g := range m.state.GroupsList {
			members := 0
			if m.state.GroupsMemberCounts != nil {
				members = m.state.GroupsMemberCounts[g.ID]
			}
			row := fmt.Sprintf("%-16s %-14s %-10s %-8d %-7d",
				truncate(g.Name, 16), truncate(g.Profile, 14),
				fmt.Sprintf("%.2f", g.PriorityWeight), g.MaxConcurrency, members)

			if i == m.state.GroupsCursor {
				row = ListItemSelectedStyle.Width(m.contentWidth()).Render(row)
			} else {
				row = ListItemStyle.Width(m.contentWidth()).Render(row)
			}
			lines = append(lines, row)
		}
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title, m.blankLine(),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
		m.blankLine(),
		m.footline("[a] Add   [\u232b] Delete   [Enter] Edit   [Esc] Back"),
	)

	if m.state.ErrorMessage != "" {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.blankLine(),
			ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage),
		)
	}

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}

// --- Create/Edit Group Render ---

func (m *WizardModel) renderCreateGroup() string {
	titleText := "Create Group"
	if m.state.EditingGroupID != 0 {
		titleText = "Edit Group"
	}
	title := TitleStyle.Width(m.contentWidth()).Render(titleText)
	backHint := HelpTextStyle.Render("[Esc] Back")

	var lines []string
	lines = append(lines, "")

	// Group Name field (locked when editing)
	nameLabel := "Group Name:"
	if m.state.EditingGroupID != 0 {
		nameLabel = "Group Name: (locked)"
	}
	lines = append(lines, nameLabel)
	if m.state.EditingGroupID != 0 {
		lines = append(lines, InputFieldDisabledStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupName))
	} else if m.focusedField == 0 {
		lines = append(lines, InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupName+"_"))
	} else {
		lines = append(lines, InputFieldStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupName))
	}
	lines = append(lines, "")

	// Profile field with dropdown
	lines = append(lines, "Route Profile:")
	if m.focusedField == 1 {
		lines = append(lines, InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupProfile+" \u25be"))
	} else {
		lines = append(lines, InputFieldStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupProfile))
	}

	// Profile dropdown
	if m.state.ShowGroupProfileDropdown {
		var dropdownItems []string
		dropdownContentWidth := m.inputFieldWidth() - DropdownStyle.GetHorizontalFrameSize()
		for i, name := range m.state.NewGroupProfileNames {
			if i == m.state.NewGroupProfileDropdownCursor {
				dropdownItems = append(dropdownItems, ListItemSelectedStyle.Width(dropdownContentWidth).Render(name))
			} else {
				dropdownItems = append(dropdownItems, ListItemStyle.Width(dropdownContentWidth).Render(name))
			}
		}
		if len(dropdownItems) > 0 {
			dropdown := DropdownStyle.Width(m.inputFieldWidth()).Render(
				lipgloss.JoinVertical(lipgloss.Left, dropdownItems...),
			)
			lines = append(lines, dropdown)
		}
	}
	lines = append(lines, "")

	// Priority Weight field
	lines = append(lines, "Priority Weight (0.00-1.00):")
	if m.focusedField == 2 {
		lines = append(lines, InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupPriority+"_"))
	} else {
		lines = append(lines, InputFieldStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupPriority))
	}
	lines = append(lines, "")

	// Max Concurrency field
	lines = append(lines, "Max Concurrency:")
	if m.focusedField == 3 {
		lines = append(lines, InputFieldFocusedStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupMaxConc+"_"))
	} else {
		lines = append(lines, InputFieldStyle.Width(m.inputFieldWidth()).Render(m.state.NewGroupMaxConc))
	}
	lines = append(lines, "")

	// Buttons
	saveLabel := " Save "
	if m.state.EditingGroupID != 0 {
		saveLabel = " Ok "
	}
	saveBtn := ButtonSaveStyle.Render(saveLabel)
	cancelBtn := ButtonCancelStyle.Render(" Cancel ")
	if m.focusedField == 4 {
		saveBtn = ButtonSaveActiveStyle.Render(saveLabel)
	}
	if m.focusedField == 5 {
		cancelBtn = ButtonCancelActiveStyle.Render(" Cancel ")
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, "  ", saveBtn, "    ", cancelBtn, "  ")
	lines = append(lines, m.fullWidth(buttons))

	if m.state.ErrorMessage != "" {
		lines = append(lines, "")
		lines = append(lines, ErrorStyle.Width(m.contentWidth()).Render(m.state.ErrorMessage))
	}

	header := title
	if m.state.EditingGroupID == 0 {
		header = title + "  " + backHint
	}
	helpText := "[Tab] Next   [Enter] Confirm   [Esc] Cancel"
	if m.state.EditingGroupID != 0 {
		helpText = "[Tab] Next   [Enter] Confirm   [Esc] Back"
	}
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.fullWidth(header),
		m.blankLine(),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
		m.blankLine(),
		m.divider(),
		m.footline(helpText),
	)

	mainBox := MainContainerStyle.Width(m.width - 2).Render(content)
	return m.renderWithModal(mainBox)
}