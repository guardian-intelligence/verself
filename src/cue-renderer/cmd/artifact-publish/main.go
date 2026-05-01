package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

const emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type manifest struct {
	ArtifactDelivery artifactDelivery `json:"artifact_delivery"`
	Artifacts        []artifact       `json:"artifacts"`
}

type artifactDelivery struct {
	Bucket               string            `json:"bucket"`
	GetterSourcePrefix   string            `json:"getter_source_prefix"`
	GetterOptions        map[string]string `json:"getter_options"`
	PublisherCredentials credentials       `json:"publisher_credentials"`
}

type credentials struct {
	EnvironmentFile    string `json:"environment_file"`
	AccessKeyIDEnv     string `json:"access_key_id_env"`
	SecretAccessKeyEnv string `json:"secret_access_key_env"`
}

type artifact struct {
	Output       string `json:"output"`
	LocalPath    string `json:"local_path"`
	SHA256       string `json:"sha256"`
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	GetterSource string `json:"getter_source"`
}

type publisher struct {
	client   *http.Client
	signer   *awsv4.Signer
	creds    aws.Credentials
	endpoint *url.URL
	region   string
}

func main() {
	var manifestPath string
	var repoRoot string
	var connectAddress string
	var caFile string
	flag.StringVar(&manifestPath, "manifest", "", "Path to publish.json")
	flag.StringVar(&repoRoot, "repo-root", ".", "Repository root used to resolve artifact local_path values")
	flag.StringVar(&connectAddress, "connect-address", "", "Optional TCP address used instead of the manifest endpoint address")
	flag.StringVar(&caFile, "ca-file", "", "Optional PEM CA bundle for the artifact origin")
	flag.Parse()

	if err := run(context.Background(), manifestPath, repoRoot, connectAddress, caFile); err != nil {
		fmt.Fprintf(os.Stderr, "artifact-publish: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, manifestPath, repoRoot, connectAddress, caFile string) error {
	if manifestPath == "" {
		return errors.New("--manifest is required")
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if len(m.Artifacts) == 0 {
		fmt.Fprintln(os.Stderr, "artifact-publish: no artifacts to publish")
		return nil
	}
	pub, err := newPublisher(m.ArtifactDelivery, connectAddress, caFile)
	if err != nil {
		return err
	}
	for _, item := range m.Artifacts {
		if err := pub.publish(ctx, repoRoot, item); err != nil {
			return fmt.Errorf("%s: %w", item.Output, err)
		}
	}
	return nil
}

func newPublisher(delivery artifactDelivery, connectAddress, caFile string) (*publisher, error) {
	endpoint, bucket, err := endpointFromGetterPrefix(delivery.GetterSourcePrefix)
	if err != nil {
		return nil, err
	}
	if bucket != delivery.Bucket {
		return nil, fmt.Errorf("artifact delivery bucket mismatch: prefix=%q bucket=%q", bucket, delivery.Bucket)
	}
	accessKeyID := strings.TrimSpace(os.Getenv(delivery.PublisherCredentials.AccessKeyIDEnv))
	secretAccessKey := strings.TrimSpace(os.Getenv(delivery.PublisherCredentials.SecretAccessKeyEnv))
	if accessKeyID == "" || secretAccessKey == "" {
		return nil, fmt.Errorf("publisher credentials require %s and %s", delivery.PublisherCredentials.AccessKeyIDEnv, delivery.PublisherCredentials.SecretAccessKeyEnv)
	}
	transport, err := transportFor(endpoint, connectAddress, caFile)
	if err != nil {
		return nil, err
	}
	region := strings.TrimSpace(delivery.GetterOptions["region"])
	if region == "" {
		region = "garage"
	}
	return &publisher{
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		signer:   awsv4.NewSigner(),
		creds:    aws.Credentials{AccessKeyID: accessKeyID, SecretAccessKey: secretAccessKey, Source: "verself-artifact-publish"},
		endpoint: endpoint,
		region:   region,
	}, nil
}

func endpointFromGetterPrefix(raw string) (*url.URL, string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "s3::")
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, "", fmt.Errorf("parse getter_source_prefix: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return nil, "", fmt.Errorf("getter_source_prefix must be s3::https://host/bucket, got %q", raw)
	}
	bucket := strings.Trim(u.Path, "/")
	if bucket == "" || strings.Contains(bucket, "/") {
		return nil, "", fmt.Errorf("getter_source_prefix must contain exactly one bucket path segment, got %q", raw)
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u, bucket, nil
}

func transportFor(endpoint *url.URL, connectAddress, caFile string) (*http.Transport, error) {
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if caFile != "" {
		body, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}
		if !rootCAs.AppendCertsFromPEM(body) {
			return nil, fmt.Errorf("ca file %s contained no PEM certificates", caFile)
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	dialContext := dialer.DialContext
	if connectAddress != "" {
		originAddress := endpoint.Host
		dialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			if address == originAddress {
				address = connectAddress
			}
			return dialer.DialContext(ctx, network, address)
		}
	}
	return &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialContext,
		TLSClientConfig:     &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12},
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	}, nil
}

func (p *publisher) publish(ctx context.Context, repoRoot string, item artifact) error {
	if item.Bucket == "" || item.Key == "" || item.LocalPath == "" || item.SHA256 == "" {
		return fmt.Errorf("manifest artifact is incomplete")
	}
	localPath := item.LocalPath
	if !filepath.IsAbs(localPath) {
		localPath = filepath.Join(repoRoot, localPath)
	}
	digest, err := sha256File(localPath)
	if err != nil {
		return err
	}
	if digest != item.SHA256 {
		return fmt.Errorf("local sha256=%s does not match manifest sha256=%s", digest, item.SHA256)
	}
	status, remoteDigest, err := p.head(ctx, item)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK:
		if remoteDigest == item.SHA256 {
			fmt.Fprintf(os.Stderr, "artifact-publish: exists %s sha256=%s\n", item.Key, item.SHA256[:12])
			return nil
		}
		actual, err := p.getDigest(ctx, item)
		if err != nil {
			return err
		}
		if actual != item.SHA256 {
			return fmt.Errorf("remote object exists with sha256=%s, want %s", actual, item.SHA256)
		}
		fmt.Fprintf(os.Stderr, "artifact-publish: exists %s sha256=%s\n", item.Key, item.SHA256[:12])
		return nil
	case http.StatusNotFound:
		return p.put(ctx, localPath, item)
	default:
		return fmt.Errorf("unexpected HEAD status %d for %s", status, item.Key)
	}
}

func (p *publisher) head(ctx context.Context, item artifact) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, p.objectURL(item).String(), http.NoBody)
	if err != nil {
		return 0, "", err
	}
	if err := p.sign(ctx, req, emptyPayloadSHA256); err != nil {
		return 0, "", err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("head object: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusOK {
		return resp.StatusCode, strings.TrimSpace(resp.Header.Get("X-Amz-Meta-Sha256")), nil
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return resp.StatusCode, "", nil
	}
	return resp.StatusCode, "", nil
}

func (p *publisher) getDigest(ctx context.Context, item artifact) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.objectURL(item).String(), http.NoBody)
	if err != nil {
		return "", err
	}
	if err := p.sign(ctx, req, emptyPayloadSHA256); err != nil {
		return "", err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("GET returned status %d", resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		return "", fmt.Errorf("hash remote object: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (p *publisher) put(ctx context.Context, localPath string, item artifact) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat artifact: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.objectURL(item).String(), file)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/x-tar")
	req.Header.Set("X-Amz-Meta-Sha256", item.SHA256)
	if err := p.sign(ctx, req, item.SHA256); err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PUT returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	fmt.Fprintf(os.Stderr, "artifact-publish: uploaded %s sha256=%s\n", item.Key, item.SHA256[:12])
	return nil
}

func (p *publisher) objectURL(item artifact) *url.URL {
	u := *p.endpoint
	u.Path = "/" + path.Join(item.Bucket, item.Key)
	return &u
}

func (p *publisher) sign(ctx context.Context, req *http.Request, payloadHash string) error {
	req.Header.Del("Authorization")
	req.Header.Del("X-Amz-Security-Token")
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if err := p.signer.SignHTTP(ctx, p.creds, req, payloadHash, "s3", p.region, time.Now().UTC(), func(options *awsv4.SignerOptions) {
		options.DisableURIPathEscaping = true
	}); err != nil {
		return fmt.Errorf("sign request: %w", err)
	}
	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
