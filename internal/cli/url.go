package cli

import "fmt"

// buildBaseURL assembles the ANTHROPIC_BASE_URL string. Omits the port when it
// is the scheme's default (80 for http, 443 for https) so generated URLs match
// standard web convention.
func buildBaseURL(scheme, host string, port int) string {
	switch scheme {
	case "https":
		if port == 443 {
			return fmt.Sprintf("https://%s", host)
		}
	case "http":
		if port == 80 {
			return fmt.Sprintf("http://%s", host)
		}
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// defaultSchemeFor returns the scheme implied by a deployment choice when the
// user has not set --scheme explicitly. Used by both flag and TTY paths; the
// TTY path also uses this as the pre-selected default in the scheme prompt.
func defaultSchemeFor(choice string) string {
	if choice == "domain" {
		return "https"
	}
	return "http"
}

// defaultPortFor returns the port prompt default for a given scheme. We do NOT
// use port 80 as the HTTP default — 8081 is the ccrouter server's bind default
// and what most operators will already have running.
func defaultPortFor(scheme string) int {
	if scheme == "https" {
		return 443
	}
	return 8081
}
