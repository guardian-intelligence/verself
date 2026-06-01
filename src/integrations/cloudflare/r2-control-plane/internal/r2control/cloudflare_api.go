package r2control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	PermissionR2BucketItemRead  = "Workers R2 Storage Bucket Item Read"
	PermissionR2BucketItemWrite = "Workers R2 Storage Bucket Item Write"
	PermissionR2StorageWrite    = "Workers R2 Storage Write"
	CloudflareAPIBase           = "https://api.cloudflare.com/client/v4"
)

var cloudflareAPIBase = CloudflareAPIBase

type CloudflareAPIClient struct {
	apiBase string
	token   string
	http    *http.Client
}

type CreatedAPIToken struct {
	ID              string
	Value           string
	S3AccessKeyID   string
	S3SecretKey     string
	Name            string
	PermissionGroup string
	Bucket          string
}

type permissionGroup struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type tokenPolicy struct {
	Effect           string              `json:"effect"`
	PermissionGroups []map[string]string `json:"permission_groups"`
	Resources        map[string]any      `json:"resources"`
}

type tokenCreateResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID       string        `json:"id"`
		Name     string        `json:"name"`
		Value    string        `json:"value"`
		Policies []tokenPolicy `json:"policies"`
	} `json:"result"`
	Errors []cloudflareMessage `json:"errors"`
}

type temporaryCredentialResponse struct {
	Success bool `json:"success"`
	Result  struct {
		AccessKeyID     string `json:"accessKeyId"`
		SecretAccessKey string `json:"secretAccessKey"`
		SessionToken    string `json:"sessionToken"`
	} `json:"result"`
	Errors []cloudflareMessage `json:"errors"`
}

type tokenVerifyResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"result"`
	Errors []cloudflareMessage `json:"errors"`
}

type TokenVerification struct {
	ID     string
	Status string
}

type permissionGroupsResponse struct {
	Success bool                `json:"success"`
	Result  []permissionGroup   `json:"result"`
	Errors  []cloudflareMessage `json:"errors"`
}

type cloudflareMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewCloudflareAPIClient(apiToken string, timeout time.Duration) (*CloudflareAPIClient, error) {
	if strings.TrimSpace(apiToken) == "" {
		return nil, fmt.Errorf("Cloudflare API token value is required")
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &CloudflareAPIClient{
		apiBase: cloudflareAPIBase,
		token:   strings.TrimSpace(apiToken),
		http:    &http.Client{Timeout: timeout},
	}, nil
}

func (c *CloudflareAPIClient) VerifyAccountToken(ctx context.Context, accountID string) (TokenVerification, error) {
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	if !IsCloudflareAccountID(accountID) {
		return TokenVerification{}, fmt.Errorf("account ID must be a 32-character lowercase hex Cloudflare account ID")
	}
	var response tokenVerifyResponse
	if err := c.doJSON(ctx, http.MethodGet, "/accounts/"+url.PathEscape(accountID)+"/tokens/verify", nil, &response); err != nil {
		return TokenVerification{}, err
	}
	if !response.Success {
		return TokenVerification{}, fmt.Errorf("Cloudflare token verify failed: %s", cloudflareErrors(response.Errors))
	}
	if response.Result.ID == "" {
		return TokenVerification{}, fmt.Errorf("Cloudflare token verify returned no token ID")
	}
	return TokenVerification{
		ID:     response.Result.ID,
		Status: response.Result.Status,
	}, nil
}

func (c *CloudflareAPIClient) CreateR2BucketToken(ctx context.Context, accountID, bucket, name, permissionName string) (CreatedAPIToken, error) {
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	bucket = strings.TrimSpace(bucket)
	name = strings.TrimSpace(name)
	permissionName = strings.TrimSpace(permissionName)
	if !IsCloudflareAccountID(accountID) {
		return CreatedAPIToken{}, fmt.Errorf("account ID must be a 32-character lowercase hex Cloudflare account ID")
	}
	if !IsR2BucketName(bucket) {
		return CreatedAPIToken{}, fmt.Errorf("bucket must be a valid lowercase R2 bucket name")
	}
	if name == "" {
		return CreatedAPIToken{}, fmt.Errorf("token name is required")
	}
	if permissionName == "" {
		return CreatedAPIToken{}, fmt.Errorf("permission group name is required")
	}
	group, err := c.findPermissionGroup(ctx, accountID, permissionName, "com.cloudflare.edge.r2.bucket")
	if err != nil {
		return CreatedAPIToken{}, err
	}
	body := map[string]any{
		"name": name,
		"policies": []tokenPolicy{{
			Effect: "allow",
			PermissionGroups: []map[string]string{{
				"id": group.ID,
			}},
			Resources: map[string]any{
				r2BucketResource(accountID, bucket): "*",
			},
		}},
	}
	var response tokenCreateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/accounts/"+url.PathEscape(accountID)+"/tokens", body, &response); err != nil {
		return CreatedAPIToken{}, err
	}
	if !response.Success {
		return CreatedAPIToken{}, fmt.Errorf("Cloudflare token create failed: %s", cloudflareErrors(response.Errors))
	}
	if response.Result.ID == "" || response.Result.Value == "" {
		return CreatedAPIToken{}, fmt.Errorf("Cloudflare token create returned no token value")
	}
	return CreatedAPIToken{
		ID:              response.Result.ID,
		Value:           response.Result.Value,
		S3AccessKeyID:   response.Result.ID,
		S3SecretKey:     SHA256Hex([]byte(response.Result.Value)),
		Name:            response.Result.Name,
		PermissionGroup: group.Name,
		Bucket:          bucket,
	}, nil
}

func (c *CloudflareAPIClient) CreateR2AccountToken(ctx context.Context, accountID, name, permissionName string) (CreatedAPIToken, error) {
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	name = strings.TrimSpace(name)
	permissionName = strings.TrimSpace(permissionName)
	if !IsCloudflareAccountID(accountID) {
		return CreatedAPIToken{}, fmt.Errorf("account ID must be a 32-character lowercase hex Cloudflare account ID")
	}
	if name == "" {
		return CreatedAPIToken{}, fmt.Errorf("token name is required")
	}
	if permissionName == "" {
		return CreatedAPIToken{}, fmt.Errorf("permission group name is required")
	}
	group, err := c.findPermissionGroup(ctx, accountID, permissionName, "com.cloudflare.api.account")
	if err != nil {
		return CreatedAPIToken{}, err
	}
	body := map[string]any{
		"name": name,
		"policies": []tokenPolicy{{
			Effect: "allow",
			PermissionGroups: []map[string]string{{
				"id": group.ID,
			}},
			Resources: allBucketsResource(accountID),
		}},
	}
	var response tokenCreateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/accounts/"+url.PathEscape(accountID)+"/tokens", body, &response); err != nil {
		return CreatedAPIToken{}, err
	}
	if !response.Success {
		return CreatedAPIToken{}, fmt.Errorf("Cloudflare token create failed: %s", cloudflareErrors(response.Errors))
	}
	if response.Result.ID == "" || response.Result.Value == "" {
		return CreatedAPIToken{}, fmt.Errorf("Cloudflare token create returned no token value")
	}
	return CreatedAPIToken{
		ID:              response.Result.ID,
		Value:           response.Result.Value,
		S3AccessKeyID:   response.Result.ID,
		S3SecretKey:     SHA256Hex([]byte(response.Result.Value)),
		Name:            response.Result.Name,
		PermissionGroup: group.Name,
	}, nil
}

func (c *CloudflareAPIClient) CreateR2AllBucketsToken(ctx context.Context, accountID, name, permissionName string) (CreatedAPIToken, error) {
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	name = strings.TrimSpace(name)
	permissionName = strings.TrimSpace(permissionName)
	if !IsCloudflareAccountID(accountID) {
		return CreatedAPIToken{}, fmt.Errorf("account ID must be a 32-character lowercase hex Cloudflare account ID")
	}
	if name == "" {
		return CreatedAPIToken{}, fmt.Errorf("token name is required")
	}
	if permissionName == "" {
		return CreatedAPIToken{}, fmt.Errorf("permission group name is required")
	}
	group, err := c.findPermissionGroup(ctx, accountID, permissionName, "com.cloudflare.edge.r2.bucket")
	if err != nil {
		return CreatedAPIToken{}, err
	}
	body := map[string]any{
		"name": name,
		"policies": []tokenPolicy{{
			Effect: "allow",
			PermissionGroups: []map[string]string{{
				"id": group.ID,
			}},
			Resources: allBucketsResource(accountID),
		}},
	}
	var response tokenCreateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/accounts/"+url.PathEscape(accountID)+"/tokens", body, &response); err != nil {
		return CreatedAPIToken{}, err
	}
	if !response.Success {
		return CreatedAPIToken{}, fmt.Errorf("Cloudflare token create failed: %s", cloudflareErrors(response.Errors))
	}
	if response.Result.ID == "" || response.Result.Value == "" {
		return CreatedAPIToken{}, fmt.Errorf("Cloudflare token create returned no token value")
	}
	return CreatedAPIToken{
		ID:              response.Result.ID,
		Value:           response.Result.Value,
		S3AccessKeyID:   response.Result.ID,
		S3SecretKey:     SHA256Hex([]byte(response.Result.Value)),
		Name:            response.Result.Name,
		PermissionGroup: group.Name,
	}, nil
}

func (c *CloudflareAPIClient) CreateTemporaryCredentials(ctx context.Context, accountID string, req TemporaryCredentialRequest) (TemporaryCredentials, error) {
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	if !IsCloudflareAccountID(accountID) {
		return TemporaryCredentials{}, fmt.Errorf("account ID must be a 32-character lowercase hex Cloudflare account ID")
	}
	req, err := req.validate()
	if err != nil {
		return TemporaryCredentials{}, err
	}
	body := map[string]any{
		"bucket":            req.Bucket,
		"parentAccessKeyId": req.ParentAccessKeyID,
		"permission":        req.Permission,
		"ttlSeconds":        int64(req.TTL / time.Second),
	}
	if len(req.Prefixes) > 0 {
		body["prefixes"] = req.Prefixes
	}
	if len(req.Objects) > 0 {
		body["objects"] = req.Objects
	}
	var response temporaryCredentialResponse
	if err := c.doJSON(ctx, http.MethodPost, "/accounts/"+url.PathEscape(accountID)+"/r2/temp-access-credentials", body, &response); err != nil {
		return TemporaryCredentials{}, err
	}
	if !response.Success {
		return TemporaryCredentials{}, fmt.Errorf("Cloudflare temporary R2 credentials create failed: %s", cloudflareErrors(response.Errors))
	}
	if response.Result.AccessKeyID == "" || response.Result.SecretAccessKey == "" || response.Result.SessionToken == "" {
		return TemporaryCredentials{}, fmt.Errorf("Cloudflare temporary R2 credentials response was missing credential fields")
	}
	return TemporaryCredentials{
		AccessKeyID:     response.Result.AccessKeyID,
		SecretAccessKey: response.Result.SecretAccessKey,
		SessionToken:    response.Result.SessionToken,
	}, nil
}

func (c *CloudflareAPIClient) findPermissionGroup(ctx context.Context, accountID, name, scope string) (permissionGroup, error) {
	q := url.Values{}
	q.Set("name", name)
	if scope != "" {
		q.Set("scope", scope)
	}
	path := "/accounts/" + url.PathEscape(accountID) + "/tokens/permission_groups?" + q.Encode()
	var response permissionGroupsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return permissionGroup{}, err
	}
	for _, group := range response.Result {
		if group.Name == name {
			return group, nil
		}
	}
	return permissionGroup{}, fmt.Errorf("Cloudflare permission group %q was not returned for account %s", name, accountID)
}

func (c *CloudflareAPIClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Cloudflare API request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Cloudflare API %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Cloudflare API response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Cloudflare API %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode Cloudflare API response: %w", err)
		}
	}
	return nil
}

func r2BucketResource(accountID, bucket string) string {
	return "com.cloudflare.edge.r2.bucket." + accountID + "_default_" + bucket
}

func accountResource(accountID string) string {
	return "com.cloudflare.api.account." + accountID
}

func allBucketsResource(accountID string) map[string]any {
	return map[string]any{
		accountResource(accountID): map[string]string{
			"com.cloudflare.edge.r2.bucket.*": "*",
		},
	}
}

func cloudflareErrors(messages []cloudflareMessage) string {
	if len(messages) == 0 {
		return "unknown error"
	}
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Code == 0 {
			out = append(out, msg.Message)
			continue
		}
		out = append(out, fmt.Sprintf("%d: %s", msg.Code, msg.Message))
	}
	return strings.Join(out, "; ")
}
