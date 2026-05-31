// Package s3artifact publishes deploy artifacts to an S3-compatible private
// bucket. It signs HEAD/GET/PUT with AWS SigV4 and wraps each operation in an
// OTel span.
//
// Credentials and optional private-origin TLS roots are passed as typed config,
// so this package has no SSH, Cloudflare, or Nomad dependency.
package s3artifact

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

	"github.com/verself/deployment-tools/internal/deploymodel"
)

const (
	tracerName                = "github.com/verself/deployment-tools/internal/s3artifact"
	emptyPayloadSHA256        = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	httpResponseHeaderTimeout = 30 * time.Second
)

var ErrNotFound = errors.New("s3artifact: object not found")

// Config is the controller-side material the publisher needs that cannot be
// inferred from the checked-in site manifest.
type Config struct {
	// ConnectAddress overrides the TCP dial target while preserving the URL host
	// for TLS/SigV4. R2 leaves this empty; private origins may use a tunnel.
	ConnectAddress string

	// CABundlePEM is only needed for private origins whose TLS root is absent
	// from the system bundle. R2 leaves this empty.
	CABundlePEM []byte

	AccessKeyID     string
	SecretAccessKey string
}

// Publisher applies a manifest of artifacts to the artifact store. It is single-use
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
func New(delivery deploymodel.ArtifactDelivery, cfg Config) (*Publisher, error) {
	endpoint, bucket, err := endpointFromGetterPrefix(delivery.GetterSourcePrefix)
	if err != nil {
		return nil, err
	}
	if bucket != delivery.Bucket {
		return nil, fmt.Errorf("artifact delivery bucket mismatch: prefix=%q bucket=%q", bucket, delivery.Bucket)
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("s3artifact: AccessKeyID and SecretAccessKey are required")
	}
	transport, err := transportFor(endpoint, cfg.ConnectAddress, cfg.CABundlePEM)
	if err != nil {
		return nil, err
	}
	region := strings.TrimSpace(delivery.GetterOptions["region"])
	if region == "" {
		region = "auto"
	}
	return &Publisher{
		client: &http.Client{
			Transport: transport,
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
func (p *Publisher) PublishAll(ctx context.Context, artifacts []deploymodel.Artifact, repoRoot string) error {
	if err := p.ensureBucket(ctx, artifacts); err != nil {
		return err
	}
	for _, item := range artifacts {
		if err := p.publishOne(ctx, item, repoRoot); err != nil {
			return fmt.Errorf("%s: %w", item.Output, err)
		}
	}
	return nil
}

func (p *Publisher) ensureBucket(ctx context.Context, artifacts []deploymodel.Artifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	bucket := artifacts[0].Bucket
	for _, item := range artifacts {
		if item.Bucket != bucket {
			return fmt.Errorf("artifact manifest targets multiple buckets: %q and %q", bucket, item.Bucket)
		}
	}
	ctx, span := p.tracer.Start(ctx, "verself_deploy.artifact_store.ensure_bucket",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("artifact.bucket", bucket)),
	)
	defer span.End()
	status, err := p.headBucket(ctx, bucket)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if status == http.StatusOK {
		span.SetAttributes(attribute.String("artifact.action", "bucket-exists"))
		span.SetStatus(codes.Ok, "")
		return nil
	}
	if status != http.StatusNotFound {
		err := fmt.Errorf("unexpected bucket HEAD status %d for %s", status, bucket)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := p.createBucket(ctx, bucket); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.String("artifact.action", "bucket-created"))
	span.SetStatus(codes.Ok, "")
	return nil
}

func (p *Publisher) PublishBytes(ctx context.Context, item deploymodel.Artifact, body []byte, contentType string) error {
	ctx, span := p.tracer.Start(ctx, "verself_deploy.artifact_store.put_object",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("artifact.bucket", item.Bucket),
			attribute.String("artifact.key", item.Key),
			attribute.String("artifact.sha256", item.SHA256),
			attribute.String("verself.artifact_output", item.Output),
		),
	)
	defer span.End()

	if item.Bucket == "" || item.Key == "" || item.SHA256 == "" {
		err := errors.New("artifact store byte object is incomplete")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	digest := deploymodel.SHA256(body)
	if digest != item.SHA256 {
		err := fmt.Errorf("body sha256=%s does not match expected sha256=%s", digest, item.SHA256)
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
	if status == http.StatusOK {
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
		span.SetAttributes(attribute.String("artifact.action", "skip-already-uploaded"))
		span.SetStatus(codes.Ok, "")
		return nil
	}
	if status != http.StatusNotFound {
		err := fmt.Errorf("unexpected HEAD status %d for %s", status, item.Key)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	uploaded, err := p.putReader(ctx, bytesReader(body), int64(len(body)), contentType, item)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(
		attribute.String("artifact.action", "uploaded"),
		attribute.Int64("artifact.bytes_uploaded", uploaded),
	)
	span.SetStatus(codes.Ok, "")
	return nil
}

func (p *Publisher) ReadBytes(ctx context.Context, item deploymodel.Artifact, maxBytes int64) ([]byte, error) {
	ctx, span := p.tracer.Start(ctx, "verself_deploy.artifact_store.get_object",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("artifact.bucket", item.Bucket),
			attribute.String("artifact.key", item.Key),
			attribute.String("artifact.sha256", item.SHA256),
			attribute.String("verself.artifact_output", item.Output),
		),
	)
	defer span.End()

	if item.Bucket == "" || item.Key == "" {
		err := errors.New("artifact store object is incomplete")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if maxBytes <= 0 {
		err := fmt.Errorf("maxBytes must be positive: %d", maxBytes)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.objectURL(item).String(), http.NoBody)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if err := p.sign(ctx, req, emptyPayloadSHA256); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		err = fmt.Errorf("get object: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		err := fmt.Errorf("%w: %s", ErrNotFound, item.Key)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("GET returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		err = fmt.Errorf("read object: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		err := fmt.Errorf("object exceeds %d bytes", maxBytes)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	digest := deploymodel.SHA256(body)
	if item.SHA256 != "" && digest != item.SHA256 {
		err := fmt.Errorf("remote object sha256=%s does not match expected sha256=%s", digest, item.SHA256)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	metaDigest := strings.TrimSpace(resp.Header.Get("X-Amz-Meta-Sha256"))
	if item.SHA256 == "" && metaDigest == "" {
		err := fmt.Errorf("remote object %s has no expected or metadata sha256", item.Key)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if metaDigest != "" && metaDigest != digest {
		err := fmt.Errorf("remote object metadata sha256=%s does not match body sha256=%s", metaDigest, digest)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("artifact.bytes_downloaded", len(body)))
	span.SetStatus(codes.Ok, "")
	return body, nil
}

func (p *Publisher) Verify(ctx context.Context, item deploymodel.Artifact) error {
	ctx, span := p.tracer.Start(ctx, "verself_deploy.artifact_store.verify_object",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("artifact.bucket", item.Bucket),
			attribute.String("artifact.key", item.Key),
			attribute.String("artifact.sha256", item.SHA256),
			attribute.String("verself.artifact_output", item.Output),
		),
	)
	defer span.End()

	if item.Bucket == "" || item.Key == "" || item.SHA256 == "" {
		err := errors.New("artifact store verify object is incomplete")
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
	if status == http.StatusNotFound {
		err := fmt.Errorf("%w: %s", ErrNotFound, item.Key)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if status != http.StatusOK {
		err := fmt.Errorf("unexpected HEAD status %d for %s", status, item.Key)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if remoteDigest == item.SHA256 {
		span.SetStatus(codes.Ok, "")
		return nil
	}
	actual, err := p.getDigest(ctx, item)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if actual != item.SHA256 {
		err := fmt.Errorf("remote object sha256=%s does not match expected sha256=%s", actual, item.SHA256)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func (p *Publisher) publishOne(ctx context.Context, item deploymodel.Artifact, repoRoot string) error {
	ctx, span := p.tracer.Start(ctx, "verself_deploy.artifact_store.put_object",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("artifact.bucket", item.Bucket),
			attribute.String("artifact.key", item.Key),
			attribute.String("artifact.sha256", item.SHA256),
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
			attribute.String("artifact.action", "skip-already-uploaded"),
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
			attribute.String("artifact.action", "uploaded"),
			attribute.Int64("artifact.bytes_uploaded", bytes),
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

func (p *Publisher) head(ctx context.Context, item deploymodel.Artifact) (int, string, error) {
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
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusOK {
		return resp.StatusCode, strings.TrimSpace(resp.Header.Get("X-Amz-Meta-Sha256")), nil
	}
	return resp.StatusCode, "", nil
}

func (p *Publisher) headBucket(ctx context.Context, bucket string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, p.bucketURL(bucket).String(), http.NoBody)
	if err != nil {
		return 0, err
	}
	if err := p.sign(ctx, req, emptyPayloadSHA256); err != nil {
		return 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("head bucket: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func (p *Publisher) createBucket(ctx context.Context, bucket string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.bucketURL(bucket).String(), strings.NewReader(""))
	if err != nil {
		return err
	}
	req.ContentLength = 0
	if err := p.sign(ctx, req, emptyPayloadSHA256); err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("CreateBucket returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

func (p *Publisher) getDigest(ctx context.Context, item deploymodel.Artifact) (string, error) {
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
	defer func() { _ = resp.Body.Close() }()
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

func (p *Publisher) put(ctx context.Context, localPath string, item deploymodel.Artifact) (int64, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("open artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat artifact: %w", err)
	}
	return p.putReader(ctx, file, info.Size(), "application/x-tar", item)
}

func (p *Publisher) putReader(ctx context.Context, body io.Reader, contentLength int64, contentType string, item deploymodel.Artifact) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.objectURL(item).String(), body)
	if err != nil {
		return 0, err
	}
	req.ContentLength = contentLength
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Amz-Meta-Sha256", item.SHA256)
	if err := p.sign(ctx, req, item.SHA256); err != nil {
		return 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("put object: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("PUT returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return contentLength, nil
}

func (p *Publisher) objectURL(item deploymodel.Artifact) *url.URL {
	u := p.bucketURL(item.Bucket)
	u.Path = "/" + path.Join(item.Bucket, item.Key)
	return u
}

func (p *Publisher) bucketURL(bucket string) *url.URL {
	u := *p.endpoint
	u.Path = "/" + strings.Trim(bucket, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u
}

func bytesReader(body []byte) io.Reader {
	return bytes.NewReader(body)
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
			return nil, errors.New("s3artifact: CABundlePEM contained no PEM certificates")
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
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialContext,
		TLSClientConfig:       &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12},
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: httpResponseHeaderTimeout,
	}, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
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
