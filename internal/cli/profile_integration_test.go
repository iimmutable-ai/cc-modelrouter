package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
)

// Integration test for full profile switch workflow
// Tests: Config -> Router Engine -> Admin API -> Profile Switch

func TestProfileSwitchWorkflow_Integration(t *testing.T) {
	// Setup configuration with profiles
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 0, // Will be assigned by test server
			Host: "localhost",
		},
		Providers: map[string]config.ProviderConfig{
			"test-provider": {
				APIKey:  "test-key",
				BaseURL: "https://api.test.com",
				Models:  []string{"model-1"},
			},
		},
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "test-provider:model-1",
			},
			Profiles: map[string]config.ProfileConfig{
				"fast": {
					Name:        "Fast",
					Description: "Quick responses",
					Routes: map[string]string{
						"default": "test-provider:fast-model",
						"think":   "test-provider:fast-think",
					},
				},
				"quality": {
					Name:        "Quality",
					Description: "High quality responses",
					Routes: map[string]string{
						"default":    "test-provider:quality-model",
						"think":      "test-provider:quality-think",
						"ultrathink": "test-provider:quality-ultrathink",
					},
				},
			},
		},
	}

	// Create handler with config
	handler := proxy.NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	adminToken := daemon.GenerateAdminToken()
	handler.SetAdminToken(adminToken)
	handler.SetActiveProfile("fast")

	// Create admin handler
	adminHandler := proxy.NewAdminHandler(handler)

	// Create test server
	server := httptest.NewServer(adminHandler)
	defer server.Close()

	// Step 1: List profiles - should show "fast" as active
	t.Run("list_profiles", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/_admin/profiles?token="+adminToken, nil)
		req.Host = "localhost:8081"

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result proxy.ListProfilesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !result.HasProfiles {
			t.Error("expected HasProfiles to be true")
		}
		if result.ActiveProfile != "fast" {
			t.Errorf("expected active profile 'fast', got '%s'", result.ActiveProfile)
		}
		if len(result.Profiles) != 2 {
			t.Errorf("expected 2 profiles, got %d", len(result.Profiles))
		}
	})

	// Step 2: Get active profile
	t.Run("get_active_profile", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/_admin/profiles/active?token="+adminToken, nil)
		req.Host = "localhost:8081"

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result struct {
			ActiveProfile string
			HasProfiles   bool
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.ActiveProfile != "fast" {
			t.Errorf("expected active profile 'fast', got '%s'", result.ActiveProfile)
		}
	})

	// Step 3: Switch to "quality" profile
	t.Run("switch_profile", func(t *testing.T) {
		body := `{"profile": "quality"}`
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/_admin/profiles/switch", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Admin-Token", adminToken)
		req.Host = "localhost:8081"

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result proxy.SwitchProfileResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !result.Success {
			t.Errorf("expected success, got error: %s", result.Error)
		}
		if result.ActiveProfile != "quality" {
			t.Errorf("expected active profile 'quality', got '%s'", result.ActiveProfile)
		}
		// Verify routes were returned
		if len(result.Routes) == 0 {
			t.Error("expected routes to be returned")
		}
	})

	// Step 4: Verify the switch persisted in handler
	t.Run("verify_switch_persisted", func(t *testing.T) {
		active := handler.GetActiveProfile()
		if active != "quality" {
			t.Errorf("handler active profile should be 'quality', got '%s'", active)
		}
	})

	// Step 5: Verify routes changed in handler's config
	t.Run("verify_routes_changed", func(t *testing.T) {
		routes := cfg.GetActiveRoutes(handler.GetActiveProfile())
		if routes["default"] != "test-provider:quality-model" {
			t.Errorf("expected quality default route, got '%s'", routes["default"])
		}
		if routes["ultrathink"] != "test-provider:quality-ultrathink" {
			t.Errorf("expected quality ultrathink route, got '%s'", routes["ultrathink"])
		}
	})

	// Step 6: Switch back to "fast"
	t.Run("switch_back", func(t *testing.T) {
		body := `{"profile": "fast"}`
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/_admin/profiles/switch", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Admin-Token", adminToken)
		req.Host = "localhost:8081"

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var result proxy.SwitchProfileResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.ActiveProfile != "fast" {
			t.Errorf("expected active profile 'fast', got '%s'", result.ActiveProfile)
		}
	})
}

// Test profile switch with router engine
func TestProfileSwitchWithRouterEngine_Integration(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:legacy-default",
			},
			Profiles: map[string]config.ProfileConfig{
				"cost-opt": {
					Name: "Cost Optimized",
					Routes: map[string]string{
						"default": "provider:cheap-model",
					},
				},
				"premium": {
					Name: "Premium",
					Routes: map[string]string{
						"default": "provider:premium-model",
					},
				},
			},
		},
	}

	engine := router.NewEngine(cfg)
	engine.SetActiveProfile("cost-opt")

	// Initially using cost-opt profile
	t.Run("initial_cost_opt", func(t *testing.T) {
		targets := engine.GetTargets("default")
		if len(targets) == 0 {
			t.Fatal("expected at least one target")
		}
		if targets[0].Model != "cheap-model" {
			t.Errorf("expected cheap-model, got '%s'", targets[0].Model)
		}
	})

	// Switch to premium profile
	t.Run("switch_to_premium", func(t *testing.T) {
		engine.SetActiveProfile("premium")

		targets := engine.GetTargets("default")
		if len(targets) == 0 {
			t.Fatal("expected at least one target")
		}
		if targets[0].Model != "premium-model" {
			t.Errorf("expected premium-model, got '%s'", targets[0].Model)
		}
	})

	// Switch back
	t.Run("switch_back_to_cost_opt", func(t *testing.T) {
		engine.SetActiveProfile("cost-opt")

		targets := engine.GetTargets("default")
		if len(targets) == 0 {
			t.Fatal("expected at least one target")
		}
		if targets[0].Model != "cheap-model" {
			t.Errorf("expected cheap-model again, got '%s'", targets[0].Model)
		}
	})
}

// Test printProfileList with captured output via command execution
func TestProfileListCommand_OutputFormatting(t *testing.T) {
	// Save original HOME
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)

	// Create a config file with profiles
	configDir := tmpDir + "/.cc-modelrouter"
	os.MkdirAll(configDir, 0755)

	// Note: activeProfile is NOT in config file - it's runtime state
	configContent := `{
		"server": {"port": 8081, "host": "localhost"},
		"providers": {
			"test": {"apiKey": "key", "baseURL": "https://api.test.com", "models": ["m1"]}
		},
		"router": {"routes": {"default": "test:m1"}},
		"profiles": {
			"default": {"name": "Development", "description": "Dev profile", "routes": {"default": "test:dev-model"}},
			"prod": {"name": "Production", "routes": {"default": "test:prod-model"}}
		}
	}`

	configPath := configDir + "/config.json"
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Execute command and capture output
	cmd := NewProfileListCommand()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Set --from-config flag
	cmd.SetArgs([]string{"--from-config"})
	err := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("command execution failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output format - "default" is the default profile at startup
	if !strings.Contains(output, "Profiles (default at startup: default)") {
		t.Errorf("expected header with default profile, got: %s", output)
	}
	if !strings.Contains(output, "* Development [default]") {
		t.Errorf("expected default profile marked with asterisk, got: %s", output)
	}
	if !strings.Contains(output, "Dev profile") {
		t.Errorf("expected description for default profile, got: %s", output)
	}
	if !strings.Contains(output, "Production [prod]") {
		t.Errorf("expected other profile without asterisk, got: %s", output)
	}
}

// Test end-to-end: CLI command -> printProfileList with config
func TestPrintProfileList_FromConfig(t *testing.T) {
	// This tests the internal function directly with a real config
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{"default": "provider:legacy"},
			Profiles: map[string]config.ProfileConfig{
				"default": {
					Name:        "Default Profile",
					Description: "Standard configuration",
					Routes:      map[string]string{"default": "provider:standard"},
				},
				"experimental": {
					Name:        "Experimental",
					Description: "New features testing",
					Routes:      map[string]string{"default": "provider:exp"},
				},
			},
		},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := printProfileList(cfg)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("printProfileList failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify formatting - "default" is the default profile at startup
	expectedStrings := []string{
		"Profiles (default at startup: default)",
		"* Default Profile [default]",
		"Standard configuration",
		"Experimental [experimental]",
		"New features testing",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("expected output to contain '%s', got: %s", expected, output)
		}
	}
}