package objectstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

const r2EmptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type R2BucketProvider struct {
	endpoint *url.URL
	client   *http.Client
	signer   *awsv4.Signer
	creds    aws.Credentials
	region   string
}

func NewR2BucketProvider(endpoint, accessKeyID, secretAccessKey, region string, httpClient *http.Client) (*R2BucketProvider, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse r2 endpoint: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("r2 endpoint must be https://<account_id>.r2.cloudflarestorage.com")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if strings.TrimSpace(accessKeyID) == "" || strings.TrimSpace(secretAccessKey) == "" {
		return nil, fmt.Errorf("r2 access key id and secret access key are required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &R2BucketProvider{
		endpoint: parsed,
		client:   httpClient,
		signer:   awsv4.NewSigner(),
		creds: aws.Credentials{
			AccessKeyID:     strings.TrimSpace(accessKeyID),
			SecretAccessKey: strings.TrimSpace(secretAccessKey),
			Source:          "object-storage-service-r2-provider",
		},
		region: firstNonEmpty(region, "auto"),
	}, nil
}

func (p *R2BucketProvider) Kind() string {
	return ProviderCloudflareR2
}

func (p *R2BucketProvider) Health(ctx context.Context) error {
	if p == nil {
		return fmt.Errorf("r2 provider is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint.String(), http.NoBody)
	if err != nil {
		return err
	}
	if err := p.sign(ctx, req, r2EmptyPayloadSHA256); err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("r2 health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("r2 health returned status %d", resp.StatusCode)
	}
	return nil
}

func (p *R2BucketProvider) CreateBucket(ctx context.Context, input BucketProviderInput) (ProviderBucket, error) {
	if err := rejectR2UnsupportedBucketPolicy(input.QuotaBytes, input.QuotaObjects, input.LifecycleJSON); err != nil {
		return ProviderBucket{}, err
	}
	status, err := p.headBucket(ctx, input.Name)
	if err != nil {
		return ProviderBucket{}, err
	}
	if status == http.StatusOK {
		return ProviderBucket{ID: input.Name, LifecycleRules: normalizeLifecycleJSON(input.LifecycleJSON)}, nil
	}
	if status != http.StatusNotFound {
		return ProviderBucket{}, fmt.Errorf("r2 bucket HEAD status %d", status)
	}
	if err := p.createBucket(ctx, input.Name); err != nil {
		return ProviderBucket{}, err
	}
	return ProviderBucket{ID: input.Name, LifecycleRules: normalizeLifecycleJSON(input.LifecycleJSON)}, nil
}

func (p *R2BucketProvider) UpdateBucket(ctx context.Context, input BucketProviderUpdate) (ProviderBucket, error) {
	if err := rejectR2UnsupportedBucketPolicy(input.QuotaBytes, input.QuotaObjects, input.LifecycleJSON); err != nil {
		return ProviderBucket{}, err
	}
	status, err := p.headBucket(ctx, input.ProviderBucketID)
	if err != nil {
		return ProviderBucket{}, err
	}
	if status != http.StatusOK {
		return ProviderBucket{}, fmt.Errorf("r2 bucket HEAD status %d", status)
	}
	return ProviderBucket{ID: input.ProviderBucketID, LifecycleRules: normalizeLifecycleJSON(input.LifecycleJSON)}, nil
}

func (p *R2BucketProvider) DeleteBucket(ctx context.Context, providerBucketID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.bucketURL(providerBucketID).String(), http.NoBody)
	if err != nil {
		return err
	}
	if err := p.sign(ctx, req, r2EmptyPayloadSHA256); err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("r2 delete bucket: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNotFound || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf("r2 DeleteBucket returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

func (p *R2BucketProvider) PresignObject(ctx context.Context, method, bucketName, objectKey, payloadSHA256 string, ttl time.Duration) (S3CompatibleObject, error) {
	if p == nil {
		return S3CompatibleObject{}, fmt.Errorf("r2 provider is nil")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut:
	default:
		return S3CompatibleObject{}, fmt.Errorf("unsupported object transfer method %q", method)
	}
	if ttl <= 0 {
		return S3CompatibleObject{}, fmt.Errorf("object transfer ttl must be positive")
	}
	payloadSHA256 = strings.TrimSpace(payloadSHA256)
	if payloadSHA256 == "" {
		payloadSHA256 = awsUnsignedPayload
	}
	req, err := http.NewRequestWithContext(ctx, method, p.objectURL(bucketName, objectKey).String(), http.NoBody)
	if err != nil {
		return S3CompatibleObject{}, err
	}
	query := req.URL.Query()
	query.Set("X-Amz-Expires", strconv.FormatInt(int64(ttl/time.Second), 10))
	req.URL.RawQuery = query.Encode()
	signedURL, signedHeaders, err := p.signer.PresignHTTP(ctx, p.creds, req, payloadSHA256, "s3", p.region, time.Now().UTC(), func(options *awsv4.SignerOptions) {
		options.DisableURIPathEscaping = true
	})
	if err != nil {
		return S3CompatibleObject{}, fmt.Errorf("r2 presign object: %w", err)
	}
	headers := map[string]string{}
	for key, values := range signedHeaders {
		if strings.EqualFold(key, "Host") || len(values) == 0 {
			continue
		}
		headers[key] = values[0]
	}
	return S3CompatibleObject{
		Method:    method,
		URL:       signedURL,
		Headers:   headers,
		ExpiresAt: time.Now().UTC().Add(ttl),
		SHA256:    payloadSHA256,
	}, nil
}

func (p *R2BucketProvider) VerifyObject(ctx context.Context, bucketName, objectKey, expectedSHA256 string, expectedSizeBytes int64) error {
	if p == nil {
		return fmt.Errorf("r2 provider is nil")
	}
	if expectedSizeBytes <= 0 {
		return fmt.Errorf("expected size must be positive")
	}
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	if !sigv4HexSHA256Pattern.MatchString(expectedSHA256) {
		return fmt.Errorf("expected sha256 must be a lowercase hex digest")
	}
	size, err := p.headObject(ctx, bucketName, objectKey)
	if err != nil {
		return err
	}
	if size != expectedSizeBytes {
		return fmt.Errorf("object %s/%s size=%d does not match expected size=%d", bucketName, objectKey, size, expectedSizeBytes)
	}
	digest, err := p.objectSHA256(ctx, bucketName, objectKey)
	if err != nil {
		return err
	}
	if digest != expectedSHA256 {
		return fmt.Errorf("object %s/%s sha256=%s does not match expected sha256=%s", bucketName, objectKey, digest, expectedSHA256)
	}
	return nil
}

func (p *R2BucketProvider) headBucket(ctx context.Context, bucket string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, p.bucketURL(bucket).String(), http.NoBody)
	if err != nil {
		return 0, err
	}
	if err := p.sign(ctx, req, r2EmptyPayloadSHA256); err != nil {
		return 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("r2 head bucket: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func (p *R2BucketProvider) createBucket(ctx context.Context, bucket string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.bucketURL(bucket).String(), strings.NewReader(""))
	if err != nil {
		return err
	}
	req.ContentLength = 0
	if err := p.sign(ctx, req, r2EmptyPayloadSHA256); err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("r2 create bucket: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusConflict || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf("r2 CreateBucket returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

func (p *R2BucketProvider) bucketURL(bucket string) *url.URL {
	u := *p.endpoint
	u.Path = "/" + strings.Trim(bucket, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u
}

func (p *R2BucketProvider) objectURL(bucket, objectKey string) *url.URL {
	u := *p.endpoint
	u.Path = "/" + strings.Trim(bucket, "/") + "/" + strings.TrimLeft(objectKey, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u
}

func (p *R2BucketProvider) sign(ctx context.Context, req *http.Request, payloadHash string) error {
	req.Header.Del("Authorization")
	req.Header.Del("X-Amz-Security-Token")
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if err := p.signer.SignHTTP(ctx, p.creds, req, payloadHash, "s3", p.region, time.Now().UTC(), func(options *awsv4.SignerOptions) {
		options.DisableURIPathEscaping = true
	}); err != nil {
		return fmt.Errorf("r2 sign request: %w", err)
	}
	return nil
}

func (p *R2BucketProvider) headObject(ctx context.Context, bucketName, objectKey string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, p.objectURL(bucketName, objectKey).String(), http.NoBody)
	if err != nil {
		return 0, err
	}
	if err := p.sign(ctx, req, r2EmptyPayloadSHA256); err != nil {
		return 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("r2 head object: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return 0, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("r2 HeadObject returned status %d", resp.StatusCode)
	}
	return resp.ContentLength, nil
}

func (p *R2BucketProvider) objectSHA256(ctx context.Context, bucketName, objectKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.objectURL(bucketName, objectKey).String(), http.NoBody)
	if err != nil {
		return "", err
	}
	if err := p.sign(ctx, req, r2EmptyPayloadSHA256); err != nil {
		return "", err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("r2 get object for verification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("r2 GetObject returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, resp.Body); err != nil {
		return "", fmt.Errorf("hash r2 object: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func rejectR2UnsupportedBucketPolicy(quotaBytes, quotaObjects *int64, lifecycleJSON json.RawMessage) error {
	if quotaBytes != nil || quotaObjects != nil {
		return errors.New("cloudflare_r2 provider does not support provider-enforced bucket quotas")
	}
	if !emptyLifecycleJSON(lifecycleJSON) {
		return errors.New("cloudflare_r2 provider lifecycle mapping is not declared")
	}
	return nil
}

func emptyLifecycleJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null" || trimmed == "[]"
}
