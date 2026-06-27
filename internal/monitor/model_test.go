package monitor

import (
	"errors"
	"testing"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
	lipgloss "github.com/charmbracelet/lipgloss"
)

func TestDateRange_String(t *testing.T) {
	tests := []struct {
		dr  DateRange
		want string
	}{
		{DateRangeToday, "TODAY"},
		{DateRangeWeek, "WEEK"},
		{DateRangeMonth, "MONTH"},
		{DateRangeYTD, "YTD"},
		{DateRangeTTM, "TTM"},
		{DateRange(99), "TODAY"}, // default
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.dr.String(); got != tt.want {
				t.Errorf("DateRange(%d).String() = %q, want %q", tt.dr, got, tt.want)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  LogLevel
	}{
		{"VERBS", LogLevelVerbs},
		{"TRACE", LogLevelTrace},
		{"DEBUG", LogLevelDebug},
		{"INFO", LogLevelInfo},
		{"WARN", LogLevelWarn},
		{"WARNING", LogLevelWarn},
		{"ERROR", LogLevelError},
		{"FATAL", LogLevelFatal},
		{"unknown", LogLevelInfo}, // default
		{"", LogLevelInfo},       // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseLogLevel(tt.input); got != tt.want {
				t.Errorf("parseLogLevel(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseLogLine(t *testing.T) {
	t.Run("valid log line", func(t *testing.T) {
		raw := "[2026-04-01T12:00:00.000000+08:00] [INFO] Hello world"
		line := parseLogLine(raw)

		if line.Level != LogLevelInfo {
			t.Errorf("expected LogLevelInfo, got %d", line.Level)
		}
		if line.Message != "Hello world" {
			t.Errorf("expected 'Hello world', got %q", line.Message)
		}
		if line.Raw != raw {
			t.Error("expected raw to be preserved")
		}
	})

	t.Run("malformed log line", func(t *testing.T) {
		raw := "this is not a proper log line"
		line := parseLogLine(raw)

		if line.Level != LogLevelInfo {
			t.Errorf("expected default LogLevelInfo, got %d", line.Level)
		}
		if line.Message != raw {
			t.Errorf("expected raw message, got %q", line.Message)
		}
	})

	t.Run("empty log line", func(t *testing.T) {
		line := parseLogLine("")
		if line.Message != "" {
			t.Errorf("expected empty message, got %q", line.Message)
		}
	})
}

func TestSplitLogLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int // expected number of parts
	}{
		{"standard log line", "[2026-04-01T12:00:00+08:00] [INFO] message content here", 4},
		{"short line", "[ts] [LEVEL] msg", 4},
		{"no brackets", "plain text", 1},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := splitLogLine(tt.input)
			if len(parts) != tt.want {
				t.Errorf("splitLogLine(%q) returned %d parts, want %d", tt.input, len(parts), tt.want)
			}
		})
	}
}

func TestNewMonitorModel(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	if m.RefreshInterval != 1*time.Second {
		t.Errorf("expected RefreshInterval 1s, got %v", m.RefreshInterval)
	}
	if m.DBPath != "/tmp/test.db" {
		t.Errorf("expected DBPath '/tmp/test.db', got %q", m.DBPath)
	}
	if m.SelectedDateRange != DateRangeToday {
		t.Errorf("expected DateRangeToday, got %d", m.SelectedDateRange)
	}
	if m.SelectedInstance != "" {
		t.Errorf("expected empty SelectedInstance, got %q", m.SelectedInstance)
	}
	if m.ConsoleLogEnabled {
		t.Error("expected ConsoleLogEnabled false")
	}
	if m.ConsoleLogPaused {
		t.Error("expected ConsoleLogPaused false")
	}
	if m.LogBuffer == nil {
		t.Error("expected LogBuffer to be initialized")
	}
	if m.StatsPoller == nil {
		t.Error("expected StatsPoller to be initialized")
	}
	if m.StatsPoller.Interval != 1*time.Second {
		t.Errorf("expected poller interval 1s, got %v", m.StatsPoller.Interval)
	}
}

func TestMonitorModel_CalculateLayoutWidths(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")
	m.WindowSize = tea.WindowSizeMsg{Width: 100, Height: 24}

	m.calculateLayoutWidths()

	if m.LeftPanelWidth < 50 {
		t.Errorf("expected LeftPanelWidth >= 50, got %d", m.LeftPanelWidth)
	}
	if m.RightPanelWidth < 20 {
		t.Errorf("expected RightPanelWidth >= 20, got %d", m.RightPanelWidth)
	}
}

func TestMonitorModel_UpdateSelectedInstance(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")
	m.Instances = []InstanceInfo{
		{ID: "inst-1", IsRunning: true, StartTime: time.Now()},
		{ID: "inst-2", IsRunning: false, StartTime: time.Now()},
	}

	// Select first instance
	m.InstanceCursor = 1
	m.updateSelectedInstance()

	if m.SelectedInstance != "inst-1" {
		t.Errorf("expected 'inst-1', got %q", m.SelectedInstance)
	}

	// Select all (cursor 0)
	m.InstanceCursor = 0
	m.updateSelectedInstance()

	if m.SelectedInstance != "" {
		t.Errorf("expected empty (all), got %q", m.SelectedInstance)
	}
}

func TestMonitorModel_UpdateSelectedInstance_StopsLogOnSwitch(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")
	m.Instances = []InstanceInfo{
		{ID: "inst-1", IsRunning: true, StartTime: time.Now()},
	}
	m.InstanceCursor = 1
	m.updateSelectedInstance()
	m.ConsoleLogEnabled = true

	// Switch to all
	m.InstanceCursor = 0
	m.updateSelectedInstance()

	if m.ConsoleLogEnabled {
		t.Error("expected ConsoleLogEnabled to be false after switching instance")
	}
}

func TestMonitorModel_StatsPoller_UpdateMethods(t *testing.T) {
	sp := &StatsPoller{
		Interval:   1 * time.Second,
		DBPath:     "/tmp/test.db",
		DateRange:  DateRangeToday,
		InstanceID: "",
	}

	sp.UpdateDateRange(DateRangeWeek)
	if sp.DateRange != DateRangeWeek {
		t.Errorf("expected DateRangeWeek, got %d", sp.DateRange)
	}

	sp.UpdateInstance("inst-123")
	if sp.InstanceID != "inst-123" {
		t.Errorf("expected 'inst-123', got %q", sp.InstanceID)
	}

	dr, inst := sp.getParams()
	if dr != DateRangeWeek {
		t.Errorf("expected DateRangeWeek, got %d", dr)
	}
	if inst != "inst-123" {
		t.Errorf("expected 'inst-123', got %q", inst)
	}
}

func TestMonitorModel_LogLevelFilterToggle(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	initial := m.LogLevelFilters
	m.handleKeyPress(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'3'}})) // toggle DEBUG

	if m.LogLevelFilters == initial {
		t.Error("expected LogLevelFilters to change after toggling DEBUG")
	}

	// Toggle again should restore
	m.handleKeyPress(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'3'}}))
	if m.LogLevelFilters != initial {
		t.Error("expected LogLevelFilters to be restored after second toggle")
	}
}

func TestMonitorModel_DateRangeNavigation(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	// At DateRangeToday (0), left should not go below 0
	m.handleKeyPress(tea.KeyMsg(tea.Key{Type: tea.KeyLeft}))
	if m.SelectedDateRange != DateRangeToday {
		t.Errorf("expected DateRangeToday after left at min, got %d", m.SelectedDateRange)
	}

	// Right should advance
	m.handleKeyPress(tea.KeyMsg(tea.Key{Type: tea.KeyRight}))
	if m.SelectedDateRange != DateRangeWeek {
		t.Errorf("expected DateRangeWeek after right, got %d", m.SelectedDateRange)
	}

	// Right to max
	m.handleKeyPress(tea.KeyMsg(tea.Key{Type: tea.KeyRight}))
	m.handleKeyPress(tea.KeyMsg(tea.Key{Type: tea.KeyRight}))
	m.handleKeyPress(tea.KeyMsg(tea.Key{Type: tea.KeyRight}))
	if m.SelectedDateRange != DateRangeTTM {
		t.Errorf("expected DateRangeTTM at max, got %d", m.SelectedDateRange)
	}

	// Right at max should stay at max
	m.handleKeyPress(tea.KeyMsg(tea.Key{Type: tea.KeyRight}))
	if m.SelectedDateRange != DateRangeTTM {
		t.Errorf("expected DateRangeTTM after right at max, got %d", m.SelectedDateRange)
	}
}

func TestDetectChanges_Summary(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")

	prev := &UsageStats{
		Summary: usage.Summary{TotalRequests: 10, TotalTokens: 5000, TotalFallbacks: 1},
		ByRoute: map[string]*usage.RouteStats{},
		ByModel: map[string]*usage.ModelStats{},
	}
	curr := &UsageStats{
		Summary: usage.Summary{TotalRequests: 12, TotalTokens: 6000, TotalFallbacks: 1},
		ByRoute: map[string]*usage.RouteStats{},
		ByModel: map[string]*usage.ModelStats{},
	}

	m.detectChanges(prev, curr)

	// Requests and tokens changed, fallbacks did not
	if _, ok := m.flashCells["summary:requests"]; !ok {
		t.Error("expected flash for summary:requests")
	}
	if _, ok := m.flashCells["summary:tokens"]; !ok {
		t.Error("expected flash for summary:tokens")
	}
	if _, ok := m.flashCells["summary:fallbacks"]; ok {
		t.Error("expected no flash for summary:fallbacks (unchanged)")
	}
}

func TestDetectChanges_RoutesAndModels(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")

	prev := &UsageStats{
		ByRoute: map[string]*usage.RouteStats{
			"default": {Route: "default", Requests: 5, Tokens: 1000, Fallbacks: 0},
		},
		ByModel: map[string]*usage.ModelStats{
			"glm-4": {Model: "glm-4", Requests: 3, Tokens: 800},
		},
	}
	curr := &UsageStats{
		ByRoute: map[string]*usage.RouteStats{
			"default": {Route: "default", Requests: 7, Tokens: 1000, Fallbacks: 1},
			"think":   {Route: "think", Requests: 2, Tokens: 500, Fallbacks: 0},
		},
		ByModel: map[string]*usage.ModelStats{
			"glm-4":   {Model: "glm-4", Requests: 3, Tokens: 900},
			"claude-4": {Model: "claude-4", Requests: 1, Tokens: 200},
		},
	}

	m.detectChanges(prev, curr)

	// Existing route: requests and fallbacks changed, tokens did not
	if _, ok := m.flashCells["route:default:requests"]; !ok {
		t.Error("expected flash for route:default:requests")
	}
	if _, ok := m.flashCells["route:default:fallbacks"]; !ok {
		t.Error("expected flash for route:default:fallbacks")
	}
	if _, ok := m.flashCells["route:default:tokens"]; ok {
		t.Error("expected no flash for route:default:tokens (unchanged)")
	}

	// New route: all fields flash
	if _, ok := m.flashCells["route:think:requests"]; !ok {
		t.Error("expected flash for new route think:requests")
	}
	if _, ok := m.flashCells["route:think:tokens"]; !ok {
		t.Error("expected flash for new route think:tokens")
	}

	// Existing model: tokens changed, requests did not
	if _, ok := m.flashCells["model:glm-4:tokens"]; !ok {
		t.Error("expected flash for model:glm-4:tokens")
	}
	if _, ok := m.flashCells["model:glm-4:requests"]; ok {
		t.Error("expected no flash for model:glm-4:requests (unchanged)")
	}

	// New model: all fields flash
	if _, ok := m.flashCells["model:claude-4:requests"]; !ok {
		t.Error("expected flash for new model claude-4:requests")
	}
	if _, ok := m.flashCells["model:claude-4:tokens"]; !ok {
		t.Error("expected flash for new model claude-4:tokens")
	}
}

func TestDetectChanges_NilPrev(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")

	curr := &UsageStats{
		Summary: usage.Summary{TotalRequests: 5, TotalTokens: 1000, TotalFallbacks: 0},
		ByRoute: map[string]*usage.RouteStats{},
		ByModel: map[string]*usage.ModelStats{},
	}

	m.detectChanges(nil, curr)

	// No flashes on first load
	if len(m.flashCells) != 0 {
		t.Errorf("expected no flashes on first load, got %d", len(m.flashCells))
	}
}

func TestDetectChanges_NilCurr(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")

	prev := &UsageStats{
		Summary: usage.Summary{TotalRequests: 5, TotalTokens: 1000, TotalFallbacks: 0},
		ByRoute: map[string]*usage.RouteStats{},
		ByModel: map[string]*usage.ModelStats{},
	}

	m.detectChanges(prev, nil)

	if len(m.flashCells) != 0 {
		t.Errorf("expected no flashes with nil curr, got %d", len(m.flashCells))
	}
}

func TestFlashStyle_Phases(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")
	baseStyle := lipgloss.NewStyle().Foreground(PrimaryText)

	// No flash — returns base style unchanged
	result := m.flashStyle("nonexistent", baseStyle)
	if result.GetBackground() != baseStyle.GetBackground() {
		t.Error("expected base style background for non-existent key")
	}

	// Phase 1: bright highlight (elapsed < 200ms)
	m.flashCells["key1"] = time.Now()
	result = m.flashStyle("key1", baseStyle)
	if result.GetBackground() != FlashHighlightBg {
		t.Error("expected FlashHighlightBg in phase 1")
	}

	// Phase 2: fade (200ms <= elapsed < 500ms)
	m.flashCells["key2"] = time.Now().Add(-250 * time.Millisecond)
	result = m.flashStyle("key2", baseStyle)
	if result.GetBackground() != FlashFadeBg {
		t.Error("expected FlashFadeBg in phase 2")
	}

	// Expired: returns base style (elapsed >= 500ms)
	m.flashCells["key3"] = time.Now().Add(-600 * time.Millisecond)
	result = m.flashStyle("key3", baseStyle)
	if result.GetBackground() != baseStyle.GetBackground() {
		t.Error("expected base style background for expired flash")
	}
}

func TestFlashStyle_PreservesBaseBackground(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")
	altStyle := lipgloss.NewStyle().Foreground(PrimaryText).Background(AltRowBackground)

	// Verify that flashing overrides the alt row background
	m.flashCells["altkey"] = time.Now()
	result := m.flashStyle("altkey", altStyle)
	if result.GetBackground() != FlashHighlightBg {
		t.Error("expected flash to override alt row background")
	}

	// No flash preserves alt background
	delete(m.flashCells, "altkey")
	result = m.flashStyle("altkey", altStyle)
	if result.GetBackground() != AltRowBackground {
		t.Error("expected alt row background preserved when not flashing")
	}
}

func TestFlashTickMsg_PrunesExpired(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")

	// Add one expired and one active flash
	m.flashCells["expired"] = time.Now().Add(-1 * time.Second)
	m.flashCells["active"] = time.Now()
	m.flashAnimating = true

	// Simulate the FlashTickMsg handling (same logic as Update)
	now := time.Now()
	for key, startTime := range m.flashCells {
		if now.Sub(startTime) >= flashDuration {
			delete(m.flashCells, key)
		}
	}

	if _, ok := m.flashCells["expired"]; ok {
		t.Error("expected expired entry to be pruned")
	}
	if _, ok := m.flashCells["active"]; !ok {
		t.Error("expected active entry to remain")
	}
}

// ---------------------------------------------------------------------------
// Update() tests
// ---------------------------------------------------------------------------

func TestUpdate_StatsUpdateMsg(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	stats := &UsageStats{
		Summary: usage.Summary{TotalRequests: 42, TotalTokens: 5000, TotalFallbacks: 1},
		ByRoute: map[string]*usage.RouteStats{
			"default": {Route: "default", Requests: 42, Tokens: 5000, Fallbacks: 1},
		},
		ByModel: map[string]*usage.ModelStats{
			"glm-4": {Model: "glm-4", Requests: 42, Tokens: 5000},
		},
		Timestamp: time.Now(),
	}

	newModel, cmd := m.Update(StatsUpdateMsg{Stats: stats})

	if newModel == nil {
		t.Fatal("expected non-nil model")
	}
	if m.Stats != stats {
		t.Error("expected Stats to be updated")
	}
	if m.LastError != nil {
		t.Error("expected LastError to be nil after stats update")
	}
	if m.prevStats != stats {
		t.Error("expected prevStats to be set")
	}
	// First stats update: no flashes (prevStats was nil)
	if len(m.flashCells) != 0 {
		t.Errorf("expected no flashes on first update, got %d", len(m.flashCells))
	}
	// Should return a command to wait for more stats
	if cmd == nil {
		t.Error("expected a non-nil command to continue listening")
	}
}

func TestUpdate_StatsUpdateMsgWithChanges(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	// Set initial stats
	m.Stats = &UsageStats{
		Summary: usage.Summary{TotalRequests: 10, TotalTokens: 1000, TotalFallbacks: 0},
		ByRoute: map[string]*usage.RouteStats{},
		ByModel: map[string]*usage.ModelStats{},
	}
	m.prevStats = m.Stats

	// Update with different stats
	newStats := &UsageStats{
		Summary: usage.Summary{TotalRequests: 20, TotalTokens: 2000, TotalFallbacks: 1},
		ByRoute: map[string]*usage.RouteStats{},
		ByModel: map[string]*usage.ModelStats{},
	}

	m.Update(StatsUpdateMsg{Stats: newStats})

	// Flash cells should be populated for changed values
	if _, ok := m.flashCells["summary:requests"]; !ok {
		t.Error("expected flash for summary:requests")
	}
	if _, ok := m.flashCells["summary:tokens"]; !ok {
		t.Error("expected flash for summary:tokens")
	}
	if _, ok := m.flashCells["summary:fallbacks"]; !ok {
		t.Error("expected flash for summary:fallbacks")
	}
	// Flash animation should be started
	if !m.flashAnimating {
		t.Error("expected flashAnimating to be true")
	}
}

func TestUpdate_LogMsg(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	rawLine := "[2026-04-01T12:00:00.000000+08:00] [INFO] Hello world"
	newModel, cmd := m.Update(LogMsg{Line: rawLine})

	if newModel == nil {
		t.Fatal("expected non-nil model")
	}

	// Verify the log was parsed and buffered
	if m.LogBuffer.Size() != 1 {
		t.Fatalf("expected 1 log entry in buffer, got %d", m.LogBuffer.Size())
	}
	// Should return a command to wait for more logs
	if cmd == nil {
		t.Error("expected a non-nil command to continue listening")
	}
}

func TestUpdate_LogMsgMalformed(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	m.Update(LogMsg{Line: "this is not a proper log line"})

	if m.LogBuffer.Size() != 1 {
		t.Fatalf("expected 1 log entry in buffer, got %d", m.LogBuffer.Size())
	}
}

func TestUpdate_ErrorMsg(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	testErr := errors.New("database connection failed")
	newModel, cmd := m.Update(ErrorMsg{Error: testErr})

	if newModel == nil {
		t.Fatal("expected non-nil model")
	}
	if m.LastError != testErr {
		t.Errorf("expected LastError to be set, got %v", m.LastError)
	}
	if cmd == nil {
		t.Error("expected a non-nil command to continue listening for errors")
	}
}

func TestUpdate_RecoveryMsg(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	// Set an error first
	m.LastError = errors.New("previous error")

	newModel, cmd := m.Update(RecoveryMsg{})

	if newModel == nil {
		t.Fatal("expected non-nil model")
	}
	if m.LastError != nil {
		t.Errorf("expected LastError to be cleared, got %v", m.LastError)
	}
	if cmd == nil {
		t.Error("expected a non-nil command to continue listening")
	}
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	m := NewMonitorModel(1*time.Second, "/tmp/test.db")

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	newModel, cmd := m.Update(msg)

	if newModel == nil {
		t.Fatal("expected non-nil model")
	}
	if m.WindowSize.Width != 120 {
		t.Errorf("expected Width 120, got %d", m.WindowSize.Width)
	}
	if m.WindowSize.Height != 40 {
		t.Errorf("expected Height 40, got %d", m.WindowSize.Height)
	}
	// Layout widths should be recalculated
	if m.LeftPanelWidth == 0 {
		t.Error("expected LeftPanelWidth to be recalculated")
	}
	if m.RightPanelWidth == 0 {
		t.Error("expected RightPanelWidth to be recalculated")
	}
	if cmd != nil {
		t.Error("expected nil command for WindowSizeMsg")
	}
}

func TestUpdate_FlashTickMsg(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")

	// Add an expired flash cell
	m.flashCells["expired"] = time.Now().Add(-1 * time.Second)
	m.flashAnimating = true

	newModel, cmd := m.Update(FlashTickMsg(time.Now()))

	if newModel == nil {
		t.Fatal("expected non-nil model")
	}
	// Expired flash should be pruned
	if _, ok := m.flashCells["expired"]; ok {
		t.Error("expected expired flash to be pruned")
	}
	// Animation should stop since no more flashes
	if m.flashAnimating {
		t.Error("expected flashAnimating to be false after all flashes expire")
	}
	if cmd != nil {
		t.Error("expected nil command when no flashes remain")
	}
}

func TestUpdate_FlashTickMsgContinuesAnimation(t *testing.T) {
	m := NewMonitorModel(500*time.Millisecond, "/tmp/test.db")

	// Add an active flash cell
	m.flashCells["active"] = time.Now()
	m.flashAnimating = true

	_, cmd := m.Update(FlashTickMsg(time.Now()))

	// Active flash should remain
	if _, ok := m.flashCells["active"]; !ok {
		t.Error("expected active flash to remain")
	}
	if !m.flashAnimating {
		t.Error("expected flashAnimating to remain true")
	}
	if cmd == nil {
		t.Error("expected a non-nil command to continue flash tick")
	}
}
