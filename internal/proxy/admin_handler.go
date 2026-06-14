// Package proxy implements the admin API handler for runtime configuration management.
package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// AdminHandler handles admin API requests for profile management.
type AdminHandler struct {
	handler *Handler
}

// NewAdminHandler creates a new admin handler.
func NewAdminHandler(handler *Handler) *AdminHandler {
	return &AdminHandler{handler: handler}
}

// ServeHTTP handles admin API requests.
func (a *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow localhost requests for security
	host := r.Host
	if !strings.HasPrefix(host, "localhost:") && !strings.HasPrefix(host, "127.0.0.1:") {
		http.Error(w, "Admin API only accessible from localhost", http.StatusForbidden)
		return
	}

	// Verify admin token
	token := r.Header.Get("X-Admin-Token")
	if token == "" {
		// Also check query parameter for convenience
		token = r.URL.Query().Get("token")
	}
	if token != a.handler.GetAdminToken() {
		http.Error(w, "Invalid admin token", http.StatusUnauthorized)
		return
	}

	// Route based on path
	path := r.URL.Path

	switch {
	case path == "/_admin/profiles" && r.Method == http.MethodGet:
		a.handleListProfiles(w, r)
	case path == "/_admin/profiles/active" && r.Method == http.MethodGet:
		a.handleGetActiveProfile(w, r)
	case path == "/_admin/profiles/switch" && r.Method == http.MethodPost:
		a.handleSwitchProfile(w, r)

	// Multi-user: API keys
	case path == "/_admin/keys" && r.Method == http.MethodGet:
		a.handleListKeys(w, r)
	case path == "/_admin/keys" && r.Method == http.MethodPost:
		a.handleCreateKey(w, r)
	case strings.HasPrefix(path, "/_admin/keys/") && r.Method == http.MethodDelete:
		a.handleRevokeKey(w, r)

	// Multi-user: Groups
	case path == "/_admin/groups" && r.Method == http.MethodGet:
		a.handleListGroups(w, r)
	case path == "/_admin/groups" && r.Method == http.MethodPost:
		a.handleCreateGroup(w, r)
	case strings.HasPrefix(path, "/_admin/groups/") && r.Method == http.MethodPut:
		a.handleUpdateGroup(w, r)
	case strings.HasPrefix(path, "/_admin/groups/") && r.Method == http.MethodDelete:
		a.handleDeleteGroup(w, r)

	// Multi-user: QoS
	case path == "/_admin/qos" && r.Method == http.MethodGet:
		a.handleQoSStatus(w, r)
	case strings.HasPrefix(path, "/_admin/qos/provider/") && r.Method == http.MethodPost:
		a.handleResetProvider(w, r)

	default:
		http.Error(w, "Unknown admin endpoint", http.StatusNotFound)
	}
}

// ProfileResponse represents a profile in API responses.
type ProfileResponse struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Routes      map[string]string `json:"routes"`
	IsActive    bool              `json:"isActive"`
}

// ListProfilesResponse represents the response for listing profiles.
type ListProfilesResponse struct {
	Profiles       []ProfileResponse `json:"profiles"`
	ActiveProfile  string            `json:"activeProfile"`
	HasProfiles    bool              `json:"hasProfiles"`
	LegacyRoutes   map[string]string `json:"legacyRoutes,omitempty"`
}

// handleListProfiles returns all configured profiles.
func (a *AdminHandler) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := a.handler.GetProfiles()
	activeProfile := a.handler.GetActiveProfile()
	cfg := a.handler.GetConfig()

	var profileList []ProfileResponse
	for key, profile := range profiles {
		profileList = append(profileList, ProfileResponse{
			Key:         key,
			Name:        profile.Name,
			Description: profile.Description,
			Routes:      profile.Routes,
			IsActive:    key == activeProfile,
		})
	}

	// Sort profiles by key for consistent ordering
	// (simple alphabetical sort)
	for i := 0; i < len(profileList); i++ {
		for j := i + 1; j < len(profileList); j++ {
			if profileList[i].Key > profileList[j].Key {
				profileList[i], profileList[j] = profileList[j], profileList[i]
			}
		}
	}

	response := ListProfilesResponse{
		Profiles:      profileList,
		ActiveProfile: activeProfile,
		HasProfiles:   len(profiles) > 0,
		LegacyRoutes:  cfg.Router.Routes,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetActiveProfile returns the current active profile name.
func (a *AdminHandler) handleGetActiveProfile(w http.ResponseWriter, r *http.Request) {
	activeProfile := a.handler.GetActiveProfile()

	response := struct {
		ActiveProfile string `json:"activeProfile"`
		HasProfiles   bool   `json:"hasProfiles"`
	}{
		ActiveProfile: activeProfile,
		HasProfiles:   len(a.handler.GetProfiles()) > 0,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// SwitchProfileRequest represents the request body for switching profiles.
type SwitchProfileRequest struct {
	Profile string `json:"profile"`
}

// SwitchProfileResponse represents the response for switching profiles.
type SwitchProfileResponse struct {
	Success       bool              `json:"success"`
	ActiveProfile string            `json:"activeProfile"`
	ProfileName   string            `json:"profileName,omitempty"`
	Routes        map[string]string `json:"routes,omitempty"`
	Error         string            `json:"error,omitempty"`
}

// handleSwitchProfile switches to a different profile.
func (a *AdminHandler) handleSwitchProfile(w http.ResponseWriter, r *http.Request) {
	var req SwitchProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := SwitchProfileResponse{
			Success: false,
			Error:   "Invalid request body",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.Profile == "" {
		response := SwitchProfileResponse{
			Success: false,
			Error:   "Profile name is required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Switch the profile
	err := a.handler.UpdateActiveProfile(req.Profile)
	if err != nil {
		response := SwitchProfileResponse{
			Success: false,
			Error:   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get the profile details for the response
	profiles := a.handler.GetProfiles()
	profile := profiles[req.Profile]

	response := SwitchProfileResponse{
		Success:       true,
		ActiveProfile: req.Profile,
		ProfileName:   profile.Name,
		Routes:        profile.Routes,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// --- Multi-user: API Key management ---

func (a *AdminHandler) handleListKeys(w http.ResponseWriter, r *http.Request) {
	ks := a.handler.GetKeyStore()
	if ks == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Multi-user not enabled")
		return
	}
	keys, err := ks.ListKeys()
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type keyResp struct {
		ID       int64   `json:"id"`
		Prefix   string  `json:"prefix"`
		Name     string  `json:"name"`
		Group    string  `json:"group"`
		Active   bool    `json:"active"`
		LastUsed *string `json:"lastUsed,omitempty"`
	}
	resp := make([]keyResp, len(keys))
	for i, k := range keys {
		resp[i] = keyResp{
			ID:    k.KeyID,
			Prefix: k.KeyPrefix,
			Name:  k.Name,
			Group: k.GroupName,
			Active: k.IsActive,
		}
		if k.LastUsed != nil {
			s := k.LastUsed.Format("2006-01-02T15:04:05Z07:00")
			resp[i].LastUsed = &s
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"keys": resp})
}

func (a *AdminHandler) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	ks := a.handler.GetKeyStore()
	if ks == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Multi-user not enabled")
		return
	}
	var req struct {
		Name    string `json:"name"`
		GroupID int64  `json:"groupId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.GroupID == 0 {
		writeAdminError(w, http.StatusBadRequest, "groupId is required")
		return
	}
	rawKey, keyID, err := ks.CreateKey(req.Name, req.GroupID)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":  keyID,
		"key": rawKey,
	})
}

func (a *AdminHandler) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	ks := a.handler.GetKeyStore()
	if ks == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Multi-user not enabled")
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/_admin/keys/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "Invalid key ID")
		return
	}
	if err := ks.RevokeKey(id); err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// --- Multi-user: Group management ---

func (a *AdminHandler) handleListGroups(w http.ResponseWriter, r *http.Request) {
	ks := a.handler.GetKeyStore()
	if ks == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Multi-user not enabled")
		return
	}
	groups, err := ks.ListGroups()
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type groupResp struct {
		ID             int64   `json:"id"`
		Name           string  `json:"name"`
		Profile        string  `json:"profile"`
		PriorityWeight float64 `json:"priorityWeight"`
		MaxConcurrency int     `json:"maxConcurrency"`
		Members        int     `json:"members"`
	}
	resp := make([]groupResp, len(groups))
	for i, g := range groups {
		count, _ := ks.GetGroupMemberCount(g.ID)
		resp[i] = groupResp{
			ID:             g.ID,
			Name:           g.Name,
			Profile:        g.Profile,
			PriorityWeight: g.PriorityWeight,
			MaxConcurrency: g.MaxConcurrency,
			Members:        count,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"groups": resp})
}

func (a *AdminHandler) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	ks := a.handler.GetKeyStore()
	if ks == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Multi-user not enabled")
		return
	}
	var req struct {
		Name           string  `json:"name"`
		Profile        string  `json:"profile"`
		PriorityWeight float64 `json:"priorityWeight"`
		MaxConcurrency int     `json:"maxConcurrency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	id, err := ks.CreateGroup(req.Name, req.Profile, req.PriorityWeight, req.MaxConcurrency)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

func (a *AdminHandler) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	ks := a.handler.GetKeyStore()
	if ks == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Multi-user not enabled")
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/_admin/groups/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "Invalid group ID")
		return
	}
	var req struct {
		Profile        string  `json:"profile"`
		PriorityWeight float64 `json:"priorityWeight"`
		MaxConcurrency int     `json:"maxConcurrency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := ks.UpdateGroup(id, req.Profile, req.PriorityWeight, req.MaxConcurrency); err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (a *AdminHandler) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	ks := a.handler.GetKeyStore()
	if ks == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Multi-user not enabled")
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/_admin/groups/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "Invalid group ID")
		return
	}
	if err := ks.DeleteGroup(id); err != nil {
		writeAdminError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// --- Multi-user: QoS status ---

func (a *AdminHandler) handleQoSStatus(w http.ResponseWriter, r *http.Request) {
	engine := a.handler.GetQoSEngine()
	if engine == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "QoS not enabled")
		return
	}
	resp := map[string]interface{}{
		"globalMax": engine.GlobalMax(),
		"groups":    engine.GetStats(),
	}
	if engine.ProviderTracker() != nil {
		resp["providers"] = engine.ProviderTracker().GetProviderStats()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (a *AdminHandler) handleResetProvider(w http.ResponseWriter, r *http.Request) {
	engine := a.handler.GetQoSEngine()
	if engine == nil || engine.ProviderTracker() == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "QoS not enabled")
		return
	}
	providerName := strings.TrimPrefix(r.URL.Path, "/_admin/qos/provider/")
	providerName = strings.TrimSuffix(providerName, "/reset")
	cfg := a.handler.GetConfig()
	idx := -1
	for name := range cfg.Providers {
		idx++
		if name == providerName {
			engine.ProviderTracker().ResetProvider(idx)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
			return
		}
	}
	writeAdminError(w, http.StatusNotFound, fmt.Sprintf("Provider not found: %s", providerName))
}

// writeAdminError writes a JSON error response for admin API endpoints.
func writeAdminError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}