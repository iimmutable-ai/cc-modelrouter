package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/iimmutable-ai/cc-modelrouter/internal/logging"
)

// propagateSettingsToWorktrees writes settings.local.json and profile.md
// into every existing worktree's .claude/ directory under the project root.
func propagateSettingsToWorktrees(projectRoot, actualAddr string) {
	worktreesDir := filepath.Join(projectRoot, ".claude", "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return // no worktrees dir yet — fine
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		claudeDir := filepath.Join(worktreesDir, entry.Name(), ".claude")
		writeSettingsToDir(claudeDir, actualAddr)
		writeProfileCmdToDir(claudeDir)
		logging.Infof("[WORKTREE] Propagated settings to %s", claudeDir)
	}
}

// writeSettingsToDir writes a settings.local.json with the proxy URL
// into the given .claude directory, preserving any existing settings.
func writeSettingsToDir(claudeDir, actualAddr string) {
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create dir %s: %v\n", claudeDir, err)
		return
	}

	settingsPath := filepath.Join(claudeDir, "settings.local.json")

	settings := map[string]interface{}{}
	if existing, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(existing, &settings)
	}
	if settings["env"] == nil {
		settings["env"] = map[string]interface{}{}
	}
	envMap := settings["env"].(map[string]interface{})
	envMap["ANTHROPIC_BASE_URL"] = fmt.Sprintf("http://%s", actualAddr)

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to marshal settings for %s: %v\n", claudeDir, err)
		return
	}
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write %s: %v\n", settingsPath, err)
	}
}

// writeProfileCmdToDir writes the profile.md slash command into the
// given .claude directory's commands/ subdirectory.
func writeProfileCmdToDir(claudeDir string) {
	commandsDir := filepath.Join(claudeDir, "commands")
	profileCmdPath := filepath.Join(commandsDir, "profile.md")

	if err := createProfileSlashCommand(profileCmdPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write profile command to %s: %v\n", profileCmdPath, err)
	}
}

// startWorktreeWatcher watches .claude/worktrees/ for new directories
// and propagates settings into them as they appear.
// Returns a stop function the caller should defer.
func startWorktreeWatcher(ctx context.Context, projectRoot, actualAddr string) (func(), error) {
	worktreesDir := filepath.Join(projectRoot, ".claude", "worktrees")

	// Ensure the directory exists so fsnotify can watch it
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktrees dir: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	if err := watcher.Add(worktreesDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch %s: %w", worktreesDir, err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// New directory created in worktrees/
				if event.Has(fsnotify.Create) {
					info, err := os.Stat(event.Name)
					if err != nil || !info.IsDir() {
						continue
					}
					claudeDir := filepath.Join(event.Name, ".claude")
					writeSettingsToDir(claudeDir, actualAddr)
					writeProfileCmdToDir(claudeDir)
					logging.Infof("[WORKTREE] New worktree detected, propagated settings to %s", claudeDir)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logging.Warnf("[WORKTREE] Watcher error: %v", err)
			}
		}
	}()

	return func() {
		watcher.Close()
	}, nil
}

// cleanupWorktreeSettings removes settings.local.json from all worktree
// .claude/ directories, but only if the ANTHROPIC_BASE_URL still points
// to this instance's proxy (multi-instance safety).
func cleanupWorktreeSettings(projectRoot, actualAddr string) {
	worktreesDir := filepath.Join(projectRoot, ".claude", "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return
	}

	proxyURL := fmt.Sprintf("http://%s", actualAddr)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		settingsPath := filepath.Join(worktreesDir, entry.Name(), ".claude", "settings.local.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			continue
		}
		var s map[string]interface{}
		if json.Unmarshal(data, &s) != nil {
			continue
		}
		if env, ok := s["env"].(map[string]interface{}); ok {
			if url, ok := env["ANTHROPIC_BASE_URL"].(string); ok && url == proxyURL {
				os.Remove(settingsPath)
				logging.Infof("[WORKTREE] Cleaned up %s", settingsPath)
			}
		}
	}
}
