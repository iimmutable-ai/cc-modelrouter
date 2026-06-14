package configwizard

import (
	"os"
	"strconv"
	"strings"

	"github.com/iimmutable/cc-modelrouter/internal/auth"
	"github.com/iimmutable/cc-modelrouter/internal/config"
)

// Screen represents the current screen in the wizard.
type Screen int

const (
	ScreenMainMenu Screen = iota
	ScreenProviders
	ScreenAddProvider1
	ScreenAddProvider2
	ScreenRoutes
	ScreenEditRoute
	ScreenCreateProfile
	ScreenEditProfile
	ScreenServer
	ScreenLogging
	ScreenViewConfig
	ScreenTestConnection
	ScreenAPIKeys
	ScreenCreateAPIKey
	ScreenMultiUser      // Global multi-user settings
	ScreenGroups         // Group list + CRUD
	ScreenCreateGroup    // Create/edit group form
)

// WizardState holds all state for the configuration wizard.
type WizardState struct {
	// Navigation
	CurrentScreen  Screen
	PreviousScreen Screen

	// Config being edited
	Config      *config.Config
	ConfigPath  string
	HasChanges  bool
	OriginalCfg *config.Config // For detecting changes

	// Main menu state
	MainMenuCursor int // saved main menu cursor when entering sub-screens

	// Provider list screen state
	ProviderCursor int

	// Add/Edit provider state (Step 1)
	NewProviderName      string
	NewProviderBaseURL   string
	NewProviderTransformer string
	NewProviderModels    string
	ProviderPreset       string // "anthropic", "openrouter", "bigmodel", "gemini", "custom"
	EditingProvider      bool   // true when editing an existing provider

	// Advanced provider settings
	NewProviderDisableKeepAlives   bool
	NewProviderMaxRequestBodyBytes string

	// Add provider state (Step 2)
	NewProviderAPIKey      string
	AddToShellConfig       bool
	SourceImmediately      bool

	// Routes screen state
	RouteCursor int

	// Edit route state
	EditRouteName       string
	EditRouteChain      []config.RouteTarget
	EditRouteChainCursor int // selected chain item index
	SelectedProvider    string
	SelectedModel       string

	// Server screen state
	ServerHost        string
	ServerPort        string
	ServerMaxRetries  string
	ServerRetryDelay  string
	PortStatus        string // non-empty = port availability status (warning or success)
	PortTesting       bool   // true while port availability test is running

	// Logging screen state
	LoggingEnabled    bool
	LoggingLevel      string
	LoggingDestination string
	LoggingFilePath   string

	// Logging dropdown state
	ShowLogLevelDropdown bool
	LogLevelDropdownCursor int
	ShowLogDestDropdown   bool
	LogDestDropdownCursor int

	// Test connection state
	TestProvider   string
	TestModel      string
	TestStatus    string // "testing", "success", "error"
	TestError      string
	TestLatency   float64

	// Modal/Confirmation
	ShowConfirm    bool
	ConfirmMessage string
	ConfirmAction  func() bool
	ConfirmCursor  int // 0 = Yes focused, 1 = No focused

	// Resolved API keys (real values, not ${CCROUTER_...} placeholders)
	ResolvedAPIKeys      map[string]string
	OriginalResolvedKeys map[string]string // snapshot for change detection

	// Error message
	ErrorMessage string

	// Dropdown state (Add Provider screen)
	ShowDropdown   bool
	DropdownCursor int

	// Model dropdown state (Add Provider screen)
	ShowModelDropdown   bool
	ModelDropdownCursor int

	// Route name dropdown state (Edit Route screen)
	ShowRouteNameDropdown   bool
	RouteNameDropdownCursor int

	// Profile tabs state (Routes screen)
	ProfileTabIndex    int      // Current tab index (0 = legacy, 1 = default, 2 = ...)
	ProfileTabKeys     []string // Tab keys in order: ["legacy", "default", "cost-opt", ...]

	// Profile edit state (when creating/editing profile metadata)
	EditProfileKey       string // Profile key being edited (empty when creating new)
	EditProfileName      string // Profile display name (for rename/create)
	EditProfileDesc      string // Profile description
	ShowProfileEditModal bool   // Show profile name/description edit modal (deprecated - use ScreenEditProfile instead)
	IsCreatingProfile    bool   // true when creating new profile, false when editing existing

	// Migration state (when creating first profile with legacy routes)
	ShowMigrationModal bool // Show migration confirmation modal
	MigrationChoice    int  // 0 = copy routes, 1 = start empty

	// API Keys screen state
	KeysList        []*auth.KeyInfo // populated from DB
	KeysCursor      int            // selected key in list
	NewKeyName      string         // name for new key
	NewKeyGroup     string         // group for new key
	NewKeyGroups    []*auth.GroupInfo // available groups (dropdown)
	CreatedRawKey   string         // shown once after creation
	KeyShowConfirm  bool           // show "save this key" warning

	// Multi-User screen state
	MultiUserEnabled   bool   // multi-user toggle
	MultiUserGlobalMax string // global max concurrency
	MultiUserWREDMin   string // WRED min depth
	MultiUserWREDMax   string // WRED max depth

	// Multi-User screen original values (for Cancel)
	MultiUserOrigEnabled   bool
	MultiUserOrigGlobalMax string
	MultiUserOrigWREDMin   string
	MultiUserOrigWREDMax   string

	// Groups list screen state
	GroupsList         []*auth.GroupInfo // populated from DB
	GroupsCursor       int              // selected group in list
	GroupsMemberCounts map[int64]int     // group ID → member count
	GroupsSnapshot      []*auth.GroupInfo // base state from SQLite (set by loadGroupsData)
	GroupsPendingOps    []PendingGroupOp  // staged create/update/delete ops

	// Create/Edit group form state
	NewGroupName            string   // group name
	NewGroupProfile         string   // route profile
	NewGroupPriority        string   // priority weight
	NewGroupMaxConc         string   // max concurrency
	NewGroupProfileNames    []string // available profile names (sorted)
	ShowGroupProfileDropdown bool     // show profile dropdown
	NewGroupProfileDropdownCursor int // cursor in profile dropdown
	EditingGroupID          int64    // 0=create, >0=edit, <0=editing in-memory created group
}

// PendingGroupOp represents a staged group change not yet written to SQLite.
// OpType: 0=create, 1=update, 2=delete.
// For creates, ID is a negative temporary ID assigned in memory.
type PendingGroupOp struct {
	OpType         int
	ID             int64
	Name           string
	Profile        string
	PriorityWeight float64
	MaxConcurrency int
}

// ProviderPreset defines preset provider configurations.
type ProviderPreset struct {
	BaseURL     string
	Transformer string
	Models      []string
}

// ProviderPresets contains all available provider presets.
var ProviderPresets = map[string]ProviderPreset{
	"alicloud": {
		BaseURL:     "https://coding.dashscope.aliyuncs.com/apps/anthropic",
		Transformer: "glm_anthropic",
		Models:      []string{"MiniMax-M2.5", "kimi-k2.5", "qwen3-coder-plus", "glm-5", "glm-4.7"},
	},
	"anthropic": {
		BaseURL:     "https://api.anthropic.com",
		Transformer: "anthropic",
		Models:      []string{"claude-haiku-4.5", "claude-sonnet-4.6", "claude-opus-4.5", "claude-opus-4.6"},
	},
	"bigmodel": {
		BaseURL:     "https://open.bigmodel.cn/api",
		Transformer: "glm_anthropic",
		Models:      []string{"glm-4.6v", "glm-4.7", "glm-5-turbo", "glm-5v-turbo", "glm-5.1"},
	},
	"openrouter": {
		BaseURL:     "https://openrouter.ai/api",
		Transformer: "anthropic",
		Models:      []string{"openai/gpt-5.4", "openai/gpt-5.4-mini", "openai/gpt-5.3-codex", "google/gemini-2.5-flash", "google/gemini-2.5-pro"},
	},
	"openrouter-openai": {
		BaseURL:     "https://openrouter.ai/api",
		Transformer: "anthropic",
		Models:      []string{"openai/gpt-5.4", "openai/gpt-5.4-mini", "openai/gpt-5.3-codex"},
	},
	"openrouter-anthropic": {
		BaseURL:     "https://openrouter.ai/api",
		Transformer: "anthropic",
		Models:      []string{"anthropic/claude-haiku-4.5", "anthropic/claude-sonnet-4.5", "anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.5", "anthropic/claude-opus-4.6"},
	},
	"gemini": {
		BaseURL:     "https://generativelanguage.googleapis.com",
		Transformer: "gemini",
		Models:      []string{"gemini-2.5-flash", "gemini-2.5-pro", "gemini-2.0-flash"},
	},
}

// PredefinedRouteNames are the built-in route names.
var PredefinedRouteNames = []string{
	"default",
	"background",
	"subagent",
	"review",
	"think",
	"thinkMore",
	"ultrathink",
	"longContext",
	"image",
	"webSearch",
}

// LogLevelOptions are the available log level options.
var LogLevelOptions = []string{"debug", "info", "warn", "error"}

// LogDestinationOptions are the available log destination options.
var LogDestinationOptions = []string{"stdout", "stderr", "file"}

// TransformerOptions are the available transformer options.
var TransformerOptions = []string{"anthropic", "openai", "glm_anthropic", "gemini"}

// NewWizardState creates a new wizard state with defaults.
func NewWizardState(cfg *config.Config, configPath string) *WizardState {
	// Pre-resolve API keys from config (placeholders → real values)
	resolved := make(map[string]string)
	for name, pCfg := range cfg.Providers {
		resolved[name] = stripEnvVarPlaceholder(os.ExpandEnv(pCfg.APIKey))
	}

	// Deep-copy resolved keys for change detection
	originalResolved := make(map[string]string, len(resolved))
	for k, v := range resolved {
		originalResolved[k] = v
	}

	return &WizardState{
		CurrentScreen:         ScreenMainMenu,
		Config:                cfg,
		ConfigPath:            configPath,
		HasChanges:            false,
		OriginalCfg:           deepCopyConfig(cfg),
		ResolvedAPIKeys:       resolved,
		OriginalResolvedKeys:  originalResolved,
		ProviderCursor:        0,
		MainMenuCursor:        0,
		ProviderPreset:        "anthropic",
		NewProviderTransformer: "anthropic",
		LoggingLevel:          "info",
		LoggingDestination:    "stdout",
		LoggingEnabled:        cfg.Logging.Enabled,
		ServerHost:            cfg.Server.Host,
		ServerPort:            "8081",
		ServerMaxRetries:      strconv.Itoa(cfg.Router.MaxRetries),
		ServerRetryDelay:      cfg.Router.RetryDelay,
	}
}

// deepCopyConfig creates a deep copy of the config.
func deepCopyConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	providers := make(map[string]config.ProviderConfig, len(cfg.Providers))
	for k, v := range cfg.Providers {
		models := make([]string, len(v.Models))
		copy(models, v.Models)
		providers[k] = config.ProviderConfig{
			APIKey:      v.APIKey,
			BaseURL:     v.BaseURL,
			Models:      models,
			Transformer: v.Transformer,
		}
	}
	routes := make(map[string]string, len(cfg.Router.Routes))
	for k, v := range cfg.Router.Routes {
		routes[k] = v
	}
	// Deep copy profiles from Router.Profiles
	profiles := make(map[string]config.ProfileConfig, len(cfg.Router.Profiles))
	for k, v := range cfg.Router.Profiles {
		profileRoutes := make(map[string]string, len(v.Routes))
		for rk, rv := range v.Routes {
			profileRoutes[rk] = rv
		}
		profiles[k] = config.ProfileConfig{
			Name:        v.Name,
			Description: v.Description,
			Routes:      profileRoutes,
		}
	}
	return &config.Config{
		Server: cfg.Server,
		Router: config.RouterConfig{
			Routes:     routes,
			Profiles:   profiles, // Profiles now in RouterConfig
			MaxRetries: cfg.Router.MaxRetries,
			RetryDelay: cfg.Router.RetryDelay,
		},
		Logging:   cfg.Logging,
		Providers: providers,
	}
}

// HasUnsavedChanges returns true if the config has been modified.
func (s *WizardState) HasUnsavedChanges() bool {
	if s.Config == nil || s.OriginalCfg == nil {
		return s.HasChanges
	}
	if !configsEqual(s.Config, s.OriginalCfg) {
		return true
	}
	if len(s.GroupsPendingOps) > 0 {
		return true
	}
	if s.MultiUserEnabled != s.MultiUserOrigEnabled ||
		s.MultiUserGlobalMax != s.MultiUserOrigGlobalMax ||
		s.MultiUserWREDMin != s.MultiUserOrigWREDMin ||
		s.MultiUserWREDMax != s.MultiUserOrigWREDMax {
		return true
	}
	// Also check if resolved API keys changed (env var templates stay the same
	// but the actual key values may differ after editing)
	return !resolvedKeysEqual(s.ResolvedAPIKeys, s.OriginalResolvedKeys)
}

// resolvedKeysEqual compares two resolved API key maps for equality.
func resolvedKeysEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || av != bv {
			return false
		}
	}
	return true
}

// ResolvedKeys returns the resolved API keys map from the wizard model.
func (m *WizardModel) ResolvedKeys() map[string]string {
	return m.state.ResolvedAPIKeys
}

// configsEqual compares two configs for equality.
func configsEqual(a, b *config.Config) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Server.Host != b.Server.Host || a.Server.Port != b.Server.Port {
		return false
	}
	if len(a.Providers) != len(b.Providers) {
		return false
	}
	for k, av := range a.Providers {
		bv, ok := b.Providers[k]
		if !ok {
			return false
		}
		if av.APIKey != bv.APIKey || av.BaseURL != bv.BaseURL || av.Transformer != bv.Transformer {
			return false
		}
		if len(av.Models) != len(bv.Models) {
			return false
		}
		for i, m := range av.Models {
			if m != bv.Models[i] {
				return false
			}
		}
	}
	if len(a.Router.Routes) != len(b.Router.Routes) {
		return false
	}
	for k, v := range a.Router.Routes {
		if b.Router.Routes[k] != v {
			return false
		}
	}
	if a.Logging.Enabled != b.Logging.Enabled ||
		a.Logging.Level != b.Logging.Level ||
		a.Logging.Destination != b.Logging.Destination ||
		a.Logging.FilePath != b.Logging.FilePath {
		return false
	}
	// Compare profiles from Router.Profiles
	if len(a.Router.Profiles) != len(b.Router.Profiles) {
		return false
	}
	for k, av := range a.Router.Profiles {
		bv, ok := b.Router.Profiles[k]
		if !ok {
			return false
		}
		if av.Name != bv.Name || av.Description != bv.Description {
			return false
		}
		if len(av.Routes) != len(bv.Routes) {
			return false
		}
		for rk, rv := range av.Routes {
			if bv.Routes[rk] != rv {
				return false
			}
		}
	}
	return true
}

// stripEnvVarPlaceholder removes a leading ${CCROUTER_*} env var placeholder from a key.
// This handles cases where os.ExpandEnv returns the literal placeholder (env var unset)
// or where the env var was self-referencing (e.g. "${CCROUTER_X}sk-real").
func stripEnvVarPlaceholder(key string) string {
	if strings.HasPrefix(key, "${CCROUTER_") {
		if end := strings.Index(key, "}"); end != -1 {
			return key[end+1:]
		}
	}
	return key
}