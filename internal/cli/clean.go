package cli

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/iimmutable-ai/cc-modelrouter/internal/usage"
	"github.com/spf13/cobra"
)

// durationPattern matches duration strings like "30d", "7d", "24h", "12h"
var durationPattern = regexp.MustCompile(`^(\d+)([dhm])$`)

// parseDuration parses a human-readable duration string (e.g., "30d", "24h", "12h", "90m").
func parseDuration(s string) (time.Duration, error) {
	matches := durationPattern.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid duration format %q: expected format like 30d, 24h, 90m", s)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid duration value %q: %w", matches[1], err)
	}

	switch matches[2] {
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	default:
		return 0, fmt.Errorf("unsupported duration unit %q", matches[2])
	}
}

// NewCleanCommand creates the clean command.
func NewCleanCommand() *cobra.Command {
	var usageBefore string
	var usageAll bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove stale instance files and prune usage data",
		Long: `Removes instance metadata files for processes that are no longer running.
Can also prune old usage records from the usage database.

Over time, instance metadata files can accumulate if processes terminate abnormally.
This command cleans up these stale files.

By default, only removes instances whose processes are no longer running.

Flags:
  -a, --all              Remove all instance files including running ones.
                          Use with caution - this will remove metadata for active instances.
      --usage-before <d> Prune usage records older than the given duration.
                          Duration format: 30d (days), 24h (hours), 90m (minutes).
      --usage-all         Delete all usage records.

Examples:
  # Remove stale instance files only
  ccrouter clean

  # Remove all instance files (use with caution)
  ccrouter clean --all

  # Prune usage records older than 30 days
  ccrouter clean --usage-before 30d

  # Delete all usage records
  ccrouter clean --usage-all

  # Combined: prune old usage + clean instances
  ccrouter clean --usage-before 30d`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClean(cmd, args, usageBefore, usageAll)
		},
	}

	cmd.Flags().BoolP("all", "a", false, "Remove all instance files including running ones")
	cmd.Flags().StringVar(&usageBefore, "usage-before", "", "Prune usage records older than duration (e.g., 30d, 24h, 90m)")
	cmd.Flags().BoolVar(&usageAll, "usage-all", false, "Delete all usage records")

	return cmd
}

func runClean(cmd *cobra.Command, args []string, usageBefore string, usageAll bool) error {
	// Handle usage data pruning
	if usageAll || usageBefore != "" {
		dbPath, err := usage.DBPath()
		if err != nil {
			return fmt.Errorf("failed to get usage db path: %w", err)
		}

		// Check if the database exists before trying to open it
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Println("No usage database found — nothing to prune")
		} else {
			db, err := usage.InitDB(dbPath)
			if err != nil {
				return fmt.Errorf("failed to open usage database: %w", err)
			}
			defer db.Close()

			if usageAll {
				count, err := usage.DeleteAllRecords(db)
				if err != nil {
					return fmt.Errorf("failed to delete usage records: %w", err)
				}
				fmt.Printf("Deleted %d usage record(s)\n", count)
			} else {
				duration, err := parseDuration(usageBefore)
				if err != nil {
					return err
				}
				before := time.Now().Add(-duration)
				count, err := usage.PruneRecords(db, before)
				if err != nil {
					return fmt.Errorf("failed to prune usage records: %w", err)
				}
				fmt.Printf("Pruned %d usage record(s) older than %s\n", count, usageBefore)
			}
		}
	}

	// Instance cleanup (existing behavior)
	cleanAll, _ := cmd.Flags().GetBool("all")

	instances, err := daemon.ListInstances()
	if err != nil {
		return fmt.Errorf("failed to list instances: %w", err)
	}

	if len(instances) == 0 && !usageAll && usageBefore == "" {
		fmt.Println("No instances to clean")
		return nil
	}

	if len(instances) == 0 {
		return nil
	}

	removed := 0
	for _, inst := range instances {
		shouldRemove := cleanAll || !daemon.IsRunning(inst)

		if shouldRemove {
			if err := daemon.DeleteInstance(inst.ID); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Failed to remove %s: %v\n", inst.ID, err)
			} else {
				fmt.Printf("Removed stale instance: %s\n", inst.ID)
				removed++
			}
		}
	}

	if removed == 0 {
		fmt.Println("No stale instances to remove")
	} else {
		fmt.Printf("Removed %d stale instance(s)\n", removed)
	}

	return nil
}
