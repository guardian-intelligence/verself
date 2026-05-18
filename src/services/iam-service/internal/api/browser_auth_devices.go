package api

import (
	"context"
	"strings"
)

type browserDevice struct {
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	BrowserName string `json:"browserName"`
	OSName      string `json:"osName"`
}

type browserLocation struct {
	CountryCode string `json:"countryCode"`
	Region      string `json:"region"`
	City        string `json:"city"`
}

type browserSessionObservation struct {
	ClientIP        string
	ClientIPTrusted bool
	ClientIPSource  string
	EdgePeerIP      string
	UserAgent       string
	Device          browserDevice
	Location        browserLocation
}

func authMethodsFromClaims(claims map[string]any) []string {
	if claims == nil {
		return []string{}
	}
	switch value := claims["amr"].(type) {
	case string:
		return compactUniqueStrings([]string{value})
	case []string:
		return compactUniqueStrings(value)
	case []any:
		values := make([]string, 0, len(value))
		for _, entry := range value {
			if text, ok := entry.(string); ok {
				values = append(values, text)
			}
		}
		return compactUniqueStrings(values)
	default:
		return []string{}
	}
}

func browserSessionObservationFromContext(ctx context.Context) browserSessionObservation {
	info := browserRequestInfoFromContext(ctx)
	return browserSessionObservation{
		ClientIP:        info.Attribution.ClientIP,
		ClientIPTrusted: info.Attribution.Trusted,
		ClientIPSource:  info.Attribution.ClientIPSource,
		EdgePeerIP:      info.Attribution.EdgePeerIP,
		UserAgent:       info.UserAgent,
		Device:          parseBrowserDevice(info.UserAgent),
		Location: browserLocation{
			CountryCode: info.Attribution.ClientCountry,
			Region:      info.Attribution.ClientRegion,
			City:        info.Attribution.ClientCity,
		},
	}
}

func parseBrowserDevice(userAgent string) browserDevice {
	userAgent = strings.TrimSpace(userAgent)
	browser := browserName(userAgent)
	os := operatingSystemName(userAgent)
	kind := deviceKind(userAgent)
	label := "Unknown device"
	if browser != "" && os != "" {
		label = browser + " on " + os
	} else if browser != "" {
		label = browser
	} else if os != "" {
		label = os
	}
	return browserDevice{
		Label:       label,
		Kind:        firstNonEmpty(kind, "unknown"),
		BrowserName: firstNonEmpty(browser, "Unknown browser"),
		OSName:      firstNonEmpty(os, "Unknown OS"),
	}
}

func browserName(userAgent string) string {
	lower := strings.ToLower(userAgent)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "edg/"):
		return "Edge"
	case strings.Contains(lower, "firefox/"):
		return "Firefox"
	case strings.Contains(lower, "headlesschrome/"):
		return "Headless Chrome"
	case strings.Contains(lower, "chrome/") || strings.Contains(lower, "crios/"):
		return "Chrome"
	case strings.Contains(lower, "safari/"):
		return "Safari"
	case strings.Contains(lower, "curl/"):
		return "curl"
	default:
		return ""
	}
}

func operatingSystemName(userAgent string) string {
	lower := strings.ToLower(userAgent)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "cpu os"):
		return "iOS"
	case strings.Contains(lower, "android"):
		return "Android"
	case strings.Contains(lower, "mac os x") || strings.Contains(lower, "macintosh"):
		return "macOS"
	case strings.Contains(lower, "windows"):
		return "Windows"
	case strings.Contains(lower, "linux"):
		return "Linux"
	default:
		return ""
	}
}

func deviceKind(userAgent string) string {
	lower := strings.ToLower(userAgent)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "ipad") || strings.Contains(lower, "tablet"):
		return "tablet"
	case strings.Contains(lower, "mobile") || strings.Contains(lower, "iphone") || strings.Contains(lower, "android"):
		return "mobile"
	default:
		return "desktop"
	}
}
