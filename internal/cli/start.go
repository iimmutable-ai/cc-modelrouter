package cli

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/auth"
	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
	"github.com/iimmutable-ai/cc-modelrouter/internal/qos"
	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
	"github.com/iimmutable-ai/cc-modelrouter/internal/provider"
	"github.com/iimmutable-ai/cc-modelrouter/internal/proxy"
	"github.com/iimmutable-ai/cc-modelrouter/internal/restartlog"
	"github.com/iimmutable-ai/cc-modelrouter/internal/router"
	"github.com/iimmutable-ai/cc-modelrouter/internal/transformer"
	transformers "github.com/iimmutable-ai/cc-modelrouter/internal/transformer/transformers"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
	"github.com/iimmutable-ai/cc-modelrouter/internal/useragent"
	"github.com/iimmutable-ai/cc-modelrouter/internal/version"
	"github.com/spf13/cobra"
)

// NewStartCommand creates the start command.
func NewStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the router server",
		Long: `Starts the router server in standalone mode.

The router acts as a proxy between Claude Code and LLM providers. It routes requests
based on configured rules and transforms requests/responses for provider compatibility.

Flags:
  -c, --config <path>   Path to custom configuration file.
                        If not specified, searches for config in:
                        - <project>/.cc-modelrouter/config.json (project)
                        - ~/.cc-modelrouter/config.json (global)

  -p, --port <number>   Port number for the router to listen on.
                        Overrides the port specified in config file.
                        Default: 8081 (or value from config)

  -H, --host <address>  Host address to bind to.
                        Overrides the host specified in config file.
                        Default: localhost (or value from config)

Examples:
  # Start with default configuration
  ccrouter start

  # Start with custom config file
  ccrouter start --config /path/to/config.json

  # Start on specific port
  ccrouter start --port 9090

  # Start on specific host and port
  ccrouter start --host 0.0.0.0 --port 8081

After starting, set ANTHROPIC_BASE_URL to point to the router:
  export ANTHROPIC_BASE_URL=http://localhost:8081

Flags:
  --log-destination <dest>  Log destination: "file", "stdout", "stderr", or a file path.
                           Overrides config file setting.`,
		RunE: runStart,
	}

	cmd.Flags().StringP("config", "c", "", "Path to config file")
	cmd.Flags().IntP("port", "p", 0, "Port to listen on (overrides config)")
	cmd.Flags().StringP("host", "H", "", "Host to bind to (overrides config)")
	cmd.Flags().String("log-destination", "", "Log destination (file|stdout|stderr|path)")
	cmd.Flags().String("log-level", "", "Log level: debug, info, warn, error (default: from config)")
	cmd.Flags().String("profile", "", "Specify which route profile to use at startup")
	cmd.Flags().String("auto-restart-idle", "", "Auto-restart after idle duration (e.g. 30m, 2h); empty disables")
	cmd.Flags().String("auto-restart-window", "", "Restrict restart to time window HH:MM-HH:MM (tz from --auto-restart-timezone)")
	cmd.Flags().String("auto-restart-timezone", "", "IANA timezone for --auto-restart-window (e.g. Asia/Shanghai); empty = Local")
	cmd.Flags().String("auto-restart-backoff-max", "", "Random backoff cap before restart (e.g. 10m); empty = none")
	cmd.Flags().String("tls-cert", "", "Path to TLS cert file (enables HTTPS, manual cert mode)")
	cmd.Flags().String("tls-key", "", "Path to TLS private key file (requires --tls-cert)")
	cmd.Flags().String("tls-domain", "", "FQDN for automatic Let's Encrypt cert (enables HTTPS, autocert mode)")
	cmd.Flags().Bool("tls-redirect", false, "Listen on :80 and redirect HTTP to HTTPS (forced on with --tls-domain)")

	return cmd
}

func runStart(cmd *cobra.Command, args []string) error {
	// Get flags
	configPath, _ := cmd.Flags().GetString("config")
	port, _ := cmd.Flags().GetInt("port")
	host, _ := cmd.Flags().GetString("host")
	logDestination, _ := cmd.Flags().GetString("log-destination")
	profileFlag, _ := cmd.Flags().GetString("profile")
	autoRestartIdle, _ := cmd.Flags().GetString("auto-restart-idle")
	autoRestartWindow, _ := cmd.Flags().GetString("auto-restart-window")
	autoRestartTimezone, _ := cmd.Flags().GetString("auto-restart-timezone")
	autoRestartBackoffMax, _ := cmd.Flags().GetString("auto-restart-backoff-max")
	tlsCert, _ := cmd.Flags().GetString("tls-cert")
	tlsKey, _ := cmd.Flags().GetString("tls-key")
	tlsDomain, _ := cmd.Flags().GetString("tls-domain")
	tlsRedirect, _ := cmd.Flags().GetBool("tls-redirect")

	// Get working directory
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Load configuration
	var cfg *config.Config
	var configType string
	if configPath != "" {
		cfg, err = config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		configType = "custom"
	} else {
		cfg, configType, err = config.LoadWithOverride(projectRoot)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Publish the data directory via env so downstream code (autocert
	// cache, future features) resolves to the operator's data dir rather
	// than the service user's $HOME. Under a system-scope systemd unit,
	// $HOME resolves to /var/lib/ccrouter — outside ReadWritePaths and
	// blocked by ProtectSystem=strict.
	if dir := resolveDataDir(); dir != "" {
		_ = os.Setenv("CCROUTER_DATA_DIR", dir)
	}

	// Validate and set profile if specified
	var profileName string
	if profileFlag != "" {
		if !cfg.HasProfiles() {
			return fmt.Errorf("no profiles configured in config, cannot use profile flag")
		}

		if _, ok := cfg.Router.Profiles[profileFlag]; !ok {
			availableProfiles := cfg.GetProfileNames()
			return fmt.Errorf("invalid profile '%s', available profiles: %v", profileFlag, availableProfiles)
		}

		profileName = profileFlag
		fmt.Printf("Using profile: %s\n", profileName)
	} else {
		profileName = cfg.GetDefaultProfile()
	}

	// Apply flag overrides
	if port > 0 {
		cfg.Server.Port = port
	}
	if host != "" {
		cfg.Server.Host = host
	}
	if logDestination != "" {
		cfg.Logging.Destination = logDestination
		cfg.Logging.Enabled = true // CLI flag implicitly enables logging
	}

	// Apply log level override
	logLevel, _ := cmd.Flags().GetString("log-level")
	if logLevel != "" {
		cfg.Logging.Level = logLevel
		cfg.Logging.Enabled = true // CLI flag implicitly enables logging
	}

	// Apply auto-restart overrides (validate parse up front so bad flags fail fast)
	if autoRestartIdle != "" {
		if _, err := time.ParseDuration(autoRestartIdle); err != nil {
			return fmt.Errorf("invalid --auto-restart-idle %q: %w", autoRestartIdle, err)
		}
		cfg.Server.AutoRestartIdle = autoRestartIdle
	}
	if autoRestartWindow != "" {
		parts := strings.Split(autoRestartWindow, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid --auto-restart-window %q: expected HH:MM-HH:MM", autoRestartWindow)
		}
		if _, err := time.Parse("15:04", strings.TrimSpace(parts[0])); err != nil {
			return fmt.Errorf("invalid --auto-restart-window start %q: %w", parts[0], err)
		}
		if _, err := time.Parse("15:04", strings.TrimSpace(parts[1])); err != nil {
			return fmt.Errorf("invalid --auto-restart-window end %q: %w", parts[1], err)
		}
		cfg.Server.AutoRestartWindow = autoRestartWindow
	}
	if autoRestartTimezone != "" {
		if _, err := time.LoadLocation(autoRestartTimezone); err != nil {
			return fmt.Errorf("invalid --auto-restart-timezone %q: %w", autoRestartTimezone, err)
		}
		cfg.Server.AutoRestartTimezone = autoRestartTimezone
	}
	if autoRestartBackoffMax != "" {
		if _, err := time.ParseDuration(autoRestartBackoffMax); err != nil {
			return fmt.Errorf("invalid --auto-restart-backoff-max %q: %w", autoRestartBackoffMax, err)
		}
		cfg.Server.AutoRestartBackoffMax = autoRestartBackoffMax
	}

	// Apply TLS overrides (manual cert files OR Let's Encrypt autocert).
	// CLI flags win over config-file `server.tls.*`. Validated up-front so
	// bad combinations (cert without key, mutually-exclusive modes, missing
	// files) fail fast before binding any port.
	if err := applyTLSOverrides(cmd, cfg, tlsCert, tlsKey, tlsDomain, tlsRedirect); err != nil {
		return err
	}

	// Generate instance ID and address early (needed for logging)
	instanceID := daemon.GenerateInstanceID()
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	// If we were spawned by an auto-restart (CCRROUTER_RESTART_FROM carries the
	// old instance ID across syscall.Exec), record the handoff. Unset so a
	// future manual restart doesn't re-trigger the record.
	if from := os.Getenv("CCRROUTER_RESTART_FROM"); from != "" {
		if err := restartlog.Append(restartlog.Record{
			TS:       time.Now(),
			Event:    restartlog.EventRestarted,
			From:     from,
			Instance: instanceID,
			PID:      os.Getpid(),
		}); err != nil {
			logging.Warnf("Auto-restart: failed to write restarted record: %v", err)
		} else {
			logging.Infof("Auto-restart: restarted from %s into new instance %s", from, instanceID)
		}
		os.Unsetenv("CCRROUTER_RESTART_FROM")
	}
	// Set per-instance log file path if logging to file
	if cfg.Logging.ShouldLogToFile() && cfg.Logging.FilePath == "" {
		logPath, err := cfg.Logging.GetLogPathWithInstance(instanceID)
		if err == nil {
			cfg.Logging.FilePath = logPath
		}
	}

	// Initialize logging based on configuration
	logCleanup, err := logging.Init(&cfg.Logging)
	if err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}
	defer logCleanup()

	// Verify logging is working by writing a test message
	logging.Infof("Logging initialized - router starting on %s", addr)

	// Log startup
	fmt.Printf("Starting ccrouter %s on %s\n", version.String(), addr)
	if cfg.Logging.IsEnabled() {
		if cfg.Logging.Destination == "file" || cfg.Logging.Destination == "" {
			if logPath, logErr := cfg.Logging.GetLogPath(); logErr == nil {
				fmt.Printf("Logging to: %s\n", logPath)
			}
		} else if cfg.Logging.Destination == "stdout" {
			fmt.Printf("Logging to: stdout\n")
		} else if cfg.Logging.Destination == "stderr" {
			fmt.Printf("Logging to: stderr\n")
		} else {
			// Custom path
			fmt.Printf("Logging to: %s\n", cfg.Logging.Destination)
		}
	} else {
		fmt.Printf("Logging: disabled\n")
	}

	// Create server
	serverCfg := &proxy.ServerConfig{
		Host:           cfg.Server.Host,
		Port:           cfg.Server.Port,
		MaxRequestSize: 50 * 1024 * 1024, // 50MB — matches proxy.Defaults
		TLS:            cfg.Server.TLS,
	}
	server, err := proxy.NewServer(serverCfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Setup router engine
	routerEngine := router.NewEngine(cfg)
	routerEngine.SetActiveProfile(profileName)
	server.SetRouter(NewRouterAdapter(routerEngine))

	// Setup transformer registry
	registry := transformer.NewRegistry()
	// New transformers (Anthropic-centric interface). User-Agent is resolved
	// once from cfg.Server.UserAgent (defaults to the Claude Code SDK UA).
	resolvedUA := useragent.Resolve(cfg.Server.UserAgent)
	for _, tf := range []interface {
		transformer.Transformer
		SetUserAgent(string)
	}{
		transformers.NewAnthropicTransformer(),
		transformers.NewGLMAnthropicTransformer(),
		transformers.NewOpenRouterTransformer(),
		transformers.NewOpenAITransformer(),
		transformers.NewGeminiTransformer(),
	} {
		tf.SetUserAgent(resolvedUA)
		registry.Register(tf)
	}
	// Note: Qwen and MiniMax now use the Anthropic transformer since they are Anthropic-compatible
	// GLM providers (aliyun, bigmodel) use the GLM-specific transformer which ensures signature field handling
	// OpenRouter providers use the OpenRouter-specific transformer which preserves signature fields
	server.SetTransformerRegistry(NewRegistryAdapter(registry))

	// Setup provider clients
	clients := make(map[string]proxy.HTTPClient)
	for name, providerCfg := range cfg.Providers {
		// Validate API key is not empty or unset
		if providerCfg.APIKey == "" {
			return fmt.Errorf("provider %s: API key is empty (check environment variable)", name)
		}
		if strings.HasPrefix(providerCfg.APIKey, "${") {
			return fmt.Errorf("provider %s: API key environment variable not set: %s", name, providerCfg.APIKey)
		}

		client, err := provider.NewClient(&provider.ClientConfig{
			BaseURL:           providerCfg.BaseURL,
			APIKey:            providerCfg.APIKey,
			MaxRetries:        cfg.Router.MaxRetries,
			RetryDelay:        cfg.Router.GetRetryDelay(),
			DisableKeepAlives: providerCfg.DisableKeepAlives,
		})
		if err != nil {
			return fmt.Errorf("failed to create client for %s: %w", name, err)
		}
		clients[name] = client
	}
	server.SetProviderClients(clients)

	// Create streaming clients (no timeout for long-running SSE streams)
	streamingClients := make(map[string]proxy.HTTPClient)
	for name, providerCfg := range cfg.Providers {
		streamingClient, err := provider.NewStreamingClient(&provider.ClientConfig{
			BaseURL:           providerCfg.BaseURL,
			APIKey:            providerCfg.APIKey,
			MaxRetries:        cfg.Router.MaxRetries,
			RetryDelay:        cfg.Router.GetRetryDelay(),
			DisableKeepAlives: providerCfg.DisableKeepAlives,
		})
		if err != nil {
			return fmt.Errorf("failed to create streaming client for %s: %w", name, err)
		}
		streamingClients[name] = streamingClient
	}
	server.SetStreamingClients(streamingClients)

	server.SetConfig(cfg)

	// Initialize usage tracker
	dbPath, err := usage.DBPath()
	if err != nil {
		return fmt.Errorf("failed to get db path: %w", err)
	}

	usageDB, err := usage.InitDB(dbPath)
	if err != nil {
		return fmt.Errorf("failed to init usage db: %w", err)
	}

	tracker := usage.NewTracker(usageDB, usage.DefaultBufferSize, usage.DefaultFlushTimeout)
	server.SetUsageTracker(tracker)
	server.SetInstanceID(instanceID)

	// Generate admin token for runtime profile management
	adminToken := daemon.GenerateAdminToken()
	server.SetAdminToken(adminToken)

	// Multi-user mode: read settings from SQLite
	keyStore := auth.NewKeyStore(usageDB)
	settings, err := keyStore.GetSettings()
	if err != nil {
		return fmt.Errorf("failed to read multi-user settings: %w", err)
	}

	if settings.Enabled {
		ai := proxy.NewAuthInterceptor(keyStore)
		server.SetAuthInterceptor(ai)
		server.SetMultiUserEnabled(true)

		// Build QoS engine from SQLite groups
		groups, err := keyStore.ListGroups()
		if err != nil {
			return fmt.Errorf("failed to load groups for QoS engine: %w", err)
		}
		qosGroupCfgs := make([]qos.GroupConfig, len(groups))
		for i, g := range groups {
			qosGroupCfgs[i] = qos.GroupConfig{
				Name:           g.Name,
				PriorityWeight:  g.PriorityWeight,
				MaxConcurrency: g.MaxConcurrency,
			}
		}
		wredCfg := qos.WREDConfig{
			MinDepth: settings.WREDMinDepth,
			MaxDepth: settings.WREDMaxDepth,
		}
		qosEngine := qos.NewQoSEngine(settings.GlobalMaxConc, wredCfg, qosGroupCfgs)
		server.SetQoSEngine(qosEngine)

		fmt.Println("Multi-user mode enabled: API key authentication required")
	}

	// One-time migration: if legacy config has multi-user enabled, migrate to SQLite
	if cfg.MultiUser.Enabled {
		tx, err := usageDB.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin migration tx: %w", err)
		}
		defer tx.Rollback()

		migrated := &auth.MultiUserSettings{
			Enabled:       true,
			GlobalMaxConc: cfg.MultiUser.GlobalMaxConc,
			WREDMinDepth:  cfg.MultiUser.WREDMinDepth,
			WREDMaxDepth:  cfg.MultiUser.WREDMaxDepth,
		}
		if migrated.WREDMinDepth == 0 {
			migrated.WREDMinDepth = 0.5
		}
		if migrated.WREDMaxDepth == 0 {
			migrated.WREDMaxDepth = 0.9
		}
		if err := keyStore.UpdateSettings(migrated); err != nil {
			return fmt.Errorf("failed to migrate multi-user settings: %w", err)
		}
		// Seed groups from config if DB is empty
		existingGroups, err := keyStore.ListGroups()
		if err != nil {
			return fmt.Errorf("failed to list groups during migration: %w", err)
		}
		if len(existingGroups) == 0 {
			for _, gc := range cfg.MultiUser.Groups {
				if _, err := keyStore.CreateGroup(gc.Name, gc.Profile, gc.PriorityWeight, gc.MaxConcurrency); err != nil {
					return fmt.Errorf("failed to migrate group %s: %w", gc.Name, err)
				}
			}
		}
		// Clear legacy config field so it's not migrated again
		cfg.MultiUser.Enabled = false
		cfg.MultiUser.Groups = nil
		if err := config.Save(cfg, configPath); err != nil {
			return fmt.Errorf("failed to clear legacy config: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration: %w", err)
		}
	}

	// Initialize handler's active profile
	server.SetActiveProfile(profileName)

	// Add logging interceptor
	loggingInterceptor := proxy.NewLoggingInterceptor()
	server.AddRequestInterceptor(loggingInterceptor)
	server.AddResponseInterceptor(loggingInterceptor)

	// Start server
	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	// Get actual bound address (important when port is 0 — OS assigns a free port)
	actualAddr := server.ActualAddr()
	_, actualPort, _ := net.SplitHostPort(actualAddr)

	// Save instance metadata with admin token and active profile
	meta := &daemon.InstanceMetadata{
		ID:           instanceID,
		Port:         cfg.Server.Port,
		PID:          os.Getpid(),
		ConfigType:   configType,
		ConfigPath:   configPath,
		ProjectRoot:  projectRoot,
		StartTime:    time.Now(),
		AdminToken:   adminToken,
		ActiveProfile: profileName,
	}
	if actualPort != "" {
		fmt.Sscanf(actualPort, "%d", &meta.Port)
	}
	if err := daemon.SaveInstance(meta); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save instance metadata: %v\n", err)
	}

	fmt.Printf("Router started with instance ID: %s\n", instanceID)
	fmt.Printf("Set these environment variables to use the router:\n")
	scheme := "http"
	displayHost := actualAddr
	if cfg.Server.TLS.Enabled() {
		scheme = "https"
		// In autocert mode the canonical client-facing hostname is the domain,
		// not the bind address (which may be 0.0.0.0). Prefer the domain in the
		// printed URL so users get a URL that actually resolves.
		if cfg.Server.TLS.Domain != "" {
			if _, portStr, splitErr := net.SplitHostPort(actualAddr); splitErr == nil {
				displayHost = net.JoinHostPort(cfg.Server.TLS.Domain, portStr)
			}
		}
	}
	fmt.Printf("  export ANTHROPIC_BASE_URL=%s://%s\n", scheme, displayHost)
	if settings.Enabled {
		fmt.Printf("  export ANTHROPIC_API_KEY=<your-sk-ccr-key>\n")
	}

	// Start idle-restart watcher if enabled. The watcher owns the full restart
	// sequence (Stop → DeleteInstance → syscall.Exec) in its own goroutine.
	// done is closed on SIGINT/SIGTERM so the watcher exits cleanly on normal shutdown.
	idleDone := make(chan struct{})
	if idle := cfg.Server.GetAutoRestartIdle(); idle > 0 {
		wStart, wEnd, wEnabled := cfg.Server.GetAutoRestartWindow()
		opts := idleWatchOpts{
			idle:        idle,
			windowStart: wStart,
			windowEnd:   wEnd,
			windowOn:    wEnabled,
			tz:          cfg.Server.GetAutoRestartTimezone(),
			backoffMax:  cfg.Server.GetAutoRestartBackoffMax(),
		}
		go runIdleWatcher(server, instanceID, opts, idleDone)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan
	fmt.Printf("\nShutting down router...\n")

	// Stop the idle watcher first so it can't fire a restart mid-shutdown.
	close(idleDone)

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Stop(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping server: %v\n", err)
	}

	// Cleanup instance file
	daemon.DeleteInstance(instanceID)

	fmt.Println("Router stopped")
	return nil
}

// idleWatchOpts configures the auto-restart watcher.
type idleWatchOpts struct {
	idle        time.Duration
	windowStart time.Duration
	windowEnd   time.Duration
	windowOn    bool
	tz          *time.Location
	backoffMax  time.Duration
}

// runIdleWatcher runs the auto-restart state machine. It owns the entire restart
// sequence atomically (does not signal the main goroutine, which avoids a TOCTOU
// window between checking idle and exec'ing). Exits when done is closed.
func runIdleWatcher(server *proxy.Server, instanceID string, opts idleWatchOpts, done <-chan struct{}) {
	deadline := time.NewTimer(opts.idle)
	defer deadline.Stop()
	tickInterval := opts.idle / 4
	if tickInterval > 10*time.Second {
		tickInterval = 10 * time.Second
	}
	if tickInterval < time.Second {
		tickInterval = time.Second
	}
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-deadline.C:
			// Idle duration elapsed. Check preconditions before restarting.
			if server.ActiveConnections() != 0 {
				deadline.Reset(remainingOrInterval(opts.idle, server.LastActivity(), tickInterval))
				continue
			}
			if opts.windowOn && !inWindow(time.Now(), opts.windowStart, opts.windowEnd, opts.tz) {
				// Outside window: skip this cycle, re-arm for the next idle period.
				deadline.Reset(opts.idle)
				continue
			}
			// Preconditions met. Apply backoff if configured.
			if opts.backoffMax > 0 {
				jitter := time.Duration(rand.Int63n(int64(opts.backoffMax + 1)))
				logging.Infof("Auto-restart: idle reached, applying backoff %s before restart", jitter)
				backoff := time.NewTimer(jitter)
				cancelled := false
			backoffWait:
				for {
					select {
					case <-done:
						backoff.Stop()
						return
					case <-backoff.C:
						break backoffWait
					case <-ticker.C:
						// A new request during backoff cancels it; a routine tick does not.
						if time.Since(server.LastActivity()) < opts.idle {
							backoff.Stop()
							cancelled = true
							break backoffWait
						}
					}
				}
				if cancelled {
					deadline.Reset(remainingOrInterval(opts.idle, server.LastActivity(), tickInterval))
					continue
				}
				performIdleRestart(server, instanceID, opts, jitter, realExec)
				return // performIdleRestart never returns (Exec), but be safe
			} else {
				performIdleRestart(server, instanceID, opts, 0, realExec)
				return
			}
		case <-ticker.C:
			// Periodically re-arm deadline based on actual activity so a request
			// that arrives between deadline fires still resets the timer.
			remaining := remainingOrInterval(opts.idle, server.LastActivity(), tickInterval)
			deadline.Reset(remaining)
		}
	}
}

// remainingOrInterval returns the time left until idle elapses since lastActivity,
// clamped to a minimum of interval (so we never busy-loop on a stale timestamp).
func remainingOrInterval(idle time.Duration, lastActivity time.Time, interval time.Duration) time.Duration {
	remaining := idle - time.Since(lastActivity)
	if remaining < interval {
		return interval
	}
	return remaining
}

// inWindow reports whether now falls within [start, end) interpreted as time-of-day
// in tz. Supports overnight wrap (end < start, e.g. 23:00-04:00).
func inWindow(now time.Time, start, end time.Duration, tz *time.Location) bool {
	l := now.In(tz)
	tod := time.Duration(l.Hour())*time.Hour + time.Duration(l.Minute())*time.Minute
	if end >= start {
		return tod >= start && tod < end
	}
	return tod >= start || tod < end
}

// execFunc abstracts syscall.Exec so performIdleRestart is unit-testable.
type execFunc func() error

// realExec is the production exec: replaces the process image in-place.
var realExec execFunc = func() error {
	return syscall.Exec(os.Args[0], os.Args, os.Environ())
}

// performIdleRestart executes the restart sequence: record the outcome to the
// restart log, stop the listener (so the bound socket FD doesn't survive
// syscall.Exec and cause EADDRINUSE), delete the instance file, set the
// CCRROUTER_RESTART_FROM env var so the new process can detect it was spawned
// by a restart, then replace the process image in-place. PID is preserved.
//
// The initiated record is written before anything destructive; on Exec failure
// an exec_failed record is written before exit. On success Exec never returns,
// so the new process is responsible for writing the restarted record (see
// runStart's boot path).
func performIdleRestart(server *proxy.Server, instanceID string, opts idleWatchOpts, backoff time.Duration, execFn execFunc) {
	if err := restartlog.Append(restartlog.Record{
		TS:      time.Now(),
		Event:   restartlog.EventInitiated,
		From:    instanceID,
		Reason:  "idle",
		Idle:    opts.idle.String(),
		Backoff: backoff.String(),
		PID:     os.Getpid(),
	}); err != nil {
		logging.Warnf("Auto-restart: failed to write initiated record: %v", err)
	}

	logging.Infof("Auto-restart: idle timeout reached, restarting instance %s", instanceID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		logging.Warnf("Auto-restart: server.Stop error: %v", err)
	}
	if n := server.ActiveConnections(); n > 0 {
		logging.Warnf("Auto-restart: %d connections still active after Stop; proceeding", n)
	}
	daemon.DeleteInstance(instanceID)

	// Hand the old instance ID to the new process. os.Environ() at the Exec
	// call below carries this forward across the image replacement.
	os.Setenv("CCRROUTER_RESTART_FROM", instanceID)

	if err := execFn(); err != nil {
		if rerr := restartlog.Append(restartlog.Record{
			TS:    time.Now(),
			Event: restartlog.EventExecFailed,
			From:  instanceID,
			Error: err.Error(),
			PID:   os.Getpid(),
		}); rerr != nil {
			logging.Warnf("Auto-restart: failed to write exec_failed record: %v", rerr)
		}
		logging.Errorf("Auto-restart: syscall.Exec failed: %v — exiting", err)
		os.Exit(1)
	}
}

// applyTLSOverrides merges CLI TLS flags into cfg.Server.TLS, with CLI flags
// winning over config-file values. Validates mutual-exclusion and file
// existence up front so bad combos fail fast. Idempotent: if no CLI TLS flags
// are set and config has no TLS block, this is a no-op.
func applyTLSOverrides(cmd *cobra.Command, cfg *config.Config, tlsCert, tlsKey, tlsDomain string, tlsRedirect bool) error {
	anyTLSFlagSet := cmd.Flags().Changed("tls-cert") ||
		cmd.Flags().Changed("tls-key") ||
		cmd.Flags().Changed("tls-domain") ||
		cmd.Flags().Changed("tls-redirect")

	// Mutually exclusive: cert + domain.
	if tlsCert != "" && tlsDomain != "" {
		return fmt.Errorf("--tls-cert and --tls-domain are mutually exclusive (pick manual certs or autocert)")
	}
	// Cert and key must come together.
	if (tlsCert != "") != (tlsKey != "") {
		return fmt.Errorf("--tls-cert and --tls-key must be specified together")
	}

	// If any CLI TLS flag is set, build a fresh TLSConfig from the flags (CLI
	// wins over config). Otherwise, fall through to whatever the config file
	// already provided.
	var tlsCfg *config.TLSConfig
	if anyTLSFlagSet {
		tlsCfg = &config.TLSConfig{
			CertFile: tlsCert,
			KeyFile:  tlsKey,
			Domain:   tlsDomain,
			Redirect: tlsRedirect,
		}
	} else if cfg.Server.TLS != nil {
		tlsCfg = cfg.Server.TLS
	} else {
		return nil // No TLS anywhere — plaintext.
	}

	// Validate the resolved config.
	switch tlsCfg.Mode() {
	case "manual":
		if _, err := os.Stat(tlsCfg.CertFile); err != nil {
			return fmt.Errorf("--tls-cert file %q: %w", tlsCfg.CertFile, err)
		}
		if _, err := os.Stat(tlsCfg.KeyFile); err != nil {
			return fmt.Errorf("--tls-key file %q: %w", tlsCfg.KeyFile, err)
		}
	case "autocert":
		// autocert forces Redirect on (ACME http-01 challenge needs :80).
		tlsCfg.Redirect = true
	case "":
		// Redirect with no cert source is a no-op — warn and ignore.
		if tlsCfg.Redirect {
			fmt.Fprintln(os.Stderr, "Warning: --tls-redirect set without --tls-cert or --tls-domain; ignoring")
			tlsCfg.Redirect = false
		}
	}

	cfg.Server.TLS = tlsCfg

	// Warn if TLS is active but the bind address is localhost — clients on
	// other machines (the whole point of public-server HTTPS) cannot reach it.
	if tlsCfg.Enabled() && isLocalhost(cfg.Server.Host) {
		fmt.Fprintf(os.Stderr,
			"Warning: TLS enabled with host %q; bind a public interface (e.g. -H 0.0.0.0) for off-host clients\n",
			cfg.Server.Host)
	}
	return nil
}

// isLocalhost reports whether the host string binds to the loopback interface.
func isLocalhost(host string) bool {
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// resolveDataDir computes the value of CCROUTER_DATA_DIR that runStart
// publishes into the process env. Resolution order:
//
//  1. $CCROUTER_DATA_DIR — explicit operator override, returned verbatim.
//  2. $HOME if it already ends with `/.cc-modelrouter` — the system-scope
//     systemd unit sets `Environment=HOME=<DataDir>` where <DataDir> is
//     the operator's ~/.cc-modelrouter path. Treating that as the data
//     dir avoids the doubled path (~/.cc-modelrouter/.cc-modelrouter/...)
//     that would otherwise arise when we append the suffix.
//  3. $HOME + /.cc-modelrouter — the default for ad-hoc `ccrouter start`
//     invocations from a TTY where $HOME is the real user home.
//
// Returns "" if none of the above resolve.
func resolveDataDir() string {
	if v := os.Getenv("CCROUTER_DATA_DIR"); v != "" {
		return v
	}
	home, err := config.EffectiveHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if strings.HasSuffix(home, string(filepath.Separator)+".cc-modelrouter") {
		return home
	}
	return filepath.Join(home, ".cc-modelrouter")
}
