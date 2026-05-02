// Package garage is the verself-deploy artifact publisher. It uses
// AWS SigV4 against the controller-side Garage S3 endpoint, taking
// a typed Config (replacing the env-var-and-CLI-flag dance the bash
// predecessor used) and wrapping each HEAD/GET/PUT in an OTel span.
//
// The publisher signs requests with AWS SigV4 (Garage's S3-compatible
// API). Credentials and CA bundle are read from the controller-side
// environment file via SSH `sudo cat`; the caller passes them in as
// raw bytes so this package has no SSH dependency itself.
package garage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/render"
)

const (
	tracerName         = "github.com/verself/deployment-tooling/internal/garage"
	emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	httpClientTimeout  = 30 * time.Second
)

// Config is the controller-side material the publisher needs that
// can't be inferred from the manifest alone: the AWS keys (sourced
// from the env file under sudo) and the CA bundle (used to verify
// the artifact origin's TLS cert).
type Config struct {
	// ConnectAddress is the local-forward address the SSH tunnel exposes
	// (e.g. 127.0.0.1:34567); the publisher dials this in lieu of the
	// origin host.
	ConnectAddress string

	// CABundlePEM is the artifact origin's TLS CA chain (Garage uses
	// our internal PKI; system roots don't include it).
	CABundlePEM []byte

	// AccessKeyID and SecretAccessKey are sourced from the controller's
	// environment file via SSH sudo cat.
	AccessKeyID     string
	SecretAccessKey string
}

// Publisher applies a manifest of artifacts to Garage. It is single-use
// (HTTP client + signer share state) and cheap to construct.
type Publisher struct {
	client   *http.Client
	signer   *awsv4.Signer
	creds    aws.Credentials
	endpoint *url.URL
	region   string
	tracer   trace.Tracer
}

// New constructs a Publisher from a manifest's artifact_delivery and
// the runtime Config.
func New(delivery render.ArtifactDelivery, cfg Config) (*Publisher, error) {
	endpoint, bucket, err := endpointFromGetterPrefix(delivery.GetterSourcePrefix)
	if err != nil {
		return nil, err
	}
	if bucket != delivery.Bucket {
		return nil, fmt.Errorf("artifact delivery bucket mismatch: prefix=%q bucket=%q", bucket, delivery.Bucket)
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("garage: AccessKeyID and SecretAccessKey are required")
	}
	transport, err := transportFor(endpoint, cfg.ConnectAddress, cfg.CABundlePEM)
	if err != nil {
		return nil, err
	}
	region := strings.TrimSpace(delivery.GetterOptions["region"])
	if region == "" {
		region = "garage"
	}
	return &Publisher{
		client: &http.Client{
			Transport: transport,
			Timeout:   httpClientTimeout,
		},
		signer:   awsv4.NewSigner(),
		creds:    aws.Credentials{AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey, Source: "verself-deploy"},
		endpoint: endpoint,
		region:   region,
		tracer:   otel.Tracer(tracerName),
	}, nil
}

// PublishAll uploads every artifact in the manifest, idempotent on
// matching remote sha256. Each item gets its own put_object span
// regardless of whether it actually transfers.
func (p *Publisher) PublishAll(ctx context.Context, m *render.Manifest, repoRoot string) error {
	if m == nil || len(m.Artifacts) == 0 {
		return nil
	}
	for _, item := range m.Artifacts {
		if err := p.publishOne(ctx, item, repoRoot); err != nil {
			return fmt.Errorf("%s: %w", item.Output, err)
		}
	}
	return nil
}

func (p *Publisher) publishOne(ctx context.Context, item render.Artifact, repoRoot string) error {
	ctx, span := p.tracer.Start(ctx, "verself_deploy.garage.put_object",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("garage.bucket", item.Bucket),
			attribute.String("garage.key", item.Key),
			attribute.String("garage.sha256", item.SHA256),
			attribute.String("verself.artifact_output", item.Output),
		),
	)
	defer span.End()

	if item.Bucket == "" || item.Key == "" || item.LocalPath == "" || item.SHA256 == "" {
		err := errors.New("manifest artifact is incomplete")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	localPath := item.ResolveLocalPath(repoRoot)
	digest, err := sha256File(localPath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if digest != item.SHA256 {
		err := fmt.Errorf("local sha256=%s does not match manifest sha256=%s", digest, item.SHA256)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	status, remoteDigest, err := p.head(ctx, item)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	switch status {
	case http.StatusOK:
		if remoteDigest != item.SHA256 {
			actual, err := p.getDigest(ctx, item)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}
			if actual != item.SHA256 {
				err := fmt.Errorf("remote object exists with sha256=%s, want %s", actual, item.SHA256)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}
		}
		span.SetAttributes(
			attribute.String("garage.action", "skip-already-uploaded"),
		)
		span.SetStatus(codes.Ok, "")
		return nil
	case http.StatusNotFound:
		bytes, err := p.put(ctx, localPath, item)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetAttributes(
			attribute.String("garage.action", "uploaded"),
			attribute.Int64("garage.bytes_uploaded", bytes),
		)
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		err := fmt.Errorf("unexpected HEAD status %d for %s", status, item.Key)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
}

func (p *Publisher) head(ctx context.Context, item render.Artifact) (int, string, error) {
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
	return resp.StatusCode, "", nil
}

func (p *Publisher) getDigest(ctx context.Context, item render.Artifact) (string, error) {
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

func (p *Publisher) put(ctx context.Context, localPath string, item render.Artifact) (int64, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat artifact: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.objectURL(item).String(), file)
	if err != nil {
		return 0, err
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/x-tar")
	req.Header.Set("X-Amz-Meta-Sha256", item.SHA256)
	if err := p.sign(ctx, req, item.SHA256); err != nil {
		return 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("put object: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("PUT returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return info.Size(), nil
}

func (p *Publisher) objectURL(item render.Artifact) *url.URL {
	u := *p.endpoint
	u.Path = "/" + path.Join(item.Bucket, item.Key)
	return &u
}

func (p *Publisher) sign(ctx context.Context, req *http.Request, payloadHash string) error {
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

func transportFor(endpoint *url.URL, connectAddress string, caBundle []byte) (*http.Transport, error) {
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if len(caBundle) > 0 {
		if !rootCAs.AppendCertsFromPEM(caBundle) {
			return nil, errors.New("garage: CABundlePEM contained no PEM certificates")
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

// ParseEnvFile reads a sourceable shell env file (KEY=VALUE per
// line, # comments) and returns the extracted access key + secret.
// The manifest-described env-var names guide which keys are pulled; missing
// keys are an error.
func ParseEnvFile(body []byte, accessKeyEnv, secretKeyEnv string) (string, string, error) {
	access, secret := "", ""
	for _, line := range bytes.Split(body, []byte("\n")) {
		s := strings.TrimSpace(string(line))
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		idx := strings.Index(s, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(s[:idx])
		value := strings.Trim(strings.TrimSpace(s[idx+1:]), `"'`)
		switch key {
		case accessKeyEnv:
			access = value
		case secretKeyEnv:
			secret = value
		}
	}
	if access == "" || secret == "" {
		return "", "", fmt.Errorf("env file missing %s and/or %s", accessKeyEnv, secretKeyEnv)
	}
	return access, secret, nil
}
