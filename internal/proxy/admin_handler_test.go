package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

func TestAdminHandler_ListProfiles(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:legacy-default",
			},
			Profiles: map[string]config.ProfileConfig{
				"fast": {
					Name:        "Fast",
					Description: "Fast models",
					Routes: map[string]string{
						"default": "provider:fast-default",
					},
				},
				"quality": {
					Name:        "Quality",
					Description: "Quality models",
					Routes: map[string]string{
						"default": "provider:quality-default",
					},
				},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	req := httptest.NewRequest(http.MethodGet, "/_admin/profiles", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ListProfilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(response.Profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(response.Profiles))
	}

	if response.ActiveProfile != "fast" {
		t.Errorf("expected active profile 'fast', got '%s'", response.ActiveProfile)
	}

	if !response.HasProfiles {
		t.Error("expected HasProfiles to be true")
	}
}

func TestAdminHandler_GetActiveProfile(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:model"}},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	req := httptest.NewRequest(http.MethodGet, "/_admin/profiles/active", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var response struct {
		ActiveProfile string
		HasProfiles   bool
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.ActiveProfile != "fast" {
		t.Errorf("expected active profile 'fast', got '%s'", response.ActiveProfile)
	}
}

func TestAdminHandler_SwitchProfile(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {
					Name: "Fast",
					Routes: map[string]string{
						"default": "provider:fast-default",
					},
				},
				"quality": {
					Name: "Quality",
					Routes: map[string]string{
						"default": "provider:quality-default",
					},
				},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	// Switch to quality profile
	body := `{"profile": "quality"}`
	req := httptest.NewRequest(http.MethodPost, "/_admin/profiles/switch", createBodyReader(body))
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response SwitchProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success, got error: %s", response.Error)
	}

	if response.ActiveProfile != "quality" {
		t.Errorf("expected active profile 'quality', got '%s'", response.ActiveProfile)
	}

	// Verify the handler's active profile was updated
	active := handler.GetActiveProfile()
	if active != "quality" {
		t.Errorf("handler's active profile should be 'quality', got '%s'", active)
	}
}

func TestAdminHandler_SwitchProfile_InvalidProfile(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:model"}},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	body := `{"profile": "nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/_admin/profiles/switch", createBodyReader(body))
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var response SwitchProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.Success {
		t.Error("expected failure for invalid profile")
	}

	if response.Error == "" {
		t.Error("expected error message")
	}
}

func TestAdminHandler_Unauthorized(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:model"}},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("correct-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	// Request without token
	req := httptest.NewRequest(http.MethodGet, "/_admin/profiles", nil)
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for missing token, got %d", rec.Code)
	}

	// Request with wrong token
	req = httptest.NewRequest(http.MethodGet, "/_admin/profiles", nil)
	req.Header.Set("X-Admin-Token", "wrong-token")
	req.Host = "localhost:8081"

	rec = httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for wrong token, got %d", rec.Code)
	}
}

func TestAdminHandler_TokenInQueryParam(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:model"}},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	// Request with token in query parameter
	req := httptest.NewRequest(http.MethodGet, "/_admin/profiles?token=test-token", nil)
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for token in query param, got %d", rec.Code)
	}
}

func TestAdminHandler_LocalhostOnly(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:model"}},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	// Request from non-localhost host
	req := httptest.NewRequest(http.MethodGet, "/_admin/profiles", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "example.com:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403 for non-localhost, got %d", rec.Code)
	}
}

func TestAdminHandler_UnknownEndpoint(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:model"}},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	req := httptest.NewRequest(http.MethodGet, "/_admin/unknown", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for unknown endpoint, got %d", rec.Code)
	}
}

// Helper function to create a body reader for httptest
func createBodyReader(body string) io.ReadCloser {
	return io.NopCloser(&mockBodyReader{data: []byte(body)})
}

type mockBodyReader struct {
	data   []byte
	offset int
}

func (r *mockBodyReader) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

func TestAdminHandler_SwitchProfile_EmptyProfile(t *testing.T) {
	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(&config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:model"}},
			},
		},
	})
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	body := `{"profile": ""}`
	req := httptest.NewRequest(http.MethodPost, "/_admin/profiles/switch", createBodyReader(body))
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for empty profile, got %d", rec.Code)
	}

	var response SwitchProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.Error != "Profile name is required" {
		t.Errorf("expected 'Profile name is required' error, got '%s'", response.Error)
	}
}

func TestAdminHandler_SwitchProfile_InvalidBody(t *testing.T) {
	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(&config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:model"}},
			},
		},
	})
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	req := httptest.NewRequest(http.MethodPost, "/_admin/profiles/switch", createBodyReader("not json"))
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid body, got %d", rec.Code)
	}

	var response SwitchProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.Error != "Invalid request body" {
		t.Errorf("expected 'Invalid request body' error, got '%s'", response.Error)
	}
}

func TestAdminHandler_ListProfiles_Sorted(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"zebra":   {Name: "Zebra", Routes: map[string]string{"default": "z:model"}},
				"alpha":   {Name: "Alpha", Routes: map[string]string{"default": "a:model"}},
				"middle":  {Name: "Middle", Routes: map[string]string{"default": "m:model"}},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("alpha")

	adminHandler := NewAdminHandler(handler)

	req := httptest.NewRequest(http.MethodGet, "/_admin/profiles", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	var response ListProfilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify sorted order
	expected := []string{"alpha", "middle", "zebra"}
	for i, p := range response.Profiles {
		if p.Key != expected[i] {
			t.Errorf("profile[%d] = %q, want %q (should be sorted)", i, p.Key, expected[i])
		}
	}
}

func TestAdminHandler_ListProfiles_LegacyRoutes(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Routes: map[string]string{
				"default": "provider:legacy-model",
				"think":   "provider:think-model",
			},
			Profiles: map[string]config.ProfileConfig{}, // No profiles
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")

	adminHandler := NewAdminHandler(handler)

	req := httptest.NewRequest(http.MethodGet, "/_admin/profiles", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	var response ListProfilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.HasProfiles {
		t.Error("expected HasProfiles to be false")
	}
	if len(response.LegacyRoutes) != 2 {
		t.Errorf("expected 2 legacy routes, got %d", len(response.LegacyRoutes))
	}
	if response.LegacyRoutes["default"] != "provider:legacy-model" {
		t.Errorf("expected legacy default route, got %s", response.LegacyRoutes["default"])
	}
}

func TestAdminHandler_SwitchProfile_ReturnsProfileDetails(t *testing.T) {
	cfg := &config.Config{
		Router: config.RouterConfig{
			Profiles: map[string]config.ProfileConfig{
				"fast": {Name: "Fast", Routes: map[string]string{"default": "provider:fast-model"}},
				"quality": {Name: "Quality", Description: "High quality", Routes: map[string]string{
					"default": "provider:quality-default",
					"think":   "provider:quality-think",
				}},
			},
		},
	}

	handler := NewHandler(10 * 1024 * 1024)
	handler.SetConfig(cfg)
	handler.SetAdminToken("test-token")
	handler.SetActiveProfile("fast")

	adminHandler := NewAdminHandler(handler)

	body := `{"profile": "quality"}`
	req := httptest.NewRequest(http.MethodPost, "/_admin/profiles/switch", createBodyReader(body))
	req.Header.Set("X-Admin-Token", "test-token")
	req.Host = "localhost:8081"

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	var response SwitchProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.ProfileName != "Quality" {
		t.Errorf("expected profile name 'Quality', got '%s'", response.ProfileName)
	}
	if len(response.Routes) != 2 {
		t.Errorf("expected 2 routes in response, got %d", len(response.Routes))
	}
	if response.Routes["default"] != "provider:quality-default" {
		t.Errorf("expected quality default route, got '%s'", response.Routes["default"])
	}
}