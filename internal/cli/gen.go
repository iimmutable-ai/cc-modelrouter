package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/iimmutable-ai/cc-modelrouter/internal/auth"
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
	var urlFlag, ipFlag, domainFlag, schemeFlag, user, key, output string
	var port int

	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Generate a Claude Code settings.json for the router",
		Long: `Generate a Claude Code settings.local.json or settings.json that configures
Claude Code to use the ccrouter proxy.

The output uses Claude Code's env format with attribution disabled.

When no flags are provided, the command auto-detects the server address from
~/.cc-modelrouter/config.json (if it exists and has server settings). This
makes "ccrouter gen settings" work out-of-the-box after "ccrouter setup server"
or "ccrouter config".

By default the command prompts for deployment type (local / domain / public IP)
and detects the server's public IP when "IP" is chosen. Pass --url, --domain,
or --ip to skip the prompt (scripting-friendly); in non-interactive sessions the
command defaults to localhost with no network call.

When --domain or --ip is used, the scheme is inferred (https for domains, http
for IPs). Override with --scheme. Standard web port-elision rules apply: https
on 443 and http on 80 produce URLs without an explicit port.

Examples:
  ccrouter gen settings --user alice
  ccrouter gen settings --domain api.example.com                   # HTTPS, port 443
  ccrouter gen settings --domain api.example.com --port 8443       # custom HTTPS port
  ccrouter gen settings --ip 10.0.0.5                              # HTTP, port 8081
  ccrouter gen settings --ip 10.0.0.5 --scheme https --port 8443   # HTTPS to raw IP
  ccrouter gen settings --url http://myserver:8081 -o .claude/settings.local.json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Flags().GetInt("port")
			portExplicit := cmd.Flags().Changed("port")
			return runGenSettings(urlFlag, ipFlag, domainFlag, schemeFlag, port, portExplicit, user, key, output)
		},
	}

	cmd.Flags().StringVar(&urlFlag, "url", "", "Full router URL (overrides prompt and detection)")
	cmd.Flags().StringVar(&domainFlag, "domain", "", "Server domain (e.g. api.example.com; skips prompt and detection, defaults to HTTPS/443)")
	cmd.Flags().StringVar(&ipFlag, "ip", "", "Server IP (skips prompt and detection; offline-friendly, defaults to HTTP/8081)")
	cmd.Flags().StringVar(&schemeFlag, "scheme", "", "URL scheme: http or https (default: inferred from --domain/--ip)")
	cmd.Flags().IntVarP(&port, "port", "p", 8081, "Router port")
	cmd.Flags().StringVar(&user, "user", "", "Username to look up API key from keystore")
	cmd.Flags().StringVar(&key, "key", "", "API key directly (overrides --user)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: stdout)")

	return cmd
}

func runGenSettings(urlFlag, ipFlag, domainFlag, schemeFlag string, port int, portExplicit bool, user, key, output string) error {
	if err := validateSchemeFlag(schemeFlag); err != nil {
		return err
	}

	// Resolve the base URL: honor --url / --domain / --ip overrides, prompt only
	// on TTY, never make a network call unless the user explicitly picks "IP".
	baseURL, err := resolveServerAddress(urlFlag, ipFlag, domainFlag, schemeFlag, port, portExplicit, os.Stdin, os.Stdout)
	if err != nil {
		return fmt.Errorf("failed to resolve server address: %w", err)
	}

	// Warn when the generated settings target a non-local address. The HTTP path
	// keeps the stern "server must bind 0.0.0.0" warning; HTTPS softens it since
	// the user has clearly set up TLS deliberately. Emitted to stderr so stdout
	// JSON piping is unaffected.
	if !isLocalHostURL(baseURL) {
		emitNonLocalWarning(baseURL)
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

// validateSchemeFlag returns an error if --scheme was set to anything other
// than http or https. Empty string (user did not pass the flag) is valid.
func validateSchemeFlag(scheme string) error {
	switch scheme {
	case "", "http", "https":
		return nil
	default:
		return fmt.Errorf("--scheme must be \"http\" or \"https\", got %q", scheme)
	}
}

// emitNonLocalWarning prints deployment guidance for non-local target URLs.
// HTTPS targets get a softer note (the user has TLS set up); HTTP targets get
// the stern "bind 0.0.0.0" warning since plaintext-to-a-remote-host is rarely
// intentional in production.
func emitNonLocalWarning(baseURL string) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return
	}
	switch u.Scheme {
	case "https":
		fmt.Fprintf(os.Stderr, "NOTE: Generated settings target a remote HTTPS server (%s).\n", baseURL)
		fmt.Fprintln(os.Stderr, `  Ensure the server is running with --tls-domain or --tls-cert/--tls-key,
  and that ports 443 (and 80 for ACME/redirect) are open in the cloud security group.`)
	default:
		fmt.Fprintf(os.Stderr, "WARNING: Generated settings target a non-local address (%s).\n", baseURL)
		fmt.Fprintln(os.Stderr, `  For the client to connect, the ccrouter server MUST bind to 0.0.0.0:
    ccrouter start --host 0.0.0.0 --port 8081
  or set "server": {"host": "0.0.0.0"} in ~/.cc-modelrouter/config.json on the server.
  Also allow inbound TCP on the port in the cloud security group and any in-VM firewall.`)
	}
}
