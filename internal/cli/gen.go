package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/iimmutable/cc-modelrouter/internal/auth"
	"github.com/spf13/cobra"
)

// NewGenCommand creates the gen subcommand group.
func NewGenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate configuration files",
		Long:  `Generate configuration files for Claude Code integration.`,
	}
	cmd.AddCommand(NewGenSettingsCommand())
	return cmd
}

// NewGenSettingsCommand creates the gen settings command.
func NewGenSettingsCommand() *cobra.Command {
	var url, ip, user, key, output string
	var port int

	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Generate a Claude Code settings.json for the router",
		Long: `Generate a Claude Code settings.local.json or settings.json that configures
Claude Code to use the ccrouter proxy.

The output uses Claude Code's env format with attribution disabled.

By default the command prompts for deployment type (local vs public) and
detects the server's public IP when "Public" is chosen. Pass --url or --ip
to skip the prompt (scripting-friendly); in non-interactive sessions the
command defaults to localhost with no network call.

Examples:
  ccrouter gen settings --user alice
  ccrouter gen settings --key sk-ccr-abc123
  ccrouter gen settings --ip 10.0.0.5 --port 8081    # offline / scripting
  ccrouter gen settings --url http://myserver:8081 -o .claude/settings.local.json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Flags().GetInt("port")
			return runGenSettings(url, ip, port, user, key, output)
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "Full router URL (overrides prompt and detection)")
	cmd.Flags().StringVar(&ip, "ip", "", "Server IP (skips prompt and detection; offline-friendly)")
	cmd.Flags().IntVarP(&port, "port", "p", 8081, "Router port")
	cmd.Flags().StringVar(&user, "user", "", "Username to look up API key from keystore")
	cmd.Flags().StringVar(&key, "key", "", "API key directly (overrides --user)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: stdout)")

	return cmd
}

func runGenSettings(urlFlag, ipFlag string, port int, user, key, output string) error {
	// Resolve the base URL: honor --url / --ip overrides, prompt only on TTY,
	// never make a network call unless the user explicitly picks "Public".
	baseURL, err := resolveServerAddress(urlFlag, ipFlag, port, os.Stdin, os.Stdout)
	if err != nil {
		return fmt.Errorf("failed to resolve server address: %w", err)
	}

	// Resolve the API key
	apiKey := ""

	if key != "" {
		apiKey = key
	} else if user != "" {
		db, err := openUsageDB()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		ks := auth.NewKeyStore(db)
		raw, _, err := ks.GetRawKeyByUserName(user)
		if err != nil {
			return fmt.Errorf("failed to look up key for user %q: %w", user, err)
		}
		apiKey = raw
	}

	// Build settings
	env := map[string]string{
		"ANTHROPIC_BASE_URL":                       baseURL,
		"CLAUDE_CODE_ATTRIBUTION_HEADER":          "0",
		"API_TIMEOUT_MS":                          "3000000",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}
	if apiKey != "" {
		env["ANTHROPIC_AUTH_TOKEN"] = apiKey
	}

	settings := map[string]interface{}{
		"env": env,
		"attribution": map[string]string{
			"commit": "",
			"pr":     "",
		},
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if output != "" {
		if err := os.WriteFile(output, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", output, err)
		}
		fmt.Printf("Settings written to %s\n", output)
	} else {
		fmt.Println(string(data))
	}

	return nil
}
