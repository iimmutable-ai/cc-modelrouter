package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

// Test seams — override in tests to control detection / TTY detection.
var (
	detectPublicIPFn = detectPublicIP
	isTTYFn          = func() bool { return isatty.IsTerminal(os.Stdin.Fd()) }
)

// detectPublicIP queries an external echo service to learn the server's
// public-facing IPv4 address. It is ONLY called after the user explicitly
// selects the "Public" deployment type — never speculatively.
//
// 3s hard timeout. Returns ("", err) on timeout, non-200, or network failure.
//
// SECURITY: The detected IP is only written to stdout (prompting line) and to
// the generated settings file (both intentional user-facing outputs). It is
// never written to instance log files.
func detectPublicIP(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	const endpoint = "https://api.ipify.org"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ipify returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("empty response from ipify")
	}
	return ip, nil
}

// resolveServerAddress applies the flag precedence table and returns the final
// base URL (e.g. "https://api.example.com", "http://10.0.0.5:8081", or
// "http://localhost:8081").
//
// Precedence (first match wins):
//  1. urlFlag set     → use verbatim (no prompt, no detection)
//  2. domainFlag set  → scheme = schemeFlag or defaultSchemeFor("domain") (https);
//                        port = portFlag (if portExplicit) or defaultPortFor(scheme)
//  3. ipFlag set      → scheme = schemeFlag or defaultSchemeFor("ip") (http);
//                        port = portFlag (if portExplicit) or defaultPortFor(scheme)
//  4. stdin not a TTY → http://localhost:<port>, no network call
//  5. stdin is a TTY  → 3-option prompt: Local / Domain / IP
//     - Local  → localhost, http, no scheme prompt
//     - Domain → scheme prompt (default https), FQDN prompt
//     - IP     → scheme prompt (default http), detectPublicIP + confirm
//     Then a port prompt whose default depends on the resolved scheme.
//
// portFlag is the value of the --port flag; portExplicit is whether the user
// actually passed --port (from cmd.Flags().Changed("port")). When portExplicit
// is false, the default port for the resolved scheme is used instead.
//
// Detection failures are never fatal. The only error path is prompt I/O failure.
func resolveServerAddress(urlFlag, ipFlag, domainFlag, schemeFlag string, portFlag int, portExplicit bool, in io.Reader, out io.Writer) (string, error) {
	// 1. --url overrides everything.
	if urlFlag != "" {
		return urlFlag, nil
	}

	// 2. --domain skips prompt and detection.
	if domainFlag != "" {
		scheme := schemeFlag
		if scheme == "" {
			scheme = defaultSchemeFor("domain")
		}
		port := defaultPortFor(scheme)
		if portExplicit {
			port = portFlag
		}
		return buildBaseURL(scheme, stripSchemePrefix(domainFlag), port), nil
	}

	// 3. --ip skips prompt and detection.
	if ipFlag != "" {
		scheme := schemeFlag
		if scheme == "" {
			scheme = defaultSchemeFor("ip")
		}
		port := defaultPortFor(scheme)
		if portExplicit {
			port = portFlag
		}
		return buildBaseURL(scheme, ipFlag, port), nil
	}

	// 4. Non-TTY: default to localhost, no network call, one-line summary.
	if !isTTYFn() {
		fmt.Fprintf(out, "Using server IP: localhost (local)\n")
		port := 8081
		if portExplicit {
			port = portFlag
		}
		return buildBaseURL("http", "localhost", port), nil
	}

	// 5. TTY: prompt deployment type.
	reader := bufio.NewReader(in)

	choice, err := promptDeploymentType(reader, out)
	if err != nil {
		return "", err
	}

	scheme := defaultSchemeFor(choice)
	if choice != "local" {
		// Pre-select the inferred scheme in the menu.
		defaultChoice := "1" // HTTPS
		if scheme == "http" {
			defaultChoice = "2"
		}
		scheme, err = promptScheme(reader, out, defaultChoice)
		if err != nil {
			return "", err
		}
	}

	var host string
	switch choice {
	case "local":
		host = "localhost"
	case "domain":
		host, err = promptDomain(reader, out)
		if err != nil {
			return "", err
		}
	case "ip":
		host, err = promptPublicIP(reader, out)
		if err != nil {
			return "", err
		}
	}

	portDefault := defaultPortFor(scheme)
	if portExplicit {
		portDefault = portFlag
	}
	finalPort, err := promptPort(reader, out, portDefault)
	if err != nil {
		return "", err
	}

	return buildBaseURL(scheme, host, finalPort), nil
}

// promptDeploymentType asks the user to pick local / domain / ip and returns
// the canonical choice token.
func promptDeploymentType(reader *bufio.Reader, out io.Writer) (string, error) {
	for {
		fmt.Fprintln(out, "Deployment type:")
		fmt.Fprintln(out, "  1) Local    (localhost — same machine only)")
		fmt.Fprintln(out, "  2) Domain   (e.g. api.example.com)")
		fmt.Fprintln(out, "  3) IP       (remote server — detect public IP)")
		fmt.Fprint(out, "Select [1]: ")

		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("failed to read deployment choice: %w", err)
		}
		choice := strings.TrimSpace(line)

		switch choice {
		case "", "1":
			return "local", nil
		case "2":
			return "domain", nil
		case "3":
			return "ip", nil
		default:
			fmt.Fprintln(out, "Invalid choice; please enter 1, 2, or 3.")
		}
	}
}

// promptScheme asks for http vs https. defaultChoice ("1" or "2") is the
// pre-selected option shown in the prompt — Domain defaults to HTTPS, IP to HTTP.
func promptScheme(reader *bufio.Reader, out io.Writer, defaultChoice string) (string, error) {
	for {
		fmt.Fprintln(out, "Scheme:")
		fmt.Fprintln(out, "  1) HTTPS")
		fmt.Fprintln(out, "  2) HTTP")
		fmt.Fprintf(out, "Select [%s]: ", defaultChoice)

		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("failed to read scheme choice: %w", err)
		}
		choice := strings.TrimSpace(line)

		switch choice {
		case "":
			// Empty input → accept the shown default.
			if defaultChoice == "2" {
				return "http", nil
			}
			return "https", nil
		case "1":
			return "https", nil
		case "2":
			return "http", nil
		default:
			fmt.Fprintln(out, "Invalid choice; please enter 1 or 2.")
		}
	}
}

// promptDomain asks for the FQDN. Strips any accidental scheme prefix the user
// might have typed and validates non-empty.
func promptDomain(reader *bufio.Reader, out io.Writer) (string, error) {
	for {
		fmt.Fprint(out, "Domain name (e.g. api.example.com): ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("failed to read domain name: %w", err)
		}
		domain := stripSchemePrefix(strings.TrimSpace(line))
		if domain == "" {
			fmt.Fprintln(out, "Domain cannot be empty; please try again.")
			continue
		}
		return domain, nil
	}
}

// stripSchemePrefix removes a leading http:// or https:// (case-insensitive)
// from a host string the user might have typed. Returns the input unchanged if
// no prefix is present.
func stripSchemePrefix(host string) string {
	lower := strings.ToLower(host)
	switch {
	case strings.HasPrefix(lower, "https://"):
		return host[len("https://"):]
	case strings.HasPrefix(lower, "http://"):
		return host[len("http://"):]
	}
	return host
}

// promptPublicIP detects the public IP, then offers a confirm/override loop.
// Falls back to "localhost" if detection fails (informing the user).
func promptPublicIP(reader *bufio.Reader, out io.Writer) (string, error) {
	ip, err := detectPublicIPFn(context.Background())
	if err != nil {
		fmt.Fprintf(out, "Could not detect public IP (%v); falling back to localhost.\n", err)
		fmt.Fprintln(out, "Re-run with --ip <address> if you know the server IP.")
		return "localhost", nil
	}

	fmt.Fprintf(out, "Detected server IP: %s\n", ip)

	for {
		fmt.Fprint(out, "Use this IP? [Y/n]: ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("failed to read confirm: %w", err)
		}
		answer := strings.ToLower(strings.TrimSpace(line))

		switch answer {
		case "", "y", "yes":
			return ip, nil
		case "n", "no":
			fmt.Fprint(out, "Enter server IP: ")
			ovr, err := reader.ReadString('\n')
			if err != nil && ovr == "" {
				return "", fmt.Errorf("failed to read IP override: %w", err)
			}
			override := strings.TrimSpace(ovr)
			if override == "" {
				fmt.Fprintln(out, "Empty override; using detected IP.")
				return ip, nil
			}
			return override, nil
		default:
			fmt.Fprintln(out, "Please answer y or n.")
		}
	}
}

// promptPort asks for the port, defaulting to the provided value on empty input.
func promptPort(reader *bufio.Reader, out io.Writer, defaultPort int) (int, error) {
	fmt.Fprintf(out, "Port [%d]: ", defaultPort)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return 0, fmt.Errorf("failed to read port: %w", err)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return defaultPort, nil
	}

	port, err := parseIntPort(trimmed)
	if err != nil {
		fmt.Fprintf(out, "Invalid port %q; using default %d.\n", trimmed, defaultPort)
		return defaultPort, nil
	}
	return port, nil
}

// parseIntPort validates a port string in the legal range.
func parseIntPort(s string) (int, error) {
	var port int
	if _, err := fmt.Sscanf(s, "%d", &port); err != nil {
		return 0, err
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port out of range: %d", port)
	}
	return port, nil
}

// isLocalHostURL reports whether rawURL's host is a loopback address.
// It is used to decide whether gen settings should emit the non-local
// deployment warning (server must bind 0.0.0.0, firewalls must allow the port).
func isLocalHostURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
