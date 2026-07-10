// Package proxy implements the HTTP proxy server.
package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/qos"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"golang.org/x/crypto/acme/autocert"
)

// ServerConfig represents server configuration.
type ServerConfig struct {
	Host           string
	Port           int
	MaxRequestSize int64
	// TLS enables HTTPS on the listener. Nil/empty = plaintext HTTP.
	TLS *config.TLSConfig
}

// Defaults returns default server configuration.
func Defaults() *ServerConfig {
	return &ServerConfig{
		Host:           "localhost",
		Port:           8081,
		MaxRequestSize: 50 * 1024 * 1024, // 50MB
	}
}

// Server is the HTTP proxy server.
type Server struct {
	config        *ServerConfig
	server        *http.Server
	redirectServer *http.Server // Optional :80 HTTP→HTTPS redirect (and ACME http-01) server
	handler       *Handler
	usageTracker  UsageTracker
	instanceID    string
	mu            sync.Mutex
	running       bool
	ready         chan struct{} // Closed when server is ready to accept connections
	actualAddr    string        // Actual bound address (differs from config when port is 0)
	activeConns   atomic.Int32  // Tracks in-flight HTTP requests
	lastActivity  atomic.Int64  // Unix nanos of last accepted request start
}

// NewServer creates a new proxy server.
func NewServer(cfg *ServerConfig) (*Server, error) {
	if cfg == nil {
		cfg = Defaults()
	}
	if cfg.MaxRequestSize == 0 {
		cfg.MaxRequestSize = Defaults().MaxRequestSize
	}

	handler := NewHandler(cfg.MaxRequestSize)

	return &Server{
		config:  cfg,
		handler: handler,
	}, nil
}

// SetRouter sets the router for the handler.
func (s *Server) SetRouter(router Router) {
	s.handler.SetRouter(router)
}

// SetTransformerRegistry sets the transformer registry.
func (s *Server) SetTransformerRegistry(reg TransformerRegistry) {
	s.handler.SetTransformerRegistry(reg)
}

// SetProviderClients sets the provider clients.
func (s *Server) SetProviderClients(clients map[string]HTTPClient) {
	s.handler.SetProviderClients(clients)
}

// SetStreamingClients sets the provider clients for streaming requests.
// These clients have no timeout and are optimized for SSE streaming.
func (s *Server) SetStreamingClients(clients map[string]HTTPClient) {
	s.handler.SetStreamingClients(clients)
}

// SetConfig sets the configuration.
func (s *Server) SetConfig(cfg *config.Config) {
	s.handler.SetConfig(cfg)
}

// SetUsageTracker sets the usage tracker.
func (s *Server) SetUsageTracker(tracker UsageTracker) {
	s.usageTracker = tracker
	s.handler.SetUsageTracker(tracker)
}

// SetInstanceID sets the instance ID.
func (s *Server) SetInstanceID(id string) {
	s.instanceID = id
	s.handler.SetInstanceID(id)
}

// SetRequestInterceptors sets the request interceptors.
func (s *Server) SetRequestInterceptors(interceptors []RequestInterceptor) {
	s.handler.SetRequestInterceptors(interceptors)
}

// SetResponseInterceptors sets the response interceptors.
func (s *Server) SetResponseInterceptors(interceptors []ResponseInterceptor) {
	s.handler.SetResponseInterceptors(interceptors)
}

// SetStreamingInterceptors sets the streaming interceptors.
func (s *Server) SetStreamingInterceptors(interceptors []StreamingResponseInterceptor) {
	s.handler.SetStreamingInterceptors(interceptors)
}

// SetAdminToken sets the admin API token for runtime management.
func (s *Server) SetAdminToken(token string) {
	s.handler.SetAdminToken(token)
}

// SetMultiUserEnabled enables or disables multi-user authentication.
func (s *Server) SetMultiUserEnabled(enabled bool) {
	s.handler.SetMultiUserEnabled(enabled)
}

// SetAuthInterceptor sets the auth interceptor for multi-user mode.
func (s *Server) SetAuthInterceptor(ai *AuthInterceptor) {
	s.handler.SetAuthInterceptor(ai)
}

// SetQoSEngine sets the QoS engine for multi-user mode.
func (s *Server) SetQoSEngine(engine *qos.QoSEngine) {
	s.handler.qosEngine = engine
}

// SetActiveProfile sets the initial active profile for the handler and router.
func (s *Server) SetActiveProfile(profile string) {
	s.handler.SetActiveProfile(profile)
	// Also set it on the router if it's already set
	if s.handler.router != nil {
		s.handler.router.SetActiveProfile(profile)
	}
}

// GetAdminToken returns the admin API token.
func (s *Server) GetAdminToken() string {
	return s.handler.GetAdminToken()
}

// AddRequestInterceptor adds a single request interceptor.
func (s *Server) AddRequestInterceptor(interceptor RequestInterceptor) {
	s.handler.AddRequestInterceptor(interceptor)
}

// AddResponseInterceptor adds a single response interceptor.
func (s *Server) AddResponseInterceptor(interceptor ResponseInterceptor) {
	s.handler.AddResponseInterceptor(interceptor)
}

// AddStreamingInterceptor adds a single streaming interceptor.
func (s *Server) AddStreamingInterceptor(interceptor StreamingResponseInterceptor) {
	s.handler.AddStreamingInterceptor(interceptor)
}

// Start starts the server and waits until it's ready to accept connections.
func (s *Server) Start() error {
	s.mu.Lock()

	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	// Create error logger that writes to our configured destination
	// Use standard log package for http.Server.ErrorLog compatibility
	errorLogWriter := logging.GetWriter()
	if errorLogWriter == nil {
		errorLogWriter = io.Discard
	}

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s, // Use Server as handler to track active connections
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long for streaming
		ErrorLog:     log.New(errorLogWriter, "", 0), // No prefix, uses our logging
	}

	// Configure TLS up-front so validation errors fail fast before binding.
	tlsMode := s.config.TLS.Mode()
	var autocrtpManager *autocert.Manager
	switch tlsMode {
	case "manual":
		if _, err := os.Stat(s.config.TLS.CertFile); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("tls-cert file %q: %w", s.config.TLS.CertFile, err)
		}
		if _, err := os.Stat(s.config.TLS.KeyFile); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("tls-key file %q: %w", s.config.TLS.KeyFile, err)
		}
	case "autocert":
		cacheDir, err := autocertCacheDir()
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("autocert cache dir: %w", err)
		}
		autocrtpManager = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.config.TLS.Domain),
			Cache:      autocert.DirCache(cacheDir),
		}
		s.server.TLSConfig = autocrtpManager.TLSConfig()
	}

	// Create readiness channel before starting
	ready := make(chan struct{})
	s.ready = ready

	s.running = true
	s.mu.Unlock()

	// Create listener explicitly to know when we're ready to accept connections
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Lock()
		s.running = false
		s.ready = nil
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// For autocert we wrap the listener with TLS ourselves (no cert files to
	// hand to ServeTLS). For manual mode we pass the raw listener to ServeTLS
	// and let it load the cert pair. Plaintext is just Serve.
	var serveListener net.Listener = listener
	if tlsMode == "autocert" {
		serveListener = tls.NewListener(listener, s.server.TLSConfig)
	}

	// Start the :80 redirect / ACME http-01 listener when needed. Bound before
	// the readiness signal so a port-80 collision fails startup cleanly.
	needRedirect := tlsMode != "" && (s.config.TLS.Redirect || tlsMode == "autocert")
	if needRedirect {
		if err := s.startRedirectServer(autocrtpManager, errorLogWriter); err != nil {
			// Cleanup the primary listener we already bound.
			listener.Close()
			s.mu.Lock()
			s.running = false
			s.ready = nil
			s.mu.Unlock()
			return err
		}
	}

	// Store the actual bound address (differs from config when port is 0)
	s.actualAddr = listener.Addr().String()
	// Initialize lastActivity to boot time so the idle watcher doesn't see a
	// zero value (1970) and immediately trip on its first tick.
	s.lastActivity.Store(time.Now().UnixNano())

	// Launch server in goroutine
	go func() {
		// Signal readiness immediately - listener is already accepting connections
		close(ready)
		var serveErr error
		switch tlsMode {
		case "manual":
			// ServeTLS loads the cert pair and wraps the raw listener itself.
			serveErr = s.server.ServeTLS(listener, s.config.TLS.CertFile, s.config.TLS.KeyFile)
		case "autocert":
			// Listener already wrapped with tls.NewListener above.
			serveErr = s.server.Serve(serveListener)
		default:
			// Plaintext.
			serveErr = s.server.Serve(listener)
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			logging.Errorf("HTTP server exited: %v", serveErr)
		}
	}()

	return nil
}

// startRedirectServer binds :80 and serves HTTP→HTTPS redirects. In autocert
// mode it also serves the ACME http-01 challenge via manager.HTTPHandler.
// The caller is responsible for tearing down via Stop() which closes
// redirectServer.
func (s *Server) startRedirectServer(manager *autocert.Manager, errorLogWriter io.Writer) error {
	var handler http.Handler
	if manager != nil {
		handler = manager.HTTPHandler(http.HandlerFunc(httpsRedirectHandler))
	} else {
		handler = http.HandlerFunc(httpsRedirectHandler)
	}
	s.redirectServer = &http.Server{
		Addr:         ":80",
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorLog:     log.New(errorLogWriter, "", 0),
	}
	ln, err := net.Listen("tcp", ":80")
	if err != nil {
		return fmt.Errorf("failed to listen on :80 for redirect: %w", err)
	}
	go func() {
		if err := s.redirectServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			logging.Errorf("HTTP redirect server on :80 exited: %v", err)
		}
	}()
	logging.Infof("TLS: HTTP→HTTPS redirect listening on :80")
	return nil
}

// httpsRedirectHandler emits a 301 to the HTTPS URL equivalent of the request.
// The scheme is forced to https; host/path/query are preserved from the inbound
// request.
func httpsRedirectHandler(w http.ResponseWriter, r *http.Request) {
	target := "https://" + stripPort(r.Host) + r.URL.RequestURI()
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// stripPort returns the host portion of a host[:port] string. The redirect
// targets the canonical HTTPS port (443) so we drop any explicit :port from
// the inbound Host header.
func stripPort(hostport string) string {
	if i := strings.LastIndex(hostport, ":"); i >= 0 {
		// Guard against stripping from an IPv6 literal without a port. net.SplitHostPort
		// is the right tool here, but a naive strip suffices because we always
		// rebuild the URL with the default HTTPS port via http.Redirect.
		return hostport[:i]
	}
	return hostport
}

// autocertCacheDir resolves the directory autocert uses to persist issued
// Let's Encrypt certificates across restarts. Created with mode 0700 if
// missing.
//
// Resolution order (first non-empty wins):
//  1. CCROUTER_AUTOCERT_CACHE_DIR — explicit operator override.
//  2. $CCROUTER_DATA_DIR/letsencrypt — the configured data dir, set by
//     the start command from config. This is the path used when running
//     under systemd where $HOME resolves to the service user's home
//     (e.g. /var/lib/ccrouter), which is outside the unit's
//     ReadWritePaths and would otherwise be blocked by ProtectSystem.
//  3. ~/.cc-modelrouter/letsencrypt — the dev/local fallback, used when
//     neither env var is set (e.g. `go run`, manual binary invocation).
//
// Migration: if the resolved path doesn't exist but the legacy
// ~/.cc-modelrouter/letsencrypt does, rename it across. Avoids re-hitting
// ACME rate limits when upgrading an install that used the old path.
func autocertCacheDir() (string, error) {
	if override := os.Getenv("CCROUTER_AUTOCERT_CACHE_DIR"); override != "" {
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", fmt.Errorf("autocert cache dir %s: %w", override, err)
		}
		return override, nil
	}

	dataDir := os.Getenv("CCROUTER_DATA_DIR")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataDir = filepath.Join(home, ".cc-modelrouter")
	}
	dir := filepath.Join(dataDir, "letsencrypt")

	// Migrate legacy path if present. Cross-device renames fail with
	// EXDEV; on that case fall through to fresh creation rather than
	// abandoning the cache.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		home, _ := os.UserHomeDir()
		if home != "" {
			legacy := filepath.Join(home, ".cc-modelrouter", "letsencrypt")
			if legacy != dir {
				if _, lerr := os.Stat(legacy); lerr == nil {
					if rerr := os.Rename(legacy, dir); rerr != nil {
						logging.Warnf("[PROXY] autocert cache migration %s → %s failed: %v (will create fresh)", legacy, dir, rerr)
					}
				}
			}
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Stop stops the server.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	// Shutdown tracker if it exists
	if shutdowner, ok := s.usageTracker.(interface{ Shutdown() }); ok {
		shutdowner.Shutdown()
	}

	var redirectErr error
	if s.redirectServer != nil {
		redirectErr = s.redirectServer.Shutdown(ctx)
	}

	err := s.server.Shutdown(ctx)
	s.running = false
	s.ready = nil // Clean up readiness channel
	if err == nil {
		return redirectErr
	}
	return err
}

// Addr returns the configured server address.
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
}

// ActualAddr returns the actual bound address after Start().
// When port 0 is used, this returns the OS-assigned port.
func (s *Server) ActualAddr() string {
	return s.actualAddr
}

// IsRunning returns true if the server is running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// ActiveConnections returns the number of currently in-flight HTTP requests.
func (s *Server) ActiveConnections() int32 {
	return s.activeConns.Load()
}

// LastActivity returns the time the most recent HTTP request was accepted.
// Safe to call concurrently with ServeHTTP.
func (s *Server) LastActivity() time.Time {
	return time.Unix(0, s.lastActivity.Load())
}

// ServeHTTP wraps the handler with connection tracking for the grace period logic.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.activeConns.Add(1)
	s.lastActivity.Store(time.Now().UnixNano())
	defer s.activeConns.Add(-1)
	s.handler.ServeHTTP(w, r)
}
