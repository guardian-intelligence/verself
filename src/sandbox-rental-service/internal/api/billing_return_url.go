package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

type billingReturnURLField struct {
	Name string
	URL  string
}

// ParseBillingReturnOrigins normalizes the deployment-owned origins allowed
// for Stripe checkout success/cancel redirects.
func ParseBillingReturnOrigins(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		origin, err := parseBillingReturnOrigin(part)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	if len(origins) == 0 {
		return nil, errors.New("at least one billing return origin is required")
	}
	return origins, nil
}

func parseBillingReturnOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse billing return origin %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("billing return origin %q must include scheme and host", raw)
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("billing return origin %q must include a hostname", raw)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("billing return origin %q must not include userinfo", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("billing return origin %q must not include path, query, or fragment", raw)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", fmt.Errorf("billing return origin %q must use http or https", raw)
	}
	if scheme == "http" && !isLoopbackHostname(parsed.Hostname()) {
		return "", fmt.Errorf("billing return origin %q must use https unless the host is loopback", raw)
	}
	return canonicalBillingReturnOrigin(scheme, parsed.Hostname(), parsed.Port()), nil
}

func validateBillingReturnURLs(ctx context.Context, origins []string, fields ...billingReturnURLField) error {
	if len(origins) == 0 {
		return internalFailure(ctx, "billing-return-origins-unconfigured", "billing return URL validation is not configured", errors.New("missing billing return origins"))
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}
	for _, field := range fields {
		if err := validateBillingReturnURL(ctx, allowed, field); err != nil {
			return err
		}
	}
	return nil
}

func validateBillingReturnURL(ctx context.Context, allowed map[string]struct{}, field billingReturnURLField) error {
	value := strings.TrimSpace(field.URL)
	if value == "" {
		return badRequest(ctx, "billing-return-url-required", field.Name+" is required", nil)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return badRequest(ctx, "billing-return-url-invalid", field.Name+" must be an absolute http(s) URL", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return badRequest(ctx, "billing-return-url-invalid", field.Name+" must be an absolute http(s) URL", nil)
	}
	if parsed.Hostname() == "" {
		return badRequest(ctx, "billing-return-url-invalid", field.Name+" must include a hostname", nil)
	}
	if parsed.User != nil {
		return badRequest(ctx, "billing-return-url-invalid", field.Name+" must not include userinfo", nil)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return badRequest(ctx, "billing-return-url-invalid", field.Name+" must use http or https", nil)
	}
	origin := canonicalBillingReturnOrigin(scheme, parsed.Hostname(), parsed.Port())
	if _, ok := allowed[origin]; !ok {
		return badRequest(ctx, "billing-return-origin-not-allowed", field.Name+" origin is not registered for this service", nil)
	}
	return nil
}

func canonicalBillingReturnOrigin(scheme, hostname, port string) string {
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	if isDefaultPort(scheme, port) {
		port = ""
	}
	if port != "" {
		return scheme + "://" + net.JoinHostPort(hostname, port)
	}
	if strings.Contains(hostname, ":") {
		return scheme + "://[" + hostname + "]"
	}
	return scheme + "://" + hostname
}

func isDefaultPort(scheme, port string) bool {
	return (scheme == "https" && port == "443") || (scheme == "http" && port == "80")
}

func isLoopbackHostname(hostname string) bool {
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	if hostname == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(hostname)
	return err == nil && addr.IsLoopback()
}
