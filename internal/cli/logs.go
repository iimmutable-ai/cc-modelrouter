package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/daemon"
	"github.com/spf13/cobra"
)

// NewLogsCommand creates the logs command.
func NewLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs [instance-id]",
		Short: "Show instance logs",
		Long: `Shows logs for a router instance.

Note: File-based logging is not yet implemented. Currently, logs are only
available via stdout/stderr of the running process.

Arguments:
  instance-id    Optional. The specific instance ID to show logs for.
                  If omitted, shows logs for the most recent running instance.

Flags:
  -f, --follow    Follow log output (like tail -f).
                  Continuously display new log entries as they are written.

  -n, --tail <number>  Number of lines to show from the end of the log.
                      Default: 100

Examples:
  # Show logs from the most recent instance
  ccrouter logs

  # Show logs from a specific instance
  ccrouter logs abc123def456

  # Follow logs in real-time
  ccrouter logs --follow

  # Show last 50 lines
  ccrouter logs --tail 50`,
		RunE:  runLogs,
	}

	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	cmd.Flags().IntP("tail", "n", 100, "Number of lines to show from the end")

	return cmd
}

func runLogs(cmd *cobra.Command, args []string) error {
	follow, _ := cmd.Flags().GetBool("follow")
	tailLines, _ := cmd.Flags().GetInt("tail")

	// Get instance ID
	var instanceID string
	if len(args) > 0 {
		instanceID = args[0]
	} else {
		// Find the most recent running instance
		instances, err := daemon.ListInstances()
		if err != nil {
			return fmt.Errorf("failed to list instances: %w", err)
		}

		var latest *daemon.InstanceMetadata
		for _, inst := range instances {
			if daemon.IsRunning(inst) {
				if latest == nil || inst.StartTime.After(latest.StartTime) {
					latest = inst
				}
			}
		}

		if latest == nil {
			fmt.Println("No running instances found")
			return nil
		}
		instanceID = latest.ID
	}

	fmt.Printf("Showing logs for instance: %s\n\n", instanceID)

	// Find log file
	logPath, err := getLogPath(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get log path: %w", err)
	}

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Println("No logs available for this instance")
		fmt.Println("\nNote: Logging to file is not yet implemented.")
		fmt.Println("For now, check stdout/stderr of the running process.")
		return nil
	}

	// Open log file
	file, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	if follow {
		return followLogs(file, logPath)
	}

	return showLogs(file, tailLines)
}

func getLogPath(instanceID string) (string, error) {
	instancesDir, err := daemon.InstancesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(instancesDir, instanceID+".log"), nil
}

func showLogs(file *os.File, tailLines int) error {
	scanner := bufio.NewScanner(file)
	var lines []string

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}

	// Show last N lines
	start := 0
	if len(lines) > tailLines {
		start = len(lines) - tailLines
	}

	for i := start; i < len(lines); i++ {
		fmt.Println(lines[i])
	}

	return nil
}

func followLogs(file *os.File, logPath string) error {
	// First show existing content
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}

	// Get current position
	_, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("failed to get file position: %w", err)
	}

	// Follow the file
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	reader := bufio.NewReader(file)

	for {
		select {
		case <-ticker.C:
			// Check if file was truncated or rotated
			info, err := os.Stat(logPath)
			if err != nil {
				// File was removed
				fmt.Println("\n--- Log file removed ---")
				return nil
			}

			// Read new content
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				fmt.Print(strings.TrimSuffix(line, "\n"), "\n")
				_ = info
			}
		}
	}
}
