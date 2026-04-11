package jobs

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

const maxGitURLLength = 2048

func validateGitCloneURLField(field, raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > maxGitURLLength {
		return fmt.Errorf("%s is too long", field)
	}
	if strings.ContainsAny(value, "\x00\r\n\t\\") {
		return fmt.Errorf("%s contains unsupported characters", field)
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", field, err)
	}
	if !parsed.IsAbs() || parsed.Opaque != "" {
		return fmt.Errorf("%s must be an absolute http or https URL", field)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s scheme %q is not supported; use http or https", field, parsed.Scheme)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	if err := validateGitURLPort(field, parsed); err != nil {
		return err
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("%s must not include query strings or fragments", field)
	}
	if strings.Trim(parsed.EscapedPath(), "/") == "" {
		return fmt.Errorf("%s must include a repository path", field)
	}

	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parsed.Hostname()), "."))
	if host == "" {
		return fmt.Errorf("%s must include a host", field)
	}
	if strings.Contains(host, "%") {
		return fmt.Errorf("%s host must not include an IPv6 zone identifier", field)
	}
	if isLocalGitHostName(host) {
		return fmt.Errorf("%s host %q is not public", field, host)
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		if err := validatePublicGitIP(field, addr); err != nil {
			return err
		}
		return nil
	}

	// URL syntax validation does not close DNS rebinding or redirect abuse; the
	// network boundary still needs egress policy for a hard SSRF guarantee.
	if !strings.Contains(host, ".") {
		return fmt.Errorf("%s host %q must be a public DNS name", field, host)
	}
	return nil
}

func isLocalGitHostName(host string) bool {
	return host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		host == "local" ||
		strings.HasSuffix(host, ".local")
}

func validateGitURLPort(field string, parsed *url.URL) error {
	port := parsed.Port()
	if strings.HasSuffix(parsed.Host, ":") && port == "" {
		return fmt.Errorf("%s host must not include an empty port", field)
	}
	if port == "" {
		return nil
	}
	if parsed.Scheme == "https" && port == "443" {
		return nil
	}
	if parsed.Scheme == "http" && port == "80" {
		return nil
	}
	return fmt.Errorf("%s port %q is not supported; use the default port for %s", field, port, parsed.Scheme)
}

func validatePublicGitIP(field string, addr netip.Addr) error {
	addr = addr.Unmap()
	if !addr.IsValid() ||
		addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() ||
		!addr.IsGlobalUnicast() {
		return fmt.Errorf("%s host IP %q is not public", field, addr.String())
	}
	return nil
}
