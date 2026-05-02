package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// cfDNSRecord is a partial subset of the Cloudflare DNS record schema —
// just the fields the reconciler reads. See
// https://developers.cloudflare.com/api/operations/dns-records-for-a-zone-list-dns-records.
type cfDNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type cloudflareClient struct {
	token string
	http  *http.Client
}

func newCloudflareClient(token string) *cloudflareClient {
	return &cloudflareClient{
		token: token,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

// cfEnvelope wraps every Cloudflare v4 response. The errors array is
// non-empty only when success=false. We still surface success=true
// responses with a non-zero error count, which Cloudflare uses for warnings.
type cfEnvelope[T any] struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result T `json:"result"`
}

func (c *cloudflareClient) do(ctx context.Context, method, path string, body any, out any) error {
	var br io.Reader
	if body != nil {
		br = bytes.NewReader(mustJSON(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, cloudflareAPIBase+path, br)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("cloudflare %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode cloudflare %s %s response: %w", method, path, err)
		}
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("cloudflare %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
	}
	return nil
}

// zonesByName resolves zone IDs for a set of zone names with one list call.
// Cloudflare's /zones endpoint supports name=<comma-separated> filters but the
// fan-out per-name keeps the call simple and tolerant of >50-zone accounts.
func (c *cloudflareClient) zonesByName(ctx context.Context, names []string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range names {
		var env cfEnvelope[[]struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}]
		q := url.Values{"name": []string{name}}
		if err := c.do(ctx, http.MethodGet, "/zones"+encodeQuery(q), nil, &env); err != nil {
			return nil, err
		}
		if !env.Success {
			return nil, fmt.Errorf("cloudflare /zones?name=%s: %v", name, env.Errors)
		}
		if len(env.Result) == 0 {
			return nil, fmt.Errorf("cloudflare zone %q not found on this token's account", name)
		}
		out[name] = env.Result[0].ID
	}
	return out, nil
}

// listARecords pages through every A record for a zone.
func (c *cloudflareClient) listARecords(ctx context.Context, zoneID string) ([]cfDNSRecord, error) {
	var all []cfDNSRecord
	page := 1
	for {
		q := url.Values{
			"type":     []string{"A"},
			"page":     []string{strconv.Itoa(page)},
			"per_page": []string{"100"},
		}
		var env struct {
			Success bool `json:"success"`
			Errors  []struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
			Result     []cfDNSRecord `json:"result"`
			ResultInfo struct {
				Page       int `json:"page"`
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if err := c.do(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records"+encodeQuery(q), nil, &env); err != nil {
			return nil, err
		}
		if !env.Success {
			return nil, fmt.Errorf("cloudflare list dns_records zone=%s: %v", zoneID, env.Errors)
		}
		all = append(all, env.Result...)
		if env.ResultInfo.TotalPages <= env.ResultInfo.Page {
			break
		}
		page++
	}
	return all, nil
}

type cfWriteBody struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

func (c *cloudflareClient) createARecord(ctx context.Context, zoneID string, want desiredRecord) error {
	body := cfWriteBody{
		Type:    "A",
		Name:    want.fqdn,
		Content: want.targetIP,
		TTL:     want.ttl,
		Proxied: want.proxied,
	}
	var env cfEnvelope[cfDNSRecord]
	if err := c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", body, &env); err != nil {
		return err
	}
	if !env.Success {
		return fmt.Errorf("cloudflare create dns_records: %v", env.Errors)
	}
	return nil
}

func (c *cloudflareClient) updateARecord(ctx context.Context, zoneID, recordID string, want desiredRecord) error {
	body := cfWriteBody{
		Type:    "A",
		Name:    want.fqdn,
		Content: want.targetIP,
		TTL:     want.ttl,
		Proxied: want.proxied,
	}
	var env cfEnvelope[cfDNSRecord]
	if err := c.do(ctx, http.MethodPut, "/zones/"+zoneID+"/dns_records/"+recordID, body, &env); err != nil {
		return err
	}
	if !env.Success {
		return fmt.Errorf("cloudflare update dns_records: %v", env.Errors)
	}
	return nil
}
