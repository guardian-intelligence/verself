package objectstorage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

type GarageAdminClient struct {
	baseURLs   []*url.URL
	adminToken string
	httpClient *http.Client
	next       atomic.Uint32
}

func NewGarageAdminClient(baseURLs []string, adminToken string, httpClient *http.Client) (*GarageAdminClient, error) {
	parsed, err := parseGarageURLs(baseURLs, "admin")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(adminToken) == "" {
		return nil, fmt.Errorf("garage admin token is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &GarageAdminClient{
		baseURLs:   parsed,
		adminToken: strings.TrimSpace(adminToken),
		httpClient: httpClient,
	}, nil
}

func (c *GarageAdminClient) Health(ctx context.Context) error {
	return c.forEachEndpoint(ctx, func(baseURL *url.URL) error {
		req, err := c.newRequest(ctx, baseURL, http.MethodGet, "/health", nil)
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("garage health: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			return &garageRequestError{
				Method:     req.Method,
				Path:       req.URL.Path,
				StatusCode: resp.StatusCode,
				Body:       strings.TrimSpace(string(body)),
			}
		}
		return nil
	})
}

func (c *GarageAdminClient) CreateBucket(ctx context.Context, globalAlias string) (GarageBucket, error) {
	body := map[string]any{
		"globalAlias": globalAlias,
	}
	var response garageBucketInfoResponse
	if err := c.doJSONWithFailover(ctx, http.MethodPost, "/v2/CreateBucket", body, &response); err != nil {
		return GarageBucket{}, err
	}
	return response.toGarageBucket(), nil
}

func (c *GarageAdminClient) UpdateBucket(ctx context.Context, bucketID string, quotas GarageQuotas, lifecycleJSON []byte) (GarageBucket, error) {
	payload := map[string]any{
		"quotas": quotas,
	}
	if len(lifecycleJSON) > 0 {
		var lifecycle any
		if err := json.Unmarshal(lifecycleJSON, &lifecycle); err != nil {
			return GarageBucket{}, fmt.Errorf("decode lifecycle rules: %w", err)
		}
		payload["lifecycleRules"] = lifecycle
	}
	path := "/v2/UpdateBucket?id=" + url.QueryEscape(bucketID)
	var response garageBucketInfoResponse
	if err := c.doJSONWithFailover(ctx, http.MethodPost, path, payload, &response); err != nil {
		return GarageBucket{}, err
	}
	return response.toGarageBucket(), nil
}

func (c *GarageAdminClient) GetBucket(ctx context.Context, bucketID string) (GarageBucket, error) {
	var response garageBucketInfoResponse
	if err := c.doJSONWithFailover(ctx, http.MethodGet, "/v2/GetBucketInfo?id="+url.QueryEscape(bucketID), nil, &response); err != nil {
		return GarageBucket{}, err
	}
	return response.toGarageBucket(), nil
}

func (c *GarageAdminClient) DeleteBucket(ctx context.Context, bucketID string) error {
	return c.doJSONWithFailover(ctx, http.MethodPost, "/v2/DeleteBucket?id="+url.QueryEscape(bucketID), nil, nil)
}

func (c *GarageAdminClient) AllowBucketKey(ctx context.Context, bucketID, accessKeyID string, perms GarageBucketPermissions) error {
	return c.doJSONWithFailover(ctx, http.MethodPost, "/v2/AllowBucketKey", map[string]any{
		"bucketId":    bucketID,
		"accessKeyId": accessKeyID,
		"permissions": perms,
	}, nil)
}

func (c *GarageAdminClient) doJSONWithFailover(ctx context.Context, method, path string, body any, out any) error {
	return c.forEachEndpoint(ctx, func(baseURL *url.URL) error {
		req, err := c.newRequest(ctx, baseURL, method, path, body)
		if err != nil {
			return err
		}
		return c.doJSON(req, out)
	})
}

func (c *GarageAdminClient) forEachEndpoint(ctx context.Context, fn func(baseURL *url.URL) error) error {
	if c == nil || len(c.baseURLs) == 0 {
		return fmt.Errorf("garage admin endpoints are not configured")
	}
	start := int(c.next.Add(1)-1) % len(c.baseURLs)
	var lastErr error
	for offset := 0; offset < len(c.baseURLs); offset++ {
		index := (start + offset) % len(c.baseURLs)
		if err := fn(c.baseURLs[index]); err != nil {
			lastErr = err
			if !retryableGarageError(err) {
				return err
			}
			continue
		}
		c.next.Store(uint32FromIndex((index+1)%len(c.baseURLs), "garage endpoint index"))
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("garage admin endpoints are not configured")
	}
	return lastErr
}

func (c *GarageAdminClient) newRequest(ctx context.Context, baseURL *url.URL, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal garage request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	// url.URL treats '?' inside Path as literal data and escapes it; split the
	// relative path first so Garage receives the intended endpoint + query.
	relativeURL, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse garage request path: %w", err)
	}
	u := *baseURL
	u.Path = strings.TrimRight(u.Path, "/") + relativeURL.Path
	u.RawPath = strings.TrimRight(u.EscapedPath(), "/") + relativeURL.EscapedPath()
	u.RawQuery = relativeURL.RawQuery
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("build garage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *GarageAdminClient) doJSON(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("garage request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return &garageRequestError{
			Method:     req.Method,
			Path:       req.URL.RequestURI(),
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(body)),
		}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode garage response: %w", err)
	}
	return nil
}

type garageRequestError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *garageRequestError) Error() string {
	if e == nil {
		return "garage request failed"
	}
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("garage request %s %s status %d", e.Method, e.Path, e.StatusCode)
	}
	return fmt.Sprintf("garage request %s %s status %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

func retryableGarageError(err error) bool {
	var requestErr *garageRequestError
	if errors.As(err, &requestErr) {
		return requestErr.StatusCode >= http.StatusInternalServerError
	}
	return true
}

func parseGarageURLs(raw []string, kind string) ([]*url.URL, error) {
	urls := make([]*url.URL, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parsed, err := url.Parse(item)
		if err != nil {
			return nil, fmt.Errorf("parse garage %s url: %w", kind, err)
		}
		urls = append(urls, parsed)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("at least one garage %s url is required", kind)
	}
	return urls, nil
}

type garageBucketInfoResponse struct {
	ID             string          `json:"id"`
	GlobalAliases  []string        `json:"globalAliases"`
	LifecycleRules json.RawMessage `json:"lifecycleRules"`
	Quotas         struct {
		MaxSize    *int64 `json:"maxSize"`
		MaxObjects *int64 `json:"maxObjects"`
	} `json:"quotas"`
}

func (r garageBucketInfoResponse) toGarageBucket() GarageBucket {
	return GarageBucket{
		ID:             r.ID,
		GlobalAliases:  append([]string(nil), r.GlobalAliases...),
		QuotaBytes:     r.Quotas.MaxSize,
		QuotaObjects:   r.Quotas.MaxObjects,
		LifecycleRules: r.LifecycleRules,
	}
}
