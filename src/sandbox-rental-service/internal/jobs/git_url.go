package jobs

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	maxGitURLLength         = 2048
	gitURLResolutionTimeout = 2 * time.Second
)

type gitDNSResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func validateGitCloneURLField(field, raw string) error {
	return validateGitCloneURLFieldWithResolver(context.Background(), net.DefaultResolver, field, raw)
}

func validateGitCloneURLFieldWithResolver(ctx context.Context, resolver gitDNSResolver, field, raw string) error {
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
		return fmt.Errorf("%s must be an absolute https URL", field)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%s scheme %q is not supported; use https", field, parsed.Scheme)
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
	if err := validateGitDNSResolution(ctx, resolver, field, host); err != nil {
		return err
	}
	return nil
}

func validateGitDNSResolution(ctx context.Context, resolver gitDNSResolver, field, host string) error {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	resolveCtx, cancel := context.WithTimeout(ctx, gitURLResolutionTimeout)
	defer cancel()

	answers, err := resolver.LookupIPAddr(resolveCtx, host)
	if err != nil {
		return fmt.Errorf("%s host %q DNS resolution failed: %w", field, host, err)
	}
	if len(answers) == 0 {
		return fmt.Errorf("%s host %q DNS resolution returned no addresses", field, host)
	}
	for _, answer := range answers {
		addr, ok := netip.AddrFromSlice(answer.IP)
		if !ok {
			return fmt.Errorf("%s host %q resolved to invalid IP %q", field, host, answer.IP.String())
		}
		if err := validatePublicGitIP(field, addr); err != nil {
			return fmt.Errorf("%s host %q resolved to non-public IP %q", field, host, addr.Unmap().String())
		}
	}
	return nil
}

func providerHostFromCloneURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parsed.Hostname()), "."))
}

func ProviderHostFromCloneURL(raw string) string {
	return providerHostFromCloneURL(raw)
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
