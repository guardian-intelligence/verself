package r2control

import (
	"context"
	"encoding/xml"
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

const (
	EmptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	UnsignedPayload    = "UNSIGNED-PAYLOAD"
)

type R2ClientConfig struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Source          string
	Timeout         time.Duration
}

type R2Client struct {
	endpoint *url.URL
	region   string
	client   *http.Client
	signer   *awsv4.Signer
	creds    aws.Credentials
}

type ObjectSummary struct {
	Key  string
	Size int64
}

type StatusError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e StatusError) Error() string {
	return fmt.Sprintf("r2 %s %s returned status %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

func Endpoint(accountID string) string {
	return "https://" + strings.ToLower(strings.TrimSpace(accountID)) + ".r2.cloudflarestorage.com"
}

func NewR2Client(cfg R2ClientConfig) (*R2Client, error) {
	endpoint, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse R2 endpoint: %w", err)
	}
	if endpoint.Scheme != "https" || endpoint.Host == "" {
		return nil, fmt.Errorf("r2 endpoint must be an https URL")
	}
	endpoint.Path = ""
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, fmt.Errorf("r2 access key ID and secret access key are required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &R2Client{
		endpoint: endpoint,
		region:   firstNonEmpty(cfg.Region, "auto"),
		client:   &http.Client{Timeout: timeout},
		signer:   awsv4.NewSigner(),
		creds: aws.Credentials{
			AccessKeyID:     strings.TrimSpace(cfg.AccessKeyID),
			SecretAccessKey: strings.TrimSpace(cfg.SecretAccessKey),
			SessionToken:    strings.TrimSpace(cfg.SessionToken),
			Source:          firstNonEmpty(cfg.Source, "cloudflare-control-plane"),
		},
	}, nil
}

func (c *R2Client) EnsureBucket(ctx context.Context, bucket string) (bool, bool, error) {
	status, err := c.HeadBucket(ctx, bucket)
	if err != nil {
		return false, false, err
	}
	if status == http.StatusOK {
		return true, false, nil
	}
	if status != http.StatusNotFound {
		return false, false, StatusError{
			Method:     http.MethodHead,
			URL:        c.BucketURL(bucket).Redacted(),
			StatusCode: status,
		}
	}
	status, _, err = c.SignedRequest(ctx, http.MethodPut, c.BucketURL(bucket), http.NoBody, EmptyPayloadSHA256, nil)
	if err != nil {
		return false, false, err
	}
	if status < 200 || status >= 300 {
		return false, false, StatusError{
			Method:     http.MethodPut,
			URL:        c.BucketURL(bucket).Redacted(),
			StatusCode: status,
		}
	}
	status, err = c.HeadBucket(ctx, bucket)
	if err != nil {
		return false, false, err
	}
	if status != http.StatusOK {
		return false, false, StatusError{
			Method:     http.MethodHead,
			URL:        c.BucketURL(bucket).Redacted(),
			StatusCode: status,
		}
	}
	return false, true, nil
}

func (c *R2Client) HeadBucket(ctx context.Context, bucket string) (int, error) {
	status, _, err := c.SignedRequest(ctx, http.MethodHead, c.BucketURL(bucket), http.NoBody, EmptyPayloadSHA256, nil)
	return status, err
}

func (c *R2Client) PutObject(ctx context.Context, bucket, key string, body io.Reader, payloadHash string) (int, error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/octet-stream")
	headers.Set("X-Amz-Meta-Sha256", payloadHash)
	status, _, err := c.SignedRequest(ctx, http.MethodPut, c.ObjectURL(bucket, key), body, payloadHash, headers)
	return status, err
}

func (c *R2Client) PresignPutObject(ctx context.Context, bucket, key, payloadHash string, expires time.Duration) (string, http.Header, error) {
	if expires < time.Minute || expires > 7*24*time.Hour {
		return "", nil, fmt.Errorf("r2 presigned PUT expiry must be between 1 minute and 7 days")
	}
	headers := http.Header{}
	headers.Set("X-Amz-Content-Sha256", UnsignedPayload)
	u := c.ObjectURL(bucket, key)
	query := u.Query()
	query.Set("X-Amz-Expires", strconv.FormatInt(int64(expires/time.Second), 10))
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), http.NoBody)
	if err != nil {
		return "", nil, err
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	signedURL, signedHeaders, err := c.signer.PresignHTTP(ctx, c.creds, req, UnsignedPayload, "s3", c.region, time.Now().UTC(), func(options *awsv4.SignerOptions) {
		options.DisableURIPathEscaping = true
	})
	if err != nil {
		return "", nil, fmt.Errorf("presign R2 PUT %s: %w", u.Redacted(), err)
	}
	return signedURL, signedHeaders, nil
}

func (c *R2Client) HeadObject(ctx context.Context, bucket, key string) (int, error) {
	status, _, err := c.SignedRequest(ctx, http.MethodHead, c.ObjectURL(bucket, key), http.NoBody, EmptyPayloadSHA256, nil)
	return status, err
}

func (c *R2Client) GetObject(ctx context.Context, bucket, key string) (int, []byte, error) {
	return c.SignedRequest(ctx, http.MethodGet, c.ObjectURL(bucket, key), http.NoBody, EmptyPayloadSHA256, nil)
}

func (c *R2Client) ListObjectsV2(ctx context.Context, bucket, prefix string) ([]ObjectSummary, error) {
	token := ""
	objects := []ObjectSummary{}
	for {
		u := c.BucketURL(bucket)
		q := u.Query()
		q.Set("list-type", "2")
		q.Set("max-keys", "1000")
		if strings.TrimSpace(prefix) != "" {
			q.Set("prefix", strings.TrimLeft(prefix, "/"))
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		u.RawQuery = q.Encode()
		status, body, err := c.SignedRequest(ctx, http.MethodGet, u, http.NoBody, EmptyPayloadSHA256, nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("list R2 bucket %s returned status %d: %s", bucket, status, strings.TrimSpace(string(body)))
		}
		var page listBucketV2Result
		if err := xml.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode R2 ListObjectsV2 response: %w", err)
		}
		for _, item := range page.Contents {
			objects = append(objects, ObjectSummary(item))
		}
		if !page.IsTruncated {
			return objects, nil
		}
		token = strings.TrimSpace(page.NextContinuationToken)
		if token == "" {
			return nil, fmt.Errorf("R2 ListObjectsV2 response was truncated without a continuation token")
		}
	}
}

type listBucketV2Result struct {
	IsTruncated           bool                `xml:"IsTruncated"`
	NextContinuationToken string              `xml:"NextContinuationToken"`
	Contents              []listBucketContent `xml:"Contents"`
}

type listBucketContent struct {
	Key  string `xml:"Key"`
	Size int64  `xml:"Size"`
}

func (c *R2Client) SignedRequest(ctx context.Context, method string, u *url.URL, body io.Reader, payloadHash string, headers http.Header) (int, []byte, error) {
	if body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return 0, nil, err
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if err := c.signer.SignHTTP(ctx, c.creds, req, payloadHash, "s3", c.region, time.Now().UTC(), func(options *awsv4.SignerOptions) {
		options.DisableURIPathEscaping = true
	}); err != nil {
		return 0, nil, fmt.Errorf("sign R2 %s %s: %w", method, u.Redacted(), err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("r2 %s %s: %w", method, u.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("read R2 response: %w", err)
	}
	if resp.StatusCode >= 500 {
		return resp.StatusCode, respBody, StatusError{
			Method:     method,
			URL:        u.Redacted(),
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(respBody)),
		}
	}
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
		return resp.StatusCode, respBody, StatusError{
			Method:     method,
			URL:        u.Redacted(),
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(respBody)),
		}
	}
	return resp.StatusCode, respBody, nil
}

func (c *R2Client) BucketURL(bucket string) *url.URL {
	u := *c.endpoint
	u.Path = "/" + strings.Trim(bucket, "/")
	return &u
}

func (c *R2Client) ObjectURL(bucket, key string) *url.URL {
	u := *c.endpoint
	u.Path = "/" + strings.Trim(bucket, "/") + "/" + strings.TrimLeft(key, "/")
	return &u
}

func IsR2BucketName(value string) bool {
	if len(value) < 3 || len(value) > 63 {
		return false
	}
	first := value[0]
	if (first < 'a' || first > 'z') && (first < '0' || first > '9') {
		return false
	}
	last := value[len(value)-1]
	if (last < 'a' || last > 'z') && (last < '0' || last > '9') {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		default:
			return false
		}
	}
	return !strings.Contains(value, "..") && !strings.Contains(value, ".-") && !strings.Contains(value, "-.")
}

func IsCloudflareAccountID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
