package s3artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"github.com/verself/deployment-tools/internal/deploymodel"
)

const (
	emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

type Config struct {
	AccessKeyID     string
	SecretAccessKey string
}

type Publisher struct {
	client   *http.Client
	signer   *awsv4.Signer
	creds    aws.Credentials
	endpoint *url.URL
	bucket   string
	region   string
}

func New(delivery deploymodel.ArtifactDelivery, cfg Config) (*Publisher, error) {
	endpoint, bucket, err := parseGetterPrefix(delivery.GetterSourcePrefix)
	if err != nil {
		return nil, err
	}
	if delivery.Bucket != "" && delivery.Bucket != bucket {
		return nil, fmt.Errorf("artifact bucket mismatch: delivery=%q getter_prefix=%q", delivery.Bucket, bucket)
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, errors.New("s3 artifact credentials are required")
	}
	region := strings.TrimSpace(delivery.GetterOptions["region"])
	if region == "" {
		region = "auto"
	}
	return &Publisher{
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
		signer: awsv4.NewSigner(),
		creds: aws.Credentials{
			AccessKeyID:     strings.TrimSpace(cfg.AccessKeyID),
			SecretAccessKey: strings.TrimSpace(cfg.SecretAccessKey),
			Source:          "verself-deploy",
		},
		endpoint: endpoint,
		bucket:   bucket,
		region:   region,
	}, nil
}

func (p *Publisher) PublishAll(ctx context.Context, artifacts []deploymodel.Artifact, repoRoot string) error {
	if len(artifacts) == 0 {
		return nil
	}
	if err := p.ensureBucket(ctx); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := p.publishOne(ctx, artifact, repoRoot); err != nil {
			return fmt.Errorf("%s: %w", artifact.Output, err)
		}
	}
	return nil
}

func (p *Publisher) ensureBucket(ctx context.Context) error {
	status, err := p.do(ctx, http.MethodHead, p.bucketURL(), nil, emptyPayloadSHA256, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("head artifact bucket %s status %d", p.bucket, status)
	}
	status, err = p.do(ctx, http.MethodPut, p.bucketURL(), nil, emptyPayloadSHA256, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("create artifact bucket %s status %d", p.bucket, status)
	}
	return nil
}

func (p *Publisher) publishOne(ctx context.Context, artifact deploymodel.Artifact, repoRoot string) error {
	if artifact.Bucket != "" && artifact.Bucket != p.bucket {
		return fmt.Errorf("artifact bucket %q does not match publisher bucket %q", artifact.Bucket, p.bucket)
	}
	body, err := os.ReadFile(artifact.ResolveLocalPath(repoRoot))
	if err != nil {
		return fmt.Errorf("read artifact: %w", err)
	}
	digest := sha256Hex(body)
	if digest != artifact.SHA256 {
		return fmt.Errorf("artifact sha256=%s does not match descriptor sha256=%s", digest, artifact.SHA256)
	}
	status, err := p.do(ctx, http.MethodHead, p.objectURL(artifact.Key), nil, emptyPayloadSHA256, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("head artifact object %s status %d", artifact.Key, status)
	}
	headers := http.Header{}
	headers.Set("Content-Type", "application/octet-stream")
	headers.Set("X-Amz-Meta-Sha256", digest)
	status, err = p.do(ctx, http.MethodPut, p.objectURL(artifact.Key), bytes.NewReader(body), digest, headers)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("put artifact object %s status %d", artifact.Key, status)
	}
	return nil
}

func (p *Publisher) do(ctx context.Context, method string, u *url.URL, body io.Reader, payloadHash string, headers http.Header) (int, error) {
	if body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return 0, err
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if err := p.signer.SignHTTP(ctx, p.creds, req, payloadHash, "s3", p.region, time.Now()); err != nil {
		return 0, fmt.Errorf("sign %s %s: %w", method, u.Redacted(), err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", method, u.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, fmt.Errorf("%s %s status %d: %s", method, u.Redacted(), resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func (p *Publisher) bucketURL() *url.URL {
	u := *p.endpoint
	u.Path = "/" + p.bucket
	return &u
}

func (p *Publisher) objectURL(key string) *url.URL {
	u := *p.endpoint
	u.Path = "/" + path.Join(p.bucket, strings.TrimLeft(key, "/"))
	return &u
}

func parseGetterPrefix(prefix string) (*url.URL, string, error) {
	raw := strings.TrimSpace(prefix)
	raw = strings.TrimPrefix(raw, "s3::")
	if raw == "" {
		return nil, "", errors.New("artifact getter prefix is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse artifact getter prefix: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return nil, "", fmt.Errorf("artifact getter prefix must be an https URL")
	}
	bucket := strings.Trim(u.Path, "/")
	if strings.Contains(bucket, "/") || bucket == "" {
		return nil, "", fmt.Errorf("artifact getter prefix must end with one bucket path segment")
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u, bucket, nil
}

func ParseEnvFile(body []byte, accessKeyEnv, secretKeyEnv string) (string, string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return "", "", fmt.Errorf("invalid environment line %q", line)
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	access := strings.TrimSpace(values[accessKeyEnv])
	secret := strings.TrimSpace(values[secretKeyEnv])
	if access == "" || secret == "" {
		return "", "", fmt.Errorf("environment file missing %s and/or %s", accessKeyEnv, secretKeyEnv)
	}
	return access, secret, nil
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
