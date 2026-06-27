package monitor

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
)

// DateRange represents the selected time period
type DateRange int

const (
	DateRangeToday DateRange = iota
	DateRangeWeek
	DateRangeMonth
	DateRangeYTD
	DateRangeTTM
)

func (d DateRange) String() string {
	switch d {
	case DateRangeToday:
		return "TODAY"
	case DateRangeWeek:
		return "WEEK"
	case DateRangeMonth:
		return "MONTH"
	case DateRangeYTD:
		return "YTD"
	case DateRangeTTM:
		return "TTM"
	default:
		return "TODAY"
	}
}

// LogLevel represents the log severity level
type LogLevel int

const (
	LogLevelVerbs LogLevel = 1 << iota
	LogLevelTrace
	LogLevelDebug
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
)

// LogLevelSet is a bitmask of LogLevel values
type LogLevelSet int

// Flash animation constants
const (
	flashDuration        = 500 * time.Millisecond // total flash lifetime
	flashPhase1Duration  = 200 * time.Millisecond // bright highlight phase
	flashTickInterval    = 50 * time.Millisecond  // animation frame rate
)

// LogLine represents a parsed log line
type LogLine struct {
	Timestamp time.Time
	Level     LogLevel
	Message   string
	Raw       string
}

// InstanceInfo represents an instance for the selector
type InstanceInfo struct {
	ID        string
	IsRunning bool
	StartTime time.Time
	LogPath   string
}

// StatsUpdateMsg is sent when stats are updated
type StatsUpdateMsg struct {
	Stats *UsageStats
}

// LogMsg is sent when a log line is received
type LogMsg struct {
	Line string
}

// ErrorMsg is sent when an error occurs
type ErrorMsg struct {
	Error error
}

// RecoveryMsg is sent when the poller recovers from an error
type RecoveryMsg struct{}

// FlashTickMsg is sent during flash animation frames
type FlashTickMsg time.Time

// ContentTab represents the selected content tab in multi-user mode
type ContentTab int

const (
	ContentTabRoutes ContentTab = iota
	ContentTabUsers
	ContentTabGroups
)

// UsageStats represents aggregated usage data
type UsageStats struct {
	Summary     usage.Summary
	ByRoute     map[string]*usage.RouteStats
	ByModel     map[string]*usage.ModelStats
	ByUser      map[string]*usage.UserStats
	ByGroup     map[string]*usage.GroupStats
	IsMultiUser bool
	Timestamp   time.Time
}

// MonitorModel is the root Bubble Tea model
type MonitorModel struct {
	// Synchronization
	mu sync.RWMutex

	// Configuration
	RefreshInterval time.Duration
	DBPath          string

	// UI State
	SelectedDateRange DateRange
	SelectedInstance  string
	ConsoleLogEnabled bool
	ConsoleLogPaused  bool
	LogLevelFilters   LogLevelSet
	LastError         error

	// Data State
	Stats      *UsageStats
	Instances  []InstanceInfo
	LogBuffer  *LogBuffer

	// Multi-user tab state
	IsMultiUser        bool
	SelectedContentTab ContentTab

	// Flash animation state
	prevStats        *UsageStats
	flashCells       map[string]time.Time
	flashAnimating   bool

	// Channels (buffered)
	StatsChan chan *UsageStats
	LogChan   chan string
	ErrChan   chan error

	// Component state
	InstanceCursor int
	WindowSize     tea.WindowSizeMsg

	// Lifecycle
	Ctx    context.Context
	Cancel context.CancelFunc
	WG     sync.WaitGroup

	// Log tailer context (separate)
	LogCtx    context.Context
	LogCancel context.CancelFunc

	// Background workers
	StatsPoller *StatsPoller
	LogTailer   *LogTailer

	// Layout calculations (updated on window resize)
	LeftPanelWidth  int
	RightPanelWidth int
	RouteColWidth   int
	ModelColWidth   int

	// View cache (for efficiency)
	viewCache string
}

// NewMonitorModel creates a new monitor model
func NewMonitorModel(refreshInterval time.Duration, dbPath string) *MonitorModel {
	ctx, cancel := context.WithCancel(context.Background())

	m := &MonitorModel{
		RefreshInterval:   refreshInterval,
		DBPath:            dbPath,
		SelectedDateRange: DateRangeToday,
		SelectedInstance:  "",
		ConsoleLogEnabled: false,
		ConsoleLogPaused:  false,
		LogLevelFilters:   LogLevelSet(LogLevelDebug | LogLevelInfo | LogLevelWarn | LogLevelError | LogLevelFatal),
		Stats:             &UsageStats{},
		Instances:         []InstanceInfo{},
		LogBuffer:         NewLogBuffer(1000),
		flashCells:        make(map[string]time.Time),
		StatsChan:         make(chan *UsageStats, 10),
		LogChan:           make(chan string, 1000),
		ErrChan:           make(chan error, 100),
		InstanceCursor:    0,
		Ctx:               ctx,
		Cancel:            cancel,
	}

	// Initialize stats poller
	m.StatsPoller = &StatsPoller{
		Interval:   refreshInterval,
		DBPath:     dbPath,
		DateRange:  DateRangeToday,
		InstanceID: "",
	}

	return m
}

// Init starts the background workers
func (m *MonitorModel) Init() tea.Cmd {
	// Initialize layout widths (will be updated when window size is received)
	m.calculateLayoutWidths()

	// Start stats poller
	m.WG.Add(1)
	go func() {
		defer m.WG.Done()
		m.StatsPoller.Run(m.Ctx, m.StatsChan, m.ErrChan)
	}()

	// Initial instance discovery
	instances, err := discoverInstances(m.SelectedDateRange)
	if err == nil && len(instances) > 0 {
		m.Instances = instances
	}

	// Return commands to listen for channel messages
	return tea.Batch(
		m.waitForStats(),
		m.waitForLogs(),
		m.waitForErrors(),
	)
}

// waitForStats returns a command that waits for stats updates
func (m *MonitorModel) waitForStats() tea.Cmd {
	return func() tea.Msg {
		select {
		case stats := <-m.StatsChan:
			return StatsUpdateMsg{Stats: stats}
		case <-m.Ctx.Done():
			return nil
		}
	}
}

// waitForLogs returns a command that waits for log lines
func (m *MonitorModel) waitForLogs() tea.Cmd {
	return func() tea.Msg {
		select {
		case line := <-m.LogChan:
			return LogMsg{Line: line}
		case <-m.Ctx.Done():
			return nil
		}
	}
}

// waitForErrors returns a command that waits for errors
func (m *MonitorModel) waitForErrors() tea.Cmd {
	return func() tea.Msg {
		select {
		case err := <-m.ErrChan:
			if err == nil {
				return RecoveryMsg{}
			}
			return ErrorMsg{Error: err}
		case <-m.Ctx.Done():
			return nil
		}
	}
}

// Update handles messages
func (m *MonitorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.WindowSize = msg
		// Recalculate layout widths based on new window size
		m.calculateLayoutWidths()
		return m, nil

	case StatsUpdateMsg:
		m.Stats = msg.Stats
		m.LastError = nil
		// Sync multi-user mode and auto-select tab
		if msg.Stats.IsMultiUser && !m.IsMultiUser {
			m.IsMultiUser = true
			m.SelectedContentTab = ContentTabGroups
		} else if !msg.Stats.IsMultiUser && m.IsMultiUser {
			m.IsMultiUser = false
			m.SelectedContentTab = ContentTabRoutes
		}
		// Detect changes for flash animation
		m.detectChanges(m.prevStats, msg.Stats)
		m.prevStats = msg.Stats
		// Start flash tick if changes detected and not already animating
		if len(m.flashCells) > 0 && !m.flashAnimating {
			m.flashAnimating = true
			return m, tea.Batch(m.waitForStats(), m.waitForFlashTick())
		}
		// Refresh instances if date range changed
		if instances, err := discoverInstances(m.SelectedDateRange); err == nil {
			m.Instances = instances
		}
		return m, m.waitForStats()

	case FlashTickMsg:
		// Prune expired flash entries
		now := time.Now()
		for key, startTime := range m.flashCells {
			if now.Sub(startTime) >= flashDuration {
				delete(m.flashCells, key)
			}
		}
		if len(m.flashCells) > 0 {
			return m, m.waitForFlashTick()
		}
		m.flashAnimating = false
		return m, nil

	case LogMsg:
		// Parse and buffer the log line
		parsed := parseLogLine(msg.Line)
		m.LogBuffer.Append(parsed)
		return m, m.waitForLogs()

	case ErrorMsg:
		m.LastError = msg.Error
		// Continue listening
		return m, m.waitForErrors()

	case RecoveryMsg:
		m.LastError = nil
		return m, m.waitForErrors()
	}

	return m, nil
}

// handleKeyPress handles keyboard input
func (m *MonitorModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, m.shutdown()

	case "c":
		// Toggle console log - only if single instance selected
		if m.SelectedInstance != "" {
			m.ConsoleLogEnabled = !m.ConsoleLogEnabled
			// Recalculate layout when console is toggled
			m.calculateLayoutWidths()
			if m.ConsoleLogEnabled {
				m.startLogTailer()
			} else {
				m.stopLogTailer()
			}
		}

	case "left", "shift+tab":
		// Previous date range
		if m.SelectedDateRange > 0 {
			m.SelectedDateRange--
			m.StatsPoller.UpdateDateRange(m.SelectedDateRange)
			m.refreshStats()
			// Refresh instances
			if instances, err := discoverInstances(m.SelectedDateRange); err == nil {
				m.Instances = instances
			}
		}

	case "right", "tab":
		// Next date range
		if m.SelectedDateRange < DateRangeTTM {
			m.SelectedDateRange++
			m.StatsPoller.UpdateDateRange(m.SelectedDateRange)
			m.refreshStats()
			// Refresh instances
			if instances, err := discoverInstances(m.SelectedDateRange); err == nil {
				m.Instances = instances
			}
		}

	case "up", "k":
		// Previous instance
		if m.InstanceCursor > 0 {
			m.InstanceCursor--
			m.updateSelectedInstance()
		}

	case "down", "j":
		// Next instance
		if m.InstanceCursor < len(m.Instances) {
			m.InstanceCursor++
			m.updateSelectedInstance()
		}

	case " ":
		// Pause/resume log tail
		if m.ConsoleLogEnabled {
			m.ConsoleLogPaused = !m.ConsoleLogPaused
		}

	case "1":
		m.LogLevelFilters ^= LogLevelSet(LogLevelVerbs)
		m.updateLogFilters()
	case "2":
		m.LogLevelFilters ^= LogLevelSet(LogLevelTrace)
		m.updateLogFilters()
	case "3":
		m.LogLevelFilters ^= LogLevelSet(LogLevelDebug)
		m.updateLogFilters()
	case "4":
		m.LogLevelFilters ^= LogLevelSet(LogLevelInfo)
		m.updateLogFilters()
	case "5":
		m.LogLevelFilters ^= LogLevelSet(LogLevelWarn)
		m.updateLogFilters()
	case "6":
		m.LogLevelFilters ^= LogLevelSet(LogLevelError)
		m.updateLogFilters()
	case "7":
		m.LogLevelFilters ^= LogLevelSet(LogLevelFatal)
		m.updateLogFilters()

	case "r":
		// Force refresh
		m.refreshStats()

	case "g":
		if m.IsMultiUser {
			m.SelectedContentTab = ContentTabGroups
		}
	case "u":
		if m.IsMultiUser {
			m.SelectedContentTab = ContentTabUsers
		}
	case "o":
		if m.IsMultiUser {
			m.SelectedContentTab = ContentTabRoutes
		}
	}

	return m, nil
}

// updateSelectedInstance updates the selected instance based on cursor
func (m *MonitorModel) updateSelectedInstance() {
	oldInstance := m.SelectedInstance

	if m.InstanceCursor == 0 {
		m.SelectedInstance = ""
	} else if m.InstanceCursor-1 < len(m.Instances) {
		m.SelectedInstance = m.Instances[m.InstanceCursor-1].ID
	}

	// If instance changed and console log was enabled, disable it
	if oldInstance != m.SelectedInstance && m.ConsoleLogEnabled {
		m.stopLogTailer()
		m.ConsoleLogEnabled = false
		m.LogBuffer.Clear()
	}

	// Update stats poller
	m.StatsPoller.UpdateInstance(m.SelectedInstance)
	m.refreshStats()
}

// startLogTailer starts the log tailer for the selected instance
func (m *MonitorModel) startLogTailer() {
	// Find log path
	var logPath string
	for _, inst := range m.Instances {
		if inst.ID == m.SelectedInstance {
			logPath = inst.LogPath
			break
		}
	}

	if logPath == "" {
		return
	}

	// Create separate context for log tailer
	m.LogCtx, m.LogCancel = context.WithCancel(m.Ctx)

	// Clear buffer
	m.LogBuffer.Clear()

	// Start tailer
	m.LogTailer = &LogTailer{
		LogPath:    logPath,
		Filters:    m.LogLevelFilters,
		BufferSize: 1000,
	}

	m.WG.Add(1)
	go func() {
		defer m.WG.Done()
		m.LogTailer.Run(m.LogCtx, m.LogChan, m.ErrChan)
	}()
}

// stopLogTailer stops the log tailer
func (m *MonitorModel) stopLogTailer() {
	if m.LogCancel != nil {
		m.LogCancel()
		m.LogCancel = nil
	}
	m.LogBuffer.Clear()
}

// updateLogFilters updates the log level filters
func (m *MonitorModel) updateLogFilters() {
	if m.LogTailer != nil {
		m.LogTailer.UpdateFilters(m.LogLevelFilters)
	}
}

// refreshStats triggers an immediate stats refresh
func (m *MonitorModel) refreshStats() {
	// The poller will pick up the new parameters on its next tick
	// For immediate refresh, we could add a trigger channel
}

// detectChanges compares previous and current stats, populating flashCells
// for any values that changed. Skips on first load (prevStats == nil).
func (m *MonitorModel) detectChanges(prev, curr *UsageStats) {
	if prev == nil || curr == nil {
		return
	}
	now := time.Now()

	// Summary fields
	if prev.Summary.TotalRequests != curr.Summary.TotalRequests {
		m.flashCells["summary:requests"] = now
	}
	if prev.Summary.TotalTokens != curr.Summary.TotalTokens {
		m.flashCells["summary:tokens"] = now
	}
	if prev.Summary.TotalFallbacks != curr.Summary.TotalFallbacks {
		m.flashCells["summary:fallbacks"] = now
	}

	// Route fields
	for name, stats := range curr.ByRoute {
		if prevRoute, ok := prev.ByRoute[name]; ok {
			if prevRoute.Requests != stats.Requests {
				m.flashCells["route:"+name+":requests"] = now
			}
			if prevRoute.Tokens != stats.Tokens {
				m.flashCells["route:"+name+":tokens"] = now
			}
			if prevRoute.Fallbacks != stats.Fallbacks {
				m.flashCells["route:"+name+":fallbacks"] = now
			}
		} else {
			// New route appeared — flash all its fields
			m.flashCells["route:"+name+":requests"] = now
			m.flashCells["route:"+name+":tokens"] = now
			m.flashCells["route:"+name+":fallbacks"] = now
		}
	}

	// Model fields
	for name, stats := range curr.ByModel {
		if prevModel, ok := prev.ByModel[name]; ok {
			if prevModel.Requests != stats.Requests {
				m.flashCells["model:"+name+":requests"] = now
			}
			if prevModel.Tokens != stats.Tokens {
				m.flashCells["model:"+name+":tokens"] = now
			}
		} else {
			// New model appeared — flash all its fields
			m.flashCells["model:"+name+":requests"] = now
			m.flashCells["model:"+name+":tokens"] = now
		}
	}

	// User fields
	for name, stats := range curr.ByUser {
		if prevUser, ok := prev.ByUser[name]; ok {
			if prevUser.Requests != stats.Requests {
				m.flashCells["user:"+name+":requests"] = now
			}
			if prevUser.Tokens != stats.Tokens {
				m.flashCells["user:"+name+":tokens"] = now
			}
		} else {
			m.flashCells["user:"+name+":requests"] = now
			m.flashCells["user:"+name+":tokens"] = now
		}
	}

	// Group fields
	for name, stats := range curr.ByGroup {
		if prevGroup, ok := prev.ByGroup[name]; ok {
			if prevGroup.Requests != stats.Requests {
				m.flashCells["group:"+name+":requests"] = now
			}
			if prevGroup.Tokens != stats.Tokens {
				m.flashCells["group:"+name+":tokens"] = now
			}
			if prevGroup.Fallbacks != stats.Fallbacks {
				m.flashCells["group:"+name+":fallbacks"] = now
			}
		} else {
			m.flashCells["group:"+name+":requests"] = now
			m.flashCells["group:"+name+":tokens"] = now
			m.flashCells["group:"+name+":fallbacks"] = now
		}
	}
}

// flashStyle returns the appropriate style for a cell based on its flash state.
// If the cell is not flashing, returns the base style unchanged.
func (m *MonitorModel) flashStyle(key string, baseStyle lipgloss.Style) lipgloss.Style {
	startTime, ok := m.flashCells[key]
	if !ok {
		return baseStyle
	}
	elapsed := time.Since(startTime)
	if elapsed >= flashDuration {
		return baseStyle
	}
	if elapsed < flashPhase1Duration {
		return baseStyle.Background(FlashHighlightBg).Bold(true).Foreground(PrimaryText)
	}
	return baseStyle.Background(FlashFadeBg).Foreground(PrimaryText)
}

// waitForFlashTick returns a command that sends animation frame messages while flashes are active
func (m *MonitorModel) waitForFlashTick() tea.Cmd {
	return tea.Tick(flashTickInterval, func(t time.Time) tea.Msg {
		return FlashTickMsg(t)
	})
}

// calculateLayoutWidths calculates adaptive column widths based on window size
func (m *MonitorModel) calculateLayoutWidths() {
	width := m.WindowSize.Width

	// Leave 4 cells total: 2 for terminal margins + 2 for two panel borders
	availableWidth := width - 4
	if availableWidth < 50 {
		availableWidth = 50
	}

	// Split: 70% left, 30% right (but enforce minimums)
	m.LeftPanelWidth = int(float64(availableWidth) * 0.70)
	if m.LeftPanelWidth < 50 {
		m.LeftPanelWidth = 50
	}
	m.RightPanelWidth = availableWidth - m.LeftPanelWidth
	if m.RightPanelWidth < 20 {
		m.RightPanelWidth = 20
	}

	// Calculate table column widths for the left panel
	// Route table: Route (60%), Requests (20%), Tokens (20%)
	// Model table: Model (70%), Requests (15%), Tokens (15%)
	m.RouteColWidth = m.LeftPanelWidth - 2 // Account for borders
	m.ModelColWidth = m.LeftPanelWidth - 2
}

// shutdown cleans up and quits
func (m *MonitorModel) shutdown() tea.Cmd {
	return func() tea.Msg {
		// Cancel all contexts
		m.Cancel()
		if m.LogCancel != nil {
			m.LogCancel()
		}

		// Wait for goroutines with timeout
		done := make(chan struct{})
		go func() {
			m.WG.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}

		return tea.Quit()
	}
}

// parseLogLevel parses a log level string
func parseLogLevel(s string) LogLevel {
	switch s {
	case "VERBS":
		return LogLevelVerbs
	case "TRACE":
		return LogLevelTrace
	case "DEBUG":
		return LogLevelDebug
	case "INFO":
		return LogLevelInfo
	case "WARN", "WARNING":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	case "FATAL":
		return LogLevelFatal
	default:
		return LogLevelInfo
	}
}

// parseLogLine parses a log line into a LogLine struct
func parseLogLine(raw string) LogLine {
	// Expected format: [TIMESTAMP] [LEVEL] message
	// Example: [2026-04-01T16:32:22.519636+08:00] [INFO] Router started

	parts := splitLogLine(raw)
	if len(parts) < 4 {
		return LogLine{
			Timestamp: time.Now(),
			Level:     LogLevelInfo,
			Message:   raw,
			Raw:       raw,
		}
	}

	// Parse timestamp
	ts := strings.TrimPrefix(parts[0], "[")
	timestamp, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		timestamp = time.Now()
	}

	// Parse level (splitLogLine puts the level bracket-group in parts[2])
	levelStr := strings.TrimPrefix(strings.TrimSpace(parts[2]), "[")
	level := parseLogLevel(levelStr)

	// Message (splitLogLine puts the message in parts[3])
	message := strings.TrimSpace(parts[3])

	return LogLine{
		Timestamp: timestamp,
		Level:     level,
		Message:   message,
		Raw:       raw,
	}
}

// splitLogLine splits a log line into parts
// Using simple string split instead of regex for efficiency
func splitLogLine(s string) []string {
	var parts []string
	var current []byte
	inBracket := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if c == '[' {
			if len(current) > 0 {
				parts = append(parts, string(current))
				current = nil
			}
			inBracket = true
			current = append(current, c)
		} else if c == ']' && inBracket {
			current = append(current, c)
			parts = append(parts, string(current))
			current = nil
			inBracket = false
		} else if !inBracket && c == ' ' && len(parts) >= 2 {
			// After level, rest is message
			parts = append(parts, s[i:])
			break
		} else {
			current = append(current, c)
		}
	}

	if len(current) > 0 {
		parts = append(parts, string(current))
	}

	return parts
}

// Styles - Adaptive Theme (light/dark auto-detection via lipgloss.HasDarkBackground)
// Light: Dracula-inspired light variant | Dark: Catppuccin Mocha

// Base palette colors
var (
	// Background colors
	BaseBackground   = lipgloss.AdaptiveColor{Light: "#f8f8f2", Dark: "#1e1e2e"} // Main background
	PanelBackground  = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#313244"} // Panel backgrounds
	AltRowBackground = lipgloss.AdaptiveColor{Light: "#eff0eb", Dark: "#45475a"} // Alternate row

	// Text colors
	PrimaryText   = lipgloss.AdaptiveColor{Light: "#282a36", Dark: "#cdd6f4"} // Main text
	SecondaryText = lipgloss.AdaptiveColor{Light: "#6272a4", Dark: "#a6adc8"} // Secondary/muted
	HeaderText    = lipgloss.AdaptiveColor{Light: "#bd93f9", Dark: "#cba6f7"} // Headers

	// Accent colors
	SelectionAccent = lipgloss.AdaptiveColor{Light: "#8be9fd", Dark: "#89b4fa"} // Selected items
	BorderColor     = lipgloss.AdaptiveColor{Light: "#44475a", Dark: "#585b70"} // Borders

	// Status colors
	SuccessColor = lipgloss.AdaptiveColor{Light: "#2e7d32", Dark: "#a6e3a1"} // Running/success
	ErrorColor   = lipgloss.AdaptiveColor{Light: "#c62828", Dark: "#f38ba8"} // Errors
	WarningColor = lipgloss.AdaptiveColor{Light: "#e65100", Dark: "#fab387"} // Warnings
	InfoColor    = lipgloss.AdaptiveColor{Light: "#1565c0", Dark: "#89b4fa"} // Info
	DebugColor   = lipgloss.AdaptiveColor{Light: "#546e7a", Dark: "#6c7086"} // Debug
	TraceColor   = lipgloss.AdaptiveColor{Light: "#7b1fa2", Dark: "#f5c2e7"} // Trace
	VerboseColor = lipgloss.AdaptiveColor{Light: "#00796b", Dark: "#94e2d5"} // Verbose

	// Semantic color aliases (for clarity in usage)
	PrimaryAccent     = SelectionAccent // Cyan for selections
	SecondaryAccent   = HeaderText      // Purple for headers
	MutedColor        = SecondaryText   // Medium gray for muted text
	BackgroundColor   = BaseBackground  // Main background
	StatusBarBackground = BaseBackground // Same as main (no dark bar)
	SelectedBackground  = SelectionAccent // Cyan selection

	// Helper styles for rendering colored text
	MutedStyle   = lipgloss.NewStyle().Foreground(MutedColor)
	ErrorStyle   = lipgloss.NewStyle().Foreground(ErrorColor)
	SuccessStyle = lipgloss.NewStyle().Foreground(SuccessColor)
	AccentStyle  = lipgloss.NewStyle().Foreground(PrimaryAccent)

	// Flash animation styles
	FlashHighlightBg = lipgloss.AdaptiveColor{Light: "#8be9fd", Dark: "#45475a"} // bright cyan / dark overlay
	FlashFadeBg      = lipgloss.AdaptiveColor{Light: "#e0f4fd", Dark: "#313244"} // light cyan / panel bg
	FlashHighlightStyle = lipgloss.NewStyle().Background(FlashHighlightBg).Bold(true).Foreground(PrimaryText)
	FlashFadeStyle      = lipgloss.NewStyle().Background(FlashFadeBg).Foreground(PrimaryText)

	// UI Component Styles
	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderColor)

	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(PrimaryText).
			Background(SelectionAccent).
			Padding(0, 1)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(SecondaryText).
				Background(BackgroundColor).
				Padding(0, 1)

	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(HeaderText).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(BorderColor)

	TableRowStyle = lipgloss.NewStyle().
			Foreground(PrimaryText)

	TableRowAltStyle = lipgloss.NewStyle().
				Foreground(PrimaryText).
				Background(AltRowBackground)

	SelectedInstanceStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(PrimaryText).
				Background(SelectionAccent).
				Padding(0, 1)

	UnselectedInstanceStyle = lipgloss.NewStyle().
					Foreground(PrimaryText).
					Padding(0, 1)

	RunningIndicator  = lipgloss.NewStyle().Foreground(SuccessColor).SetString("●")
	StoppedIndicator  = lipgloss.NewStyle().Foreground(MutedColor).SetString("○")
	StatusBarStyle    = lipgloss.NewStyle().Foreground(SecondaryText).Background(StatusBarBackground).Padding(0, 1)
	ConsoleTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(PrimaryAccent)

	LogLevelStyles = map[LogLevel]lipgloss.Style{
		LogLevelVerbs: lipgloss.NewStyle().Foreground(VerboseColor), // teal
		LogLevelTrace: lipgloss.NewStyle().Foreground(TraceColor),   // purple
		LogLevelDebug: lipgloss.NewStyle().Foreground(DebugColor),   // slate
		LogLevelInfo:  lipgloss.NewStyle().Foreground(InfoColor),    // blue
		LogLevelWarn:  lipgloss.NewStyle().Foreground(WarningColor), // dark amber / peach
		LogLevelError: lipgloss.NewStyle().Foreground(ErrorColor),   // red
		LogLevelFatal: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#880e4f", Dark: "#f38ba8"}).Bold(true), // crimson + bold
	}
)

// Import for FormatTokens
func init() {
	// Prevents unused import error
	_ = usage.FormatTokens
}