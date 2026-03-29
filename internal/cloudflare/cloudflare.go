package cloudflare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

type Client struct {
	Token   string
	BaseURL string // default: https://api.cloudflare.com/client/v4
	HTTP    *http.Client
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) do(method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL()+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// zonesResponse is the Cloudflare /zones API response.
type zonesResponse struct {
	Success bool          `json:"success"`
	Errors  []cfError     `json:"errors"`
	Result  []zoneResult  `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type zoneResult struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Permissions []string `json:"permissions"`
}

// FetchZones calls the /zones?name=<domain> API and returns the raw response body.
func (c *Client) FetchZones(domain string) ([]byte, error) {
	return c.do("GET", fmt.Sprintf("/zones?name=%s", domain), nil)
}

type dnsRecord struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

type dnsListResponse struct {
	Success bool        `json:"success"`
	Result  []dnsRecord `json:"result"`
}

// EnsureDNSRecord creates or updates an A record for fqdn pointing to ip.
func (c *Client) EnsureDNSRecord(zoneID, fqdn, ip string, proxied bool) error {
	// Check for existing record
	data, err := c.do("GET", fmt.Sprintf("/zones/%s/dns_records?type=A&name=%s", zoneID, fqdn), nil)
	if err != nil {
		return fmt.Errorf("list DNS records: %w", err)
	}

	var existing dnsListResponse
	if err := json.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("parse DNS records: %w", err)
	}

	record := dnsRecord{
		Type:    "A",
		Name:    fqdn,
		Content: ip,
		Proxied: proxied,
		TTL:     1, // auto
	}

	if existing.Success && len(existing.Result) > 0 {
		// Update existing
		recordID := existing.Result[0].Name // use the record ID from result
		_ = recordID
		_, err = c.do("PUT", fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, existing.Result[0].Name), record)
	} else {
		// Create new
		_, err = c.do("POST", fmt.Sprintf("/zones/%s/dns_records", zoneID), record)
	}
	if err != nil {
		return fmt.Errorf("upsert DNS record %s: %w", fqdn, err)
	}
	return nil
}
