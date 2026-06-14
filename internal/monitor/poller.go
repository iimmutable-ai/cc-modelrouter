package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/iimmutable/cc-modelrouter/internal/usage"
)

// StatsPoller polls the database for usage stats
type StatsPoller struct {
	mu                sync.RWMutex
	Interval          time.Duration
	DBPath            string
	DateRange         DateRange
	InstanceID        string
	ConsecutiveErrors int
}

// UpdateDateRange safely updates the date range
func (sp *StatsPoller) UpdateDateRange(dr DateRange) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.DateRange = dr
}

// UpdateInstance safely updates the instance filter
func (sp *StatsPoller) UpdateInstance(instanceID string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.InstanceID = instanceID
}

// getParams safely reads current parameters
func (sp *StatsPoller) getParams() (DateRange, string) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.DateRange, sp.InstanceID
}

// Run executes the polling loop
func (sp *StatsPoller) Run(ctx context.Context, statsChan chan<- *UsageStats, errChan chan<- error) {
	ticker := time.NewTicker(sp.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dateRange, instanceID := sp.getParams()
			stats, err := sp.fetchUsageStats(dateRange, instanceID)

			if err != nil {
				sp.ConsecutiveErrors++

				select {
				case errChan <- fmt.Errorf("stats fetch failed (attempt %d): %w", sp.ConsecutiveErrors, err):
				case <-ctx.Done():
					return
				}

				// Exponential backoff
				if sp.ConsecutiveErrors > 1 {
					backoff := time.Duration(1<<uint(sp.ConsecutiveErrors-1)) * time.Second
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
					select {
					case <-time.After(backoff):
					case <-ctx.Done():
						return
					}
				}
				continue
			}

			// Success - reset error count
			if sp.ConsecutiveErrors > 0 {
				sp.ConsecutiveErrors = 0
				select {
				case errChan <- nil: // nil signals recovery
				case <-ctx.Done():
					return
				}
			}

			select {
			case statsChan <- stats:
			case <-ctx.Done():
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// fetchUsageStats queries the database and aggregates results
func (sp *StatsPoller) fetchUsageStats(dateRange DateRange, instanceID string) (*UsageStats, error) {
	db, err := usage.InitDB(sp.DBPath)
	if err != nil {
		return &UsageStats{
			Summary:   usage.Summary{},
			ByRoute:   make(map[string]*usage.RouteStats),
			ByModel:   make(map[string]*usage.ModelStats),
			ByUser:    make(map[string]*usage.UserStats),
			ByGroup:   make(map[string]*usage.GroupStats),
			Timestamp: time.Now(),
		}, fmt.Errorf("database unavailable: %w", err)
	}
	defer db.Close()

	start, end := sp.calculateDateRange(dateRange)
	records, err := usage.GetRecordsByPeriod(db, instanceID, start, end)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	// Detect multi-user mode: any record with non-empty UserName or GroupName
	isMultiUser := false
	for _, r := range records {
		if r.UserName != "" || r.GroupName != "" {
			isMultiUser = true
			break
		}
	}

	stats := &UsageStats{
		Summary:     usage.AggregateSummary(records),
		ByRoute:     usage.AggregateByRoute(records),
		ByModel:     usage.AggregateByModel(records),
		ByUser:      usage.AggregateByUser(records),
		ByGroup:     usage.AggregateByGroup(records),
		IsMultiUser: isMultiUser,
		Timestamp:   time.Now(),
	}

	return stats, nil
}

// calculateDateRange converts DateRange enum to start/end times
func (sp *StatsPoller) calculateDateRange(dateRange DateRange) (time.Time, time.Time) {
	now := time.Now()
	loc := now.Location()

	switch dateRange {
	case DateRangeToday:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		end := start.Add(24 * time.Hour)
		return start, end

	case DateRangeWeek:
		// This calendar week (Monday to Sunday)
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		daysFromMonday := weekday - 1
		start := time.Date(now.Year(), now.Month(), now.Day()-daysFromMonday, 0, 0, 0, 0, loc)
		end := start.Add(7 * 24 * time.Hour)
		return start, end

	case DateRangeMonth:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		end := start.AddDate(0, 1, 0)
		return start, end

	case DateRangeYTD:
		start := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		end := now
		return start, end

	case DateRangeTTM:
		start := now.AddDate(-1, 0, 0)
		end := now
		return start, end

	default:
		return now, now
	}
}

// calculateDateRangeForInstances calculates date range for instance filtering
func calculateDateRangeForInstances(dateRange DateRange) (time.Time, time.Time) {
	now := time.Now()
	loc := now.Location()

	switch dateRange {
	case DateRangeToday:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		end := start.Add(24 * time.Hour)
		return start, end

	case DateRangeWeek:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		daysFromMonday := weekday - 1
		start := time.Date(now.Year(), now.Month(), now.Day()-daysFromMonday, 0, 0, 0, 0, loc)
		end := start.AddDate(0, 3, 0) // Look ahead 3 months for week view
		return start, end

	case DateRangeMonth:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		end := start.AddDate(0, 3, 0) // Look ahead 3 months
		return start, end

	case DateRangeYTD:
		start := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		end := now.AddDate(0, 3, 0)
		return start, end

	case DateRangeTTM:
		start := now.AddDate(-1, 0, 0)
		end := now
		return start, end

	default:
		return now, now
	}
}

// discoverInstances finds all instances in the given date range
func discoverInstances(dateRange DateRange) ([]InstanceInfo, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home dir: %w", err)
	}

	instancesDir := filepath.Join(homeDir, ".cc-modelrouter", "instances")

	files, err := os.ReadDir(instancesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []InstanceInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read instances dir: %w", err)
	}

	start, end := calculateDateRangeForInstances(dateRange)
	var instances []InstanceInfo

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		path := filepath.Join(instancesDir, file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var meta struct {
			ID        string    `json:"id"`
			PID       int       `json:"pid"`
			StartTime time.Time `json:"startTime"`
		}

		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		// Filter by date range
		if meta.StartTime.Before(start) || meta.StartTime.After(end) {
			continue
		}

		isRunning := processExists(meta.PID)
		logPath := filepath.Join(homeDir, ".cc-modelrouter", "logs", meta.ID+".log")

		instances = append(instances, InstanceInfo{
			ID:        meta.ID,
			IsRunning: isRunning,
			StartTime: meta.StartTime,
			LogPath:   logPath,
		})
	}

	// Sort: running first, then by start time descending
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].IsRunning != instances[j].IsRunning {
			return instances[i].IsRunning
		}
		return instances[i].StartTime.After(instances[j].StartTime)
	})

	return instances, nil
}

// processExists checks if a process with the given PID is running
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to test if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}