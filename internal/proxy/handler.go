package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/auth"
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/qos"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

const (
	// FilesAPIBetaVersion is the required beta version for Files API
	FilesAPIBetaVersion = "files-api-2025-04-14"
)

// Pre-allocated SSE byte slices to avoid repeated fmt.Sprintf allocations
var (
	sseEventPrefix   = []byte("event: ")
	sseDataPrefix    = []byte("data: ")
	sseNewline       = []byte("\n")
	sseDoubleNewline = []byte("\n\n")
)

// Router interface for handler dependency.
type Router interface {
	DetectRoute(req router.RouteRequest) string
	GetTargets(routeName string) []config.RouteTarget
	SetActiveProfile(profile string)
}

// TransformerRegistry interface for handler dependency.
type TransformerRegistry interface {
	Get(name string) (transformer.Transformer, error)
}

// HTTPClient interface for handler dependency.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
	DoWithContext(ctx context.Context, req *http.Request) (*http.Response, error)
}

// UsageTracker interface for tracking usage statistics.
type UsageTracker interface {
	Record(instanceID, route, model, profile, provider string, tokens, fallbacks int)
}

// UsageTrackerEx is an optional extension of UsageTracker that supports multi-user fields.
type UsageTrackerEx interface {
	Record(instanceID, route, model, profile, provider string, tokens, fallbacks int)
	RecordWithUser(instanceID, route, model, profile, provider string, tokens, fallbacks int, keyPrefix, groupName, userName string)
}

// Handler handles HTTP requests.
type Handler struct {
	maxRequestSize        int64
	router                Router
	transformerRegistry   TransformerRegistry
	providerClients       map[string]HTTPClient
	streamingClients      map[string]HTTPClient
	config                *config.Config
	configMu              sync.RWMutex // Protects config access during hot-reload
	activeProfile         string      // Runtime state - current active profile (not from config)
	usageTracker          UsageTracker
	instanceID            string
	requestInterceptors   []RequestInterceptor
	responseInterceptors  []ResponseInterceptor
	streamingInterceptors []StreamingResponseInterceptor
	adminToken            string // Token for admin API authentication
	multiUserEnabled      bool   // When true, require Bearer token authentication
	authInterceptor       *AuthInterceptor
	qosEngine            *qos.QoSEngine // nil when multi-user disabled
}

// NewHandler creates a new handler.
func NewHandler(maxRequestSize int64) *Handler {
	return &Handler{
		maxRequestSize:   maxRequestSize,
		providerClients: make(map[string]HTTPClient),
		streamingClients: make(map[string]HTTPClient),
	}
}

// SetRouter sets the router.
func (h *Handler) SetRouter(router Router) {
	h.router = router
}

// SetTransformerRegistry sets the transformer registry.
func (h *Handler) SetTransformerRegistry(reg TransformerRegistry) {
	h.transformerRegistry = reg
}

// SetProviderClients sets the provider clients.
func (h *Handler) SetProviderClients(clients map[string]HTTPClient) {
	h.providerClients = clients
}

// SetStreamingClients sets the provider clients for streaming requests.
// These clients have no timeout and are optimized for SSE streaming.
func (h *Handler) SetStreamingClients(clients map[string]HTTPClient) {
	h.streamingClients = clients
}

// SetConfig sets the configuration.
func (h *Handler) SetConfig(cfg *config.Config) {
	h.config = cfg
}

// SetUsageTracker sets the usage tracker.
func (h *Handler) SetUsageTracker(tracker UsageTracker) {
	h.usageTracker = tracker
}

// SetInstanceID sets the instance ID.
func (h *Handler) SetInstanceID(id string) {
	h.instanceID = id
}

// SetRequestInterceptors sets the request interceptors.
func (h *Handler) SetRequestInterceptors(interceptors []RequestInterceptor) {
	h.requestInterceptors = interceptors
}

// SetResponseInterceptors sets the response interceptors.
func (h *Handler) SetResponseInterceptors(interceptors []ResponseInterceptor) {
	h.responseInterceptors = interceptors
}

// SetStreamingInterceptors sets the streaming response interceptors.
func (h *Handler) SetStreamingInterceptors(interceptors []StreamingResponseInterceptor) {
	h.streamingInterceptors = interceptors
}

// SetAdminToken sets the admin API token.
func (h *Handler) SetAdminToken(token string) {
	h.adminToken = token
}

// GetAdminToken returns the admin API token.
func (h *Handler) GetAdminToken() string {
	return h.adminToken
}

// SetMultiUserEnabled enables or disables multi-user authentication.
func (h *Handler) SetMultiUserEnabled(enabled bool) {
	h.multiUserEnabled = enabled
}

// SetAuthInterceptor sets the auth interceptor for multi-user mode.
func (h *Handler) SetAuthInterceptor(ai *AuthInterceptor) {
	h.authInterceptor = ai
}

// GetKeyStore returns the auth KeyStore, if multi-user is enabled.
func (h *Handler) GetKeyStore() *auth.KeyStore {
	if h.authInterceptor != nil {
		return h.authInterceptor.KeyStore
	}
	return nil
}

// GetQoSEngine returns the QoS engine, if set.
func (h *Handler) GetQoSEngine() *qos.QoSEngine {
	return h.qosEngine
}

// GetConfig returns the current configuration (thread-safe).
func (h *Handler) GetConfig() *config.Config {
	h.configMu.RLock()
	defer h.configMu.RUnlock()
	return h.config
}

// SetActiveProfile sets the initial active profile (called during initialization).
func (h *Handler) SetActiveProfile(profile string) {
	h.activeProfile = profile
}

// UpdateActiveProfile switches to a different profile without restart (hot-reload).
// This is called by the admin API when a profile switch request is received.
func (h *Handler) UpdateActiveProfile(profileName string) error {
	h.configMu.Lock()
	defer h.configMu.Unlock()

	if h.config == nil {
		return fmt.Errorf("config not initialized")
	}

	if len(h.config.Router.Profiles) == 0 {
		return fmt.Errorf("no profiles configured")
	}

	if _, ok := h.config.Router.Profiles[profileName]; !ok {
		return fmt.Errorf("profile not found: %s", profileName)
	}

	h.activeProfile = profileName
	// Also update the router engine's active profile
	if h.router != nil {
		h.router.SetActiveProfile(profileName)
	}
	logging.Infof("[ADMIN] Switched to profile: %s", profileName)
	return nil
}

// GetActiveProfile returns the current active profile name (thread-safe).
func (h *Handler) GetActiveProfile() string {
	return h.activeProfile
}

// GetProfiles returns all configured profiles (thread-safe).
func (h *Handler) GetProfiles() map[string]config.ProfileConfig {
	h.configMu.RLock()
	defer h.configMu.RUnlock()
	if h.config == nil {
		return nil
	}
	return h.config.Router.Profiles
}

// AddRequestInterceptor adds a single request interceptor.
func (h *Handler) AddRequestInterceptor(interceptor RequestInterceptor) {
	h.requestInterceptors = append(h.requestInterceptors, interceptor)
}

// AddResponseInterceptor adds a single response interceptor.
func (h *Handler) AddResponseInterceptor(interceptor ResponseInterceptor) {
	h.responseInterceptors = append(h.responseInterceptors, interceptor)
}

// AddStreamingInterceptor adds a single streaming interceptor.
func (h *Handler) AddStreamingInterceptor(interceptor StreamingResponseInterceptor) {
	h.streamingInterceptors = append(h.streamingInterceptors, interceptor)
}

// deepCopyRequest creates a true deep copy of an Anthropic request.
//
// This is critical for failover scenarios where the same request may be sent
// to multiple providers sequentially. Without a deep copy, modifications made
// by one transformer (e.g., signature normalization) could affect subsequent
// provider attempts, causing validation errors.
//
// The deep copy is created using JSON marshal/unmarshal which ensures all
// nested structures (Messages, ContentBlocks, etc.) are independent copies.
// Note: gob encoding was benchmarked but found to be ~30% slower than JSON
// on this platform (see deepcopy_test.go benchmarks).
func deepCopyRequest(req *anthropic.Request) (*anthropic.Request, error) {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request for deep copy: %w", err)
	}
	var reqCopy anthropic.Request
	if err := json.Unmarshal(reqJSON, &reqCopy); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request deep copy: %w", err)
	}
	return &reqCopy, nil
}

// ServeHTTP handles HTTP requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle admin API endpoints (profile management)
	if strings.HasPrefix(r.URL.Path, "/_admin/") {
		if h.adminToken == "" {
			http.Error(w, "Admin API not initialized", http.StatusServiceUnavailable)
			return
		}
		adminHandler := NewAdminHandler(h)
		adminHandler.ServeHTTP(w, r)
		return
	}

	// Multi-user authentication: extract Bearer token and authenticate
	var userInfo *auth.UserInfo
	if h.multiUserEnabled {
		logging.Warnf("[AUTH-DIAG] Incoming request: %s %s | Authorization: %s | X-Api-Key: %s",
			r.Method, r.URL.Path,
			truncateHeader(r.Header.Get("Authorization")),
			truncateHeader(r.Header.Get("x-api-key")))

		var token string
		if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
		if token == "" {
			token = r.Header.Get("x-api-key")
		}
		if token == "" {
			WriteAuthError(w, http.StatusUnauthorized, "Missing or invalid Authorization header")
			return
		}
		logging.Warnf("[AUTH] Extracted token prefix: %s", truncateToken(token))

		info, err := h.authInterceptor.Authenticate(token)
		if err != nil {
			logging.Warnf("[AUTH] Authentication FAILED: %s", err)
			WriteAuthError(w, http.StatusUnauthorized, err.Error())
			return
		}
		userInfo = info

		// Enrich context with user info for downstream use
		ctx := context.WithValue(r.Context(), CtxKeyUser, userInfo)
		r = r.WithContext(ctx)

		// QoS admission control
		if h.qosEngine != nil && userInfo != nil {
			if err := h.qosEngine.Admit(r.Context(), userInfo.GroupName); err != nil {
				WriteQoSRejected(w, err.Error())
				return
			}
			defer h.qosEngine.Release(userInfo.GroupName)
		}
	}

	// Supported Anthropic API v1 endpoints
	supportedPaths := []string{
		"/v1/messages",
		"/v1/messages/with_overrides",
		"/v1/messages/batches",
	}

	isSupported := false
	for _, path := range supportedPaths {
		if r.URL.Path == path {
			isSupported = true
			break
		}
	}

	// Handle GET /v1/models for model listing
	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		h.handleModels(w, r)
		return
	}

	// Handle Files API endpoints
	if strings.HasPrefix(r.URL.Path, "/v1/files") {
		h.handleFilesAPI(w, r)
		return
	}

	// Handle POST to supported message endpoints
	if r.Method == http.MethodPost && isSupported {
		// Read and parse request
		body, err := io.ReadAll(io.LimitReader(r.Body, h.maxRequestSize))
		if err != nil {
			logging.Errorf("[REQUEST VALIDATION] Failed to read request body: %v", err)
			http.Error(w, "Failed to read request", http.StatusBadRequest)
			return
		}
		r.Body.Close()

		var req anthropic.Request
		if err := json.Unmarshal(body, &req); err != nil {
			// Log the actual error at ERROR level
			logging.Errorf("[REQUEST VALIDATION] Invalid request format: %v", err)
			// Log request body snippet at DEBUG level to avoid log spam
			bodySnippet := string(body)
			if len(bodySnippet) > 500 {
				bodySnippet = bodySnippet[:500] + "..."
			}
			logging.Debugf("[REQUEST VALIDATION] Request body snippet: %s", bodySnippet)
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}

		// Log successful request parsing (debug level)
		logging.Debugf("[REQUEST VALIDATION] Successfully parsed request: model=%s, messages=%d, stream=%v",
			req.Model, len(req.Messages), req.Stream)

		// Handle the request
		h.handleMessages(w, r, &req, userInfo)
		return
	}

	http.Error(w, "Not Found", http.StatusNotFound)
}

// handleMessages processes the messages request.
func (h *Handler) handleMessages(w http.ResponseWriter, r *http.Request, req *anthropic.Request, userInfo *auth.UserInfo) {
	// Call request interceptors
	for _, interceptor := range h.requestInterceptors {
		if err := interceptor.InterceptRequest(r.Context(), req); err != nil {
			logging.Errorf("Request interceptor failed: %v", err)
			http.Error(w, fmt.Sprintf("Request interceptor error: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Log incoming request details
	logging.Debugf("[REQUEST] Model: %s, Stream: %v, Messages: %d, MaxTokens: %d",
		req.Model, req.Stream, len(req.Messages), req.MaxTokens)
	if len(req.Tools) > 0 {
		logging.Debugf("[REQUEST] Tools: %d", len(req.Tools))
		for _, tool := range req.Tools {
			logging.Debugf("[REQUEST]   - %s", tool.Name)
		}
	}

	// Detect route
	routeReq := router.RouteRequest{
		IsBackground: h.isBackground(req),
		ThinkLevel:   h.getThinkLevel(req),
		TokenCount:   h.estimateTokens(req),
		HasWebSearch: h.hasWebSearch(req),
		HasImages:    h.hasImages(req),
		IsReview:     h.isReview(req),
		IsSubagent:   h.isSubagent(r, req),
	}
	routeName := h.router.DetectRoute(routeReq)

	// Resolve targets: use user's group profile if authenticated and set
	var targets []config.RouteTarget
	if userInfo != nil && userInfo.Profile != "" {
		h.configMu.RLock()
		if len(h.config.Router.Profiles) > 0 {
			if profile, ok := h.config.Router.Profiles[userInfo.Profile]; ok {
				if route, ok := profile.Routes[routeName]; ok {
					targets = config.ParseRoute(route)
				}
			}
		}
		h.configMu.RUnlock()
	}
	if targets == nil {
		targets = h.router.GetTargets(routeName)
	}

	logging.Infof("[ROUTE] Detected: %s (bg=%v sub=%v review=%v think=%d img=%v web=%v tokens=%d), Targets: %d",
		routeName, routeReq.IsBackground, routeReq.IsSubagent, routeReq.IsReview, routeReq.ThinkLevel, routeReq.HasImages, routeReq.HasWebSearch, routeReq.TokenCount, len(targets))

	// Handle streaming requests
	if req.Stream {
		h.handleStreaming(w, r, req, routeName, targets, userInfo)
		return
	}

	// Try each target with failover for non-streaming
	var successfulModel string
	var lastErr error

	for i, target := range targets {
		// FAILOVER LOGGING: Log attempt start with request details
		logging.Debugf("[FAILOVER] Attempt %d with %s/%s, messages: %d", i, target.Provider, target.Model, len(req.Messages))
		// Log thinking block count for debugging
		for msgIdx, msg := range req.Messages {
			thinkingCount := 0
			for _, block := range msg.Content {
				if block.Type == "thinking" {
					thinkingCount++
				}
			}
			if thinkingCount > 0 {
				logging.Debugf("[FAILOVER] Message[%d] has %d thinking blocks", msgIdx, thinkingCount)
			}
		}

		resp, err := h.tryTarget(r.Context(), req, target)
		if err != nil {
			lastErr = err
			logging.Errorf("Target %d (%s/%s) failed: %v", i, target.Provider, target.Model, err)
			logging.Debugf("[FAILOVER] Falling back to next provider")
			continue
		}

		successfulModel = target.Model

		// Call response interceptors
		for _, interceptor := range h.responseInterceptors {
			if err := interceptor.InterceptResponse(r.Context(), req, resp); err != nil {
				logging.Errorf("Response interceptor failed: %v", err)
				http.Error(w, fmt.Sprintf("Response interceptor error: %v", err), http.StatusInternalServerError)
				return
			}
		}

		// Track usage with actual provider-reported token counts
		if h.usageTracker != nil {
			totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
			// If provider doesn't return usage, fall back to estimate
			if totalTokens == 0 {
				totalTokens = h.estimateTokens(req)
				logging.Warnf("[USAGE] Provider didn't return usage data, using estimate: %d tokens", totalTokens)
			} else {
				logging.Infof("[USAGE] Tracking actual usage: %d tokens (input=%d, output=%d)",
					totalTokens, resp.Usage.InputTokens, resp.Usage.OutputTokens)
			}
			// Use user's group profile if authenticated
			profile := h.activeProfile
			if userInfo != nil && userInfo.Profile != "" {
				profile = userInfo.Profile
			}
			if ex, ok := h.usageTracker.(UsageTrackerEx); ok && userInfo != nil {
				ex.RecordWithUser(h.instanceID, routeName, successfulModel, profile, target.Provider, totalTokens, i, userInfo.KeyPrefix, userInfo.GroupName, userInfo.UserName)
			} else {
				h.usageTracker.Record(h.instanceID, routeName, successfulModel, profile, target.Provider, totalTokens, i)
			}
		}

		// Log response details
		logging.Infof("[RESPONSE] Success with %s/%s, StopReason: %s, InputTokens: %d, OutputTokens: %d",
			target.Provider, target.Model, resp.StopReason, resp.Usage.InputTokens, resp.Usage.OutputTokens)

		// Write response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	errorMsg := "All providers failed"
	if lastErr != nil {
		errorMsg = fmt.Sprintf("All providers failed: %v", lastErr)
	}
	logging.Errorf("All providers failed for route: %s, last error: %v", routeName, lastErr)
	http.Error(w, errorMsg, http.StatusBadGateway)
}

// handleStreaming processes a streaming messages request.
func (h *Handler) handleStreaming(w http.ResponseWriter, r *http.Request, req *anthropic.Request, routeName string, targets []config.RouteTarget, userInfo *auth.UserInfo) {
	// Call request interceptors
	for _, interceptor := range h.requestInterceptors {
		if err := interceptor.InterceptRequest(r.Context(), req); err != nil {
			logging.Errorf("Request interceptor failed: %v", err)
			h.sendStreamingError(w, "Request interceptor error", err)
			return
		}
	}

	logging.Streamf("Starting streaming request, route: %s, targets: %d", routeName, len(targets))

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Get flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Derive the upstream context from the client request so that if Claude Code
	// disconnects (exits), the upstream SSE stream is cancelled promptly and the
	// request goroutine returns. Each failover attempt below still gets its own
	// child context via context.WithCancel(baseCtx), so a failed attempt's
	// cancellation does not cascade to subsequent attempts.
	baseCtx := r.Context()

	// Try each target with failover
	for i, target := range targets {
		// Create a FRESH context for each provider attempt
		// This ensures that if one provider fails, it doesn't affect others
		ctx, cancel := context.WithCancel(baseCtx)
		defer cancel()

		// FAILOVER LOGGING: Log attempt start with request details
		logging.StreamDebugf("[FAILOVER] Streaming attempt %d with %s/%s, messages: %d", i, target.Provider, target.Model, len(req.Messages))
		// Log thinking block count for debugging
		for msgIdx, msg := range req.Messages {
			thinkingCount := 0
			for _, block := range msg.Content {
				if block.Type == "thinking" {
					thinkingCount++
				}
			}
			if thinkingCount > 0 {
				logging.StreamDebugf("[FAILOVER] Message[%d] has %d thinking blocks", msgIdx, thinkingCount)
			}
		}

		totalTokens, err := h.tryStreamingTarget(ctx, w, flusher, req, target)
		if err != nil {
			logging.Streamf("Target %d (%s/%s) failed: %v", i, target.Provider, target.Model, err)
			logging.StreamDebugf("[FAILOVER] Falling back to next provider")
			// Note: We don't need to explicitly cancel here because
			// each iteration creates a fresh context
			continue
		}

		// Track usage on successful stream with actual token counts
		if h.usageTracker != nil {
			// Use actual total tokens if available, otherwise fall back to estimate
			tokensToTrack := totalTokens
			if tokensToTrack == 0 {
				tokensToTrack = h.estimateTokens(req)
				logging.Streamf("[USAGE] No usage data from stream, using estimate: %d tokens", tokensToTrack)
			} else {
				logging.Streamf("[USAGE] Tracking actual usage: %d tokens", tokensToTrack)
			}
			// Use user's group profile if authenticated
			profile := h.activeProfile
			if userInfo != nil && userInfo.Profile != "" {
				profile = userInfo.Profile
			}
			if ex, ok := h.usageTracker.(UsageTrackerEx); ok && userInfo != nil {
				ex.RecordWithUser(h.instanceID, routeName, target.Model, profile, target.Provider, tokensToTrack, i, userInfo.KeyPrefix, userInfo.GroupName, userInfo.UserName)
			} else {
				h.usageTracker.Record(h.instanceID, routeName, target.Model, profile, target.Provider, tokensToTrack, i)
			}
		}
		logging.Streamf("Stream completed with %s/%s, fallbacks: %d", target.Provider, target.Model, i)
		return
	}

	// All providers failed - write error as SSE event following Anthropic's format
	h.sendStreamingError(w, "All providers failed", fmt.Errorf("all %d providers failed for route: %s", len(targets), routeName))
}

// sendStreamingError sends an error event in SSE format following Anthropic's error format.
func (h *Handler) sendStreamingError(w http.ResponseWriter, message string, err error) {
	logging.Errorf("Streaming error: %s: %v", message, err)

	// Anthropic's SSE error format
	errorJSON := fmt.Sprintf("{\"type\": \"error\", \"error\": {\"type\": \"api_error\", \"message\": \"%s: %s\"}}", message, err)
	w.Write(sseEventPrefix)
	w.Write([]byte("error"))
	w.Write(sseNewline)
	w.Write(sseDataPrefix)
	w.Write([]byte(errorJSON))
	w.Write(sseDoubleNewline)

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (h *Handler) tryTarget(ctx context.Context, req *anthropic.Request, target config.RouteTarget) (*anthropic.Response, error) {
	// Get provider config
	providerCfg, ok := h.config.Providers[target.Provider]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", target.Provider)
	}

	// Check if compaction is needed for this provider
	// Compaction reduces request size to fit within provider limits
	compactedReq, didCompact, err := CompactRequestIfNeeded(req, h, target.Provider, providerCfg)
	if err != nil {
		return nil, fmt.Errorf("compaction failed: %w", err)
	}
	if didCompact {
		logging.Debugf("[COMPACTOR] Request compacted for provider %s, messages reduced from %d to %d",
			target.Provider, len(req.Messages), len(compactedReq.Messages))
	}

	// Get transformer
	transformerName := providerCfg.Transformer
	if transformerName == "" {
		transformerName = target.Provider
	}
	tf, err := h.transformerRegistry.Get(transformerName)
	if err != nil {
		tf, _ = h.transformerRegistry.Get("anthropic")
	}

	// Get client
	client, ok := h.providerClients[target.Provider]
	if !ok {
		return nil, fmt.Errorf("client not found: %s", target.Provider)
	}

	// CRITICAL: Create deep copy before passing to transformer
	// This prevents state corruption during failover when multiple providers
	// are attempted sequentially with the same request object.
	// Use compacted request if compaction occurred, otherwise use original
	reqCopy, err := deepCopyRequest(compactedReq)
	if err != nil {
		return nil, fmt.Errorf("failed to copy request: %w", err)
	}

	// Prepare request: Anthropic -> Provider HTTP Request
	httpReq, err := tf.PrepareRequest(reqCopy, providerCfg.BaseURL, providerCfg.APIKey, target.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare request: %w", err)
	}

	// Log request details for debugging (with nil safety for httpReq and URL)
	// SECURITY: Sanitize headers to prevent API key leakage in logs
	if httpReq != nil && httpReq.URL != nil {
		logging.Debugf("[PROXY REQUEST] URL: %s, Method: %s, Headers: %s", httpReq.URL.String(), httpReq.Method, logging.SanitizeHeadersString(httpReq.Header))
	} else if httpReq != nil {
		logging.Debugf("[PROXY REQUEST] URL: <nil>, Method: %s, Headers: %s", httpReq.Method, logging.SanitizeHeadersString(httpReq.Header))
	} else {
		logging.Debugf("[PROXY REQUEST] URL: <nil>, Method: <nil>, Headers: <nil>")
	}

	// Send request
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check for error responses before attempting to parse JSON
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		urlStr := "<nil>"
		if httpReq != nil && httpReq.URL != nil {
			urlStr = httpReq.URL.String()
		}
		// Log error summary at ERROR level
		logging.Errorf("[PROXY ERROR] URL: %s, Status: %s", urlStr, resp.Status)
		// Log full response body at DEBUG level to avoid log spam with large error responses
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			logging.Debugf("[PROXY ERROR] Response (first 500 chars): %s...", bodyStr[:500])
		} else {
			logging.Debugf("[PROXY ERROR] Response: %s", bodyStr)
		}

		// Check for specific error code 1213 with enhanced logging
		if isErrorCode1213(bodyStr) {
			// Log additional diagnostic info for 1213 errors
			// SECURITY: Sanitize headers to prevent API key leakage in logs
			contentLength := int64(0)
			if httpReq != nil {
				contentLength = httpReq.ContentLength
			}
			logging.Errorf("[PROXY ERROR 1213] DETAILED - URL: %s, ContentLength: %d, Request Headers: %s, Response: %s",
				urlStr, contentLength, logging.SanitizeHeadersString(httpReq.Header), bodyStr)
			// Return a specific error that can be handled differently
			return nil, fmt.Errorf("API error 1213 (prompt not received): %s - %s", resp.Status, bodyStr)
		}
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, bodyStr)
	}

	// Parse response: Provider Response -> Anthropic
	const maxBodySize = 10 * 1024 * 1024 // 10 MB
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	anthropicResp, err := tf.ParseResponse(resp)
	if err != nil {
		bodyPreview := strings.ReplaceAll(string(bodyBytes), "\n", " ")
		bodyPreview = strings.ReplaceAll(bodyPreview, "\r", " ")
		if len([]rune(bodyPreview)) > 500 {
			bodyPreview = string([]rune(bodyPreview)[:500]) + "..."
		}
		logging.Debugf("[PARSE ERROR] Provider response body (status %s): %s", resp.Status, bodyPreview)
		return nil, fmt.Errorf("failed to parse provider response: %w", err)
	}
	return anthropicResp, nil
}

// tryStreamingTarget attempts to send a streaming request to a target provider.
// Returns the total tokens used (input + output) extracted from usage events, or 0 if unavailable.
func (h *Handler) tryStreamingTarget(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, req *anthropic.Request, target config.RouteTarget) (int, error) {
	logging.Streamf("Starting stream to %s/%s", target.Provider, target.Model)

	// Get provider config
	providerCfg, ok := h.config.Providers[target.Provider]
	if !ok {
		return 0, fmt.Errorf("provider not found: %s", target.Provider)
	}

	// Check if compaction is needed for this provider
	// Compaction reduces request size to fit within provider limits
	compactedReq, didCompact, err := CompactRequestIfNeeded(req, h, target.Provider, providerCfg)
	if err != nil {
		return 0, fmt.Errorf("compaction failed: %w", err)
	}
	if didCompact {
		logging.Streamf("[COMPACTOR] Request compacted for provider %s, messages reduced from %d to %d",
			target.Provider, len(req.Messages), len(compactedReq.Messages))
	}

	// Get transformer
	transformerName := providerCfg.Transformer
	if transformerName == "" {
		transformerName = target.Provider
	}
	tf, err := h.transformerRegistry.Get(transformerName)
	if err != nil {
		tf, _ = h.transformerRegistry.Get("anthropic")
	}

	// Check if transformer supports streaming
	if !tf.SupportsStreaming() {
		return 0, fmt.Errorf("transformer does not support streaming")
	}

	// Get streaming client (falls back to regular client if streaming client not available)
	// Streaming clients have no timeout to allow long-running SSE streams
	client, ok := h.streamingClients[target.Provider]
	if !ok {
		// Fall back to regular client if streaming client not available
		client, ok = h.providerClients[target.Provider]
		if !ok {
			return 0, fmt.Errorf("client not found: %s", target.Provider)
		}
		logging.Streamf("[STREAM] Using regular client for %s (no streaming client available)", target.Provider)
	}

	// CRITICAL: Create deep copy before passing to transformer
	// This prevents state corruption during failover when multiple providers
	// are attempted sequentially with the same request object.
	// Use compacted request if compaction occurred, otherwise use original
	reqCopy, err := deepCopyRequest(compactedReq)
	if err != nil {
		return 0, fmt.Errorf("failed to copy request: %w", err)
	}
	reqCopy.Stream = true

	// Prepare request: Anthropic -> Provider HTTP Request
	httpReq, err := tf.PrepareRequest(reqCopy, providerCfg.BaseURL, providerCfg.APIKey, target.Model)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare request: %w", err)
	}

	// Log request details for debugging (with nil safety for httpReq and URL)
	if httpReq != nil && httpReq.URL != nil {
		logging.StreamDebugf("[PROXY STREAM REQUEST] URL: %s, Method: %s", httpReq.URL.String(), httpReq.Method)
	} else if httpReq != nil {
		logging.StreamDebugf("[PROXY STREAM REQUEST] URL: <nil>, Method: %s", httpReq.Method)
	} else {
		logging.StreamDebugf("[PROXY STREAM REQUEST] URL: <nil>, Method: <nil>")
	}

	// Send request with context for proper cancellation and timeout propagation
	resp, err := client.DoWithContext(ctx, httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		urlStr := "<nil>"
		if httpReq != nil && httpReq.URL != nil {
			urlStr = httpReq.URL.String()
		}
		// Log error summary at INFO level
		logging.Streamf("[PROXY STREAM ERROR] URL: %s, Status: %s", urlStr, resp.Status)
		// Log full response body at DEBUG level to avoid log spam
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			logging.StreamDebugf("[PROXY STREAM ERROR] Response (first 500 chars): %s...", bodyStr[:500])
		} else {
			logging.StreamDebugf("[PROXY STREAM ERROR] Response: %s", bodyStr)
		}

		// Check for specific error code 1213 with enhanced logging
		if isErrorCode1213(bodyStr) {
			contentLength := int64(0)
			if httpReq != nil {
				contentLength = httpReq.ContentLength
			}
			logging.Streamf("[PROXY STREAM ERROR 1213] DETAILED - URL: %s, ContentLength: %d, Request Headers: %s, Response: %s",
				urlStr, contentLength, logging.SanitizeHeadersString(httpReq.Header), bodyStr)
			return 0, fmt.Errorf("API error 1213 (prompt not received): %s - %s", resp.Status, bodyStr)
		}
		return 0, fmt.Errorf("API error: %s - %s", resp.Status, bodyStr)
	}

	// Read and forward SSE stream
	// Note: We do NOT emit synthetic message_start/content_block_start events here
	// because providers like GLM already send these events. Emitting synthetic ones
	// would cause duplicate events that break the client's parsing.
	scanner := NewSSEScanner(resp.Body)
	defer scanner.Close()

	eventCount := 0
	var totalOutputTokens int
	var totalInputTokens int
	for scanner.Scan() {
		eventCount++
		eventType := scanner.Event()
		data := scanner.Data()

		// DEBUG: Log raw SSE event received from provider
		logging.StreamDebugf("[RAW SSE] Event #%d - type: '%s', data: %s", eventCount, eventType, string(data))

		// Filter out non-Anthropic events that some providers send (e.g., GLM's "ping")
		if eventType == "ping" || eventType == "keepalive" {
			logging.StreamDebugf("[FILTER] Filtering out non-Anthropic event: %s", eventType)
			continue
		}

		if len(data) == 0 {
			logging.StreamDebugf("[FILTER] Skipping empty data event")
			continue
		}

		// Validate JSON before processing
		if !json.Valid(data) {
			logging.StreamDebugf("[FILTER] Invalid JSON data from provider, skipping: %s", string(data))
			continue
		}

		// Create SSE event and transform it
		sseEvent := &transformer.SSEEvent{
			EventType: eventType,
			Data:      data,
		}

		transformedEvents, err := tf.TransformStreamEvent(sseEvent)
		if err != nil {
			logging.StreamDebugf("[TRANSFORM ERROR] Transform error: %v, raw data: %s", err, string(data))
			continue
		}

		// Detect provider error events (e.g., BigModel error code 1213) and trigger failover.
		// BigModel returns HTTP 200 but sends SSE error events mid-stream instead of
		// failing the HTTP request. Without this check, the error is forwarded to Claude
		// Code and no failover occurs.
		for _, te := range transformedEvents {
			if te.EventType == "error" {
				var errData map[string]interface{}
				if json.Unmarshal(te.Data, &errData) == nil {
					if errObj, ok := errData["error"].(map[string]interface{}); ok {
						errMsg := fmt.Sprintf("Provider stream error: %v", errObj["message"])
						if code, ok := errObj["code"]; ok {
							errMsg = fmt.Sprintf("Provider stream error (code %v): %v", code, errObj["message"])
						}
						logging.Streamf("[STREAM ERROR] Provider sent error SSE event, triggering failover: %s", errMsg)
						return 0, fmt.Errorf("%s", errMsg)
					}
				}
				logging.Streamf("[STREAM ERROR] Provider sent unrecognized error event, triggering failover: %s", string(te.Data))
				return 0, fmt.Errorf("Provider sent error event: %s", string(te.Data))
			}
		}

		// Extract usage data from message_delta events
		for _, te := range transformedEvents {
			if te.EventType == "message_delta" {
				// Parse the event to extract usage information
				var eventData map[string]interface{}
				if json.Unmarshal(te.Data, &eventData) == nil {
					if usage, ok := eventData["usage"].(map[string]interface{}); ok {
						// Extract output tokens (some providers send this)
						if outputTokens, ok := usage["output_tokens"].(float64); ok {
							totalOutputTokens += int(outputTokens)
							logging.StreamDebugf("[USAGE] Accumulated output_tokens: %d (total: %d)", int(outputTokens), totalOutputTokens)
						}
						// Extract input tokens if provider sends it (e.g., GLM sends both in message_delta)
						if inputTokens, ok := usage["input_tokens"].(float64); ok {
							totalInputTokens = int(inputTokens)
							logging.StreamDebugf("[USAGE] Provider sent input_tokens: %d (using actual instead of estimate)", totalInputTokens)
						}
					}
				}
			}
		}

		// DEBUG: Log transformed events
		logging.StreamDebugf("[TRANSFORMED] %d event(s) produced from raw event", len(transformedEvents))
		for i, te := range transformedEvents {
			logging.StreamDebugf("[TRANSFORMED] Event #%d: type='%s', data=%s", i+1, te.EventType, string(te.Data))
		}

		// Write all transformed events
		for _, te := range transformedEvents {
			if len(te.Data) == 0 {
				logging.StreamDebugf("[FILTER] Skipping empty event data, type: %s", te.EventType)
				continue
			}
			if !json.Valid(te.Data) {
				logging.StreamDebugf("[FILTER] Skipping invalid JSON event data, type: %s, data: %s", te.EventType, string(te.Data))
				continue
			}

			// Apply streaming interceptors
			interceptedData := te.Data
			var interceptorErr error
			for _, interceptor := range h.streamingInterceptors {
				interceptedData, interceptorErr = interceptor.InterceptStreamingEvent(ctx, req, te.EventType, te.Data)
				if interceptorErr != nil {
					logging.StreamDebugf("[INTERCEPTOR ERROR] Streaming interceptor error: %v", interceptorErr)
					// Continue with original data on interceptor error
					interceptedData = te.Data
				}
			}

			// Semantic validation for critical event types to prevent
			// "undefined is not an object" errors in client JavaScript
			var parsedEvent map[string]any
			if json.Unmarshal(interceptedData, &parsedEvent) == nil {
				// Validate content_block_delta events
				if te.EventType == "content_block_delta" || parsedEvent["type"] == "content_block_delta" {
					if delta, ok := parsedEvent["delta"].(map[string]any); ok {
						if deltaType, ok := delta["type"].(string); ok && deltaType == "text_delta" {
							if _, hasText := delta["text"]; !hasText {
								logging.StreamDebugf("[FILTER] Skipping text_delta event with missing text field, data: %s", string(interceptedData))
								continue
							}
						}
					}
				}
				// Validate content_block_stop has required index field
				if te.EventType == "content_block_stop" || parsedEvent["type"] == "content_block_stop" {
					if _, hasIndex := parsedEvent["index"]; !hasIndex {
						logging.StreamDebugf("[FILTER] Skipping content_block_stop event with missing index field, data: %s", string(interceptedData))
						continue
					}
				}
			}

			// Determine event format based on event type
			if te.EventType != "" {
				// Write event with explicit event type (direct byte writes, no fmt.Sprintf)
				w.Write(sseEventPrefix)
				w.Write([]byte(te.EventType))
				w.Write(sseNewline)
			}
			w.Write(sseDataPrefix)
			w.Write(interceptedData)
			w.Write(sseDoubleNewline)
			flusher.Flush()

			// DEBUG: Log successfully written event
			logging.StreamDebugf("[WRITE] Successfully wrote event to client: type='%s', data: %s", te.EventType, string(interceptedData))
		}
	}

	logging.Streamf("[STREAM SUMMARY] Stream completed with %d raw events processed, Input: %d, Output: %d", eventCount, totalInputTokens, totalOutputTokens)

	if scanner.Err() != nil {
		// Send message_stop before returning error to properly close the stream
		messageStopData, err := json.Marshal(map[string]string{
			"type": "message_stop",
		})
		if err != nil {
			logging.Errorf("[STREAM] Failed to marshal message_stop event: %v", err)
		} else if len(messageStopData) > 0 {
			w.Write(sseEventPrefix)
			w.Write([]byte("message_stop"))
			w.Write(sseNewline)
			w.Write(sseDataPrefix)
			w.Write(messageStopData)
			w.Write(sseDoubleNewline)
			flusher.Flush()
		}
		return 0, scanner.Err()
	}

	// Calculate total tokens (use actual input if provider sent it, otherwise estimate)
	// Note: Some providers (like GLM) send input_tokens in message_delta events
	inputTokens := totalInputTokens
	if inputTokens == 0 {
		inputTokens = h.estimateTokens(req) // Fall back to estimate
	}
	totalTokens := inputTokens + totalOutputTokens
	logging.Streamf("Stream completed successfully. Input: %d (%s), Output: %d, Total: %d",
		inputTokens,
		map[bool]string{true: "actual", false: "estimated"}[totalInputTokens > 0],
		totalOutputTokens,
		totalTokens)

	return totalTokens, nil
}

func (h *Handler) estimateTokens(req *anthropic.Request) int {
	// Rough estimation: ~4 chars per token
	total := 0
	for _, msg := range req.Messages {
		for _, block := range msg.Content {
			if block.Type == "text" {
				total += len(block.Text) / 4
			}
		}
	}
	return total
}

func (h *Handler) hasWebSearch(req *anthropic.Request) bool {
	for _, tool := range req.Tools {
		if strings.Contains(strings.ToLower(tool.Name), "web") ||
			strings.Contains(strings.ToLower(tool.Name), "search") {
			return true
		}
	}
	return false
}

func (h *Handler) hasImages(req *anthropic.Request) bool {
	for _, msg := range req.Messages {
		for _, block := range msg.Content {
			if block.Type == "image" {
				return true
			}
		}
	}
	return false
}

// isBackground detects if this is a background agent request.
// Claude Code uses Haiku models for background agents.
func (h *Handler) isBackground(req *anthropic.Request) bool {
	model := strings.ToLower(req.Model)
	return strings.Contains(model, "claude") && strings.Contains(model, "haiku")
}

// getThinkLevel detects the thinking level based on budget_tokens.
// Levels: NONE (0), BASIC (~4K), MIDDLE (~10K), HIGHEST (~32K)
func (h *Handler) getThinkLevel(req *anthropic.Request) router.ThinkLevel {
	if req.Thinking == nil || req.Thinking.BudgetTokens <= 0 {
		return router.ThinkNone
	}

	budget := req.Thinking.BudgetTokens
	switch {
	case budget >= router.ThinkLevelHighest:
		return router.ThinkHighest
	case budget >= router.ThinkLevelMiddle:
		return router.ThinkMiddle
	case budget >= router.ThinkLevelBasic:
		return router.ThinkBasic
	default:
		return router.ThinkBasic // Any non-zero budget is at least basic
	}
}

// lastUserMessage returns the last user message from the message list, or nil if none exists.
func lastUserMessage(messages []anthropic.Message) *anthropic.Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == anthropic.RoleUser {
			return &messages[i]
		}
	}
	return nil
}

// isReview checks if the request is a review task based on message content.
// Note: /review is a slash command (user message), not a tool.
// MCP tools like create_pull_request_review are CRUD operations, not review task indicators.
func (h *Handler) isReview(req *anthropic.Request) bool {
	// Check only the last user message — conversation history may contain
	// review keywords from earlier turns that should not classify later requests.
	if msg := lastUserMessage(req.Messages); msg != nil {
		for _, block := range msg.Content {
			if block.Type == "text" {
				text := strings.ToLower(block.Text)
				if strings.Contains(text, "/review") ||
					strings.Contains(text, "code review") ||
					strings.Contains(text, "review this") ||
					strings.Contains(text, "review the") ||
					strings.HasPrefix(text, "review ") {
					return true
				}
			}
		}
	}

	return false
}

// isSubagent checks if the request originates from a Claude Code subagent.
// Primary signal: X-Claude-Code-Agent-Id header (set by Claude Code v2.1.139+
// on subagent dispatches, absent on main-agent requests).
// Fallback heuristics: tool names / last user message containing subagent
// keywords (for older Claude Code versions).
func (h *Handler) isSubagent(r *http.Request, req *anthropic.Request) bool {
	// Primary: official Claude Code subagent identification headers.
	if r.Header.Get("X-Claude-Code-Agent-Id") != "" ||
		r.Header.Get("X-Claude-Code-Parent-Agent-Id") != "" {
		return true
	}

	// Fallback: check tool names for subagent indicators.
	for _, tool := range req.Tools {
		toolName := strings.ToLower(tool.Name)
		if strings.Contains(toolName, "subagent") {
			return true
		}
	}

	// Fallback: check only the last user message — conversation history may
	// contain subagent keywords from earlier turns that should not classify
	// later requests.
	if msg := lastUserMessage(req.Messages); msg != nil {
		for _, block := range msg.Content {
			if block.Type == "text" {
				text := strings.ToLower(block.Text)
				if strings.Contains(text, "subagent") ||
					strings.Contains(text, "delegate to agent") {
					return true
				}
			}
		}
	}

	return false
}

// handleModels returns the available models from all configured providers.
func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	type Model struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}

	type ModelsResponse struct {
		Object string  `json:"object"`
		Data   []Model `json:"data"`
	}

	var models []Model
	for _, providerCfg := range h.config.Providers {
		for _, modelID := range providerCfg.Models {
			models = append(models, Model{
				ID:     modelID,
				Object: "model",
			})
		}
	}

	response := ModelsResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleFilesAPI handles Files API endpoints.
func (h *Handler) handleFilesAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Validate anthropic-beta header as per Files API spec
	betaHeader := r.Header.Get("anthropic-beta")
	if betaHeader == "" {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"Missing required header: anthropic-beta: "+FilesAPIBetaVersion}}`, http.StatusBadRequest)
		return
	}

	// Check if the correct beta version is specified
	// The header can contain multiple beta versions separated by commas
	betaVersions := strings.Split(betaHeader, ",")
	hasCorrectBeta := false
	for _, v := range betaVersions {
		if strings.TrimSpace(v) == FilesAPIBetaVersion {
			hasCorrectBeta = true
			break
		}
	}

	if !hasCorrectBeta {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"Invalid beta version. Required: anthropic-beta: `+FilesAPIBetaVersion+`"}}`, http.StatusBadRequest)
		return
	}

	// Extract file ID from path for operations that need it
	fileID := strings.TrimPrefix(r.URL.Path, "/v1/files/")

	switch r.Method {
	case http.MethodPost:
		// POST /v1/files - Upload file
		if r.URL.Path == "/v1/files" {
			h.handleFileUpload(w, r)
		} else {
			http.Error(w, "Not Found", http.StatusNotFound)
		}

	case http.MethodGet:
		// GET /v1/files - List files
		// GET /v1/files/{id} - Get file details
		if fileID == "" || fileID == "/v1/files" {
			h.handleFileList(w, r)
		} else {
			h.handleFileGet(w, r, fileID)
		}

	case http.MethodDelete:
		// DELETE /v1/files/{id} - Delete file
		if fileID != "" && fileID != "/v1/files" {
			h.handleFileDelete(w, r, fileID)
		} else {
			http.Error(w, "Bad Request - file ID required", http.StatusBadRequest)
		}

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleFileUpload handles file uploads.
//
// IMPORTANT: Claude Code does NOT use the Files API. These handlers exist only for
// Anthropic API completeness. File storage and resolution are NOT implemented.
//
// Files API allows uploading files that can be referenced via file_id in document blocks.
// Since Claude Code doesn't use this, file_id references in document blocks are not
// resolved when routing to non-Anthropic providers (OpenAI, Gemini, etc.).
func (h *Handler) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	// MOCK IMPLEMENTATION - Returns fake response without storing anything
	// This is sufficient for API completeness since Claude Code doesn't use Files API
	response := anthropic.FileUploadResponse{
		ID:           "file-" + generateID(),
		Type:         "file",
		CreatedAt:    time.Now(),
		SizeBytes:    0,
		Filename:     "uploaded_file",
		MimeType:     "application/octet-stream",
		Downloadable: false,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleFileList handles listing files.
//
// NOTE: Claude Code does NOT use Files API. This returns an empty list.
// File storage is not implemented.
func (h *Handler) handleFileList(w http.ResponseWriter, r *http.Request) {
	response := anthropic.FileListResponse{
		Object:  "list",
		Data:    []anthropic.FileObject{}, // Empty - no file storage implemented
		HasMore: false,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleFileGet handles getting file details.
//
// NOTE: Claude Code does NOT use Files API. This returns mock data.
// File storage is not implemented.
func (h *Handler) handleFileGet(w http.ResponseWriter, r *http.Request, fileID string) {
	// MOCK IMPLEMENTATION - Returns fake data without looking up anything
	response := anthropic.FileObject{
		ID:           fileID,
		Type:         "file",
		CreatedAt:    time.Now(),
		SizeBytes:    0,
		Filename:     "example.pdf",
		MimeType:     "application/pdf",
		Downloadable: false,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleFileDelete handles file deletion.
//
// NOTE: Claude Code does NOT use Files API. This returns success without doing anything.
// File storage is not implemented.
func (h *Handler) handleFileDelete(w http.ResponseWriter, r *http.Request, fileID string) {
	// MOCK IMPLEMENTATION - Returns success without deleting anything
	response := anthropic.FileDeleteResponse{
		ID:      fileID,
		Type:    "file",
		Deleted: true,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// generateID generates a random ID for files.
func generateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 24)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// truncateToken returns the first 12 characters of a token followed by "..." for safe logging.
func truncateToken(token string) string {
	if len(token) > 12 {
		return token[:12] + "..."
	}
	return token
}

// truncateHeader safely logs an Authorization or X-Api-Key header value,
// showing the scheme (e.g. "Bearer") and a truncated token.
func truncateHeader(value string) string {
	if value == "" {
		return "<empty>"
	}
	parts := strings.SplitN(value, " ", 2)
	if len(parts) == 2 {
		return parts[0] + " " + truncateToken(parts[1])
	}
	return truncateToken(value)
}

// isErrorCode1213 checks if the response body contains error code 1213
// which indicates "prompt parameter not properly received" from BigModel/GLM providers.
func isErrorCode1213(bodyStr string) bool {
	return strings.Contains(bodyStr, "\"code\":\"1213\"") || strings.Contains(bodyStr, `"code":1213`) || strings.Contains(bodyStr, "未正常接收到prompt参数")
}
