package pwnedpasswords

import (
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	sha1HexLength     = 40
	rangePrefixLength = 5
	maxRangeBodyBytes = 2 << 20
)

type Config struct {
	RangeEndpoint string
	HTTPClient    *http.Client
	UserAgent     string
}

type Client struct {
	rangeEndpoint *url.URL
	httpClient    *http.Client
	userAgent     string
}

type BreachedPasswordCheck struct {
	Breached    bool
	Occurrences uint64
}

func New(config Config) (*Client, error) {
	endpointText := strings.TrimSpace(config.RangeEndpoint)
	if endpointText == "" {
		return nil, fmt.Errorf("hibp pwned passwords range endpoint is required")
	}
	endpoint, err := url.Parse(endpointText)
	if err != nil {
		return nil, fmt.Errorf("parse hibp pwned passwords range endpoint: %w", err)
	}
	if endpoint.Scheme != "https" && endpoint.Scheme != "http" {
		return nil, fmt.Errorf("hibp pwned passwords range endpoint must be absolute HTTP(S)")
	}
	if endpoint.Host == "" {
		return nil, fmt.Errorf("hibp pwned passwords range endpoint host is required")
	}
	if config.HTTPClient == nil {
		return nil, fmt.Errorf("hibp pwned passwords HTTP client is required")
	}
	userAgent := strings.TrimSpace(config.UserAgent)
	if userAgent == "" {
		return nil, fmt.Errorf("hibp pwned passwords user agent is required")
	}
	return &Client{rangeEndpoint: endpoint, httpClient: config.HTTPClient, userAgent: userAgent}, nil
}

func (c *Client) CheckPassword(ctx context.Context, password string) (BreachedPasswordCheck, error) {
	if c == nil || c.httpClient == nil || c.rangeEndpoint == nil {
		return BreachedPasswordCheck{}, fmt.Errorf("hibp pwned passwords client is not initialized")
	}
	sum := sha1.Sum([]byte(password))
	sha1Hex := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix := sha1Hex[:rangePrefixLength]
	suffix := sha1Hex[rangePrefixLength:]
	endpoint := *c.rangeEndpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/range/" + prefix
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return BreachedPasswordCheck{}, fmt.Errorf("build hibp pwned passwords request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("Add-Padding", "true")
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return BreachedPasswordCheck{}, fmt.Errorf("query hibp pwned passwords range: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return BreachedPasswordCheck{}, fmt.Errorf("hibp pwned passwords range returned HTTP %d", response.StatusCode)
	}
	return parseRangeResponse(io.LimitReader(response.Body, maxRangeBodyBytes), suffix)
}

func parseRangeResponse(reader io.Reader, suffix string) (BreachedPasswordCheck, error) {
	if len(suffix) != sha1HexLength-rangePrefixLength {
		return BreachedPasswordCheck{}, fmt.Errorf("hibp pwned passwords suffix is invalid")
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		candidate, countText, ok := strings.Cut(line, ":")
		if !ok {
			return BreachedPasswordCheck{}, fmt.Errorf("hibp pwned passwords range line is invalid")
		}
		candidate = strings.ToUpper(strings.TrimSpace(candidate))
		if len(candidate) != len(suffix) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(suffix)) != 1 {
			continue
		}
		count, err := strconv.ParseUint(strings.TrimSpace(countText), 10, 64)
		if err != nil {
			return BreachedPasswordCheck{}, fmt.Errorf("hibp pwned passwords count is invalid: %w", err)
		}
		return BreachedPasswordCheck{Breached: count > 0, Occurrences: count}, nil
	}
	if err := scanner.Err(); err != nil {
		return BreachedPasswordCheck{}, fmt.Errorf("read hibp pwned passwords range: %w", err)
	}
	return BreachedPasswordCheck{}, nil
}
