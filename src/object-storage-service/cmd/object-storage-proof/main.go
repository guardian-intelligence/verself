package main

import (
	"bytes"
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
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/forge-metal/apiwire"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	fmotel "github.com/forge-metal/otel"
	secretsclient "github.com/forge-metal/secrets-service/client"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var version = "dev"

var tracer = otel.Tracer("object-storage-proof")

type config struct {
	AdminURL        string
	AdminSPIFFEID   spiffeid.ID
	S3URL           string
	S3CACertPath    string
	GarageS3URLs    []string
	GarageRegion    string
	SecretsURL      string
	SecretsSPIFFEID spiffeid.ID
}

type harness struct {
	cfg                  config
	source               *workloadapi.X509Source
	adminHTTPClient      *http.Client
	GarageProxyAccessKey string
	GarageProxySecretKey string
}

type runSummary struct {
	RunID               string `json:"run_id"`
	TraceID             string `json:"trace_id"`
	PrimaryBucketID     string `json:"primary_bucket_id"`
	PrimaryBucketName   string `json:"primary_bucket_name"`
	SecondaryBucketID   string `json:"secondary_bucket_id"`
	SecondaryBucketName string `json:"secondary_bucket_name"`
	Alias               string `json:"alias"`
	ActorSPIFFEID       string `json:"actor_spiffe_id"`
	StaticAccessKeyID   string `json:"static_access_key_id"`
}

type garageSeedManifest struct {
	RunID       string              `json:"run_id"`
	TraceID     string              `json:"trace_id"`
	BucketID    string              `json:"bucket_id"`
	BucketName  string              `json:"bucket_name"`
	ObjectProof []garageObjectProof `json:"objects"`
}

type garageObjectProof struct {
	Key    string `json:"key"`
	SHA256 string `json:"sha256"`
}

type bucketView struct {
	BucketID       string                 `json:"bucket_id"`
	OrgID          string                 `json:"org_id"`
	BucketName     string                 `json:"bucket_name"`
	GarageBucketID string                 `json:"garage_bucket_id"`
	QuotaBytes     *apiwire.DecimalUint64 `json:"quota_bytes,omitempty"`
	QuotaObjects   *apiwire.DecimalUint64 `json:"quota_objects,omitempty"`
	Lifecycle      json.RawMessage        `json:"lifecycle_rules"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
}

type aliasView struct {
	Alias      string `json:"alias"`
	BucketID   string `json:"bucket_id"`
	Prefix     string `json:"prefix"`
	ServiceTag string `json:"service_tag"`
	CreatedAt  string `json:"created_at"`
}

type credentialView struct {
	CredentialID          string  `json:"credential_id"`
	BucketID              string  `json:"bucket_id"`
	AuthMode              string  `json:"auth_mode"`
	DisplayName           string  `json:"display_name"`
	AccessKeyID           string  `json:"access_key_id,omitempty"`
	SPIFFESubject         string  `json:"spiffe_subject,omitempty"`
	CredentialFingerprint string  `json:"credential_fingerprint,omitempty"`
	Status                string  `json:"status"`
	ExpiresAt             *string `json:"expires_at,omitempty"`
	CreatedAt             string  `json:"created_at"`
	RevokedAt             *string `json:"revoked_at,omitempty"`
}

type credentialSecretView struct {
	AccessKeyID     string         `json:"access_key_id"`
	SecretAccessKey string         `json:"secret_access_key"`
	Fingerprint     string         `json:"credential_fingerprint"`
	Credential      credentialView `json:"credential"`
}

type s3AccessResult struct {
	ObjectKey          string
	MultipartObjectKey string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: object-storage-proof <run|garage-seed|garage-verify>")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    "object-storage-proof",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	cfg, err := loadConfigFromEnv()
	if err != nil {
		return err
	}
	source, err := workloadauth.Source(ctx, strings.TrimSpace(os.Getenv(workloadauth.EndpointSocketEnv)))
	if err != nil {
		return fmt.Errorf("open proof spiffe source: %w", err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			logger.ErrorContext(context.Background(), "object-storage-proof spiffe source close", "error", err)
		}
	}()
	h, err := newHarness(ctx, cfg, logger, source)
	if err != nil {
		return err
	}

	switch strings.TrimSpace(args[0]) {
	case "run":
		flags := flag.NewFlagSet("run", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		runID := flags.String("run-id", defaultRunID("object-storage-proof"), "proof run id")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result, err := h.runProof(ctx, *runID)
		if err != nil {
			return err
		}
		return printJSON(result)
	case "garage-seed":
		flags := flag.NewFlagSet("garage-seed", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		runID := flags.String("run-id", defaultRunID("garage-seed"), "garage seed run id")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		result, err := h.garageSeed(ctx, *runID)
		if err != nil {
			return err
		}
		return printJSON(result)
	case "garage-verify":
		flags := flag.NewFlagSet("garage-verify", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		manifestPath := flags.String("manifest", "", "path to garage seed manifest json")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*manifestPath) == "" {
			return errors.New("garage-verify requires --manifest")
		}
		manifest, err := readGarageManifest(*manifestPath)
		if err != nil {
			return err
		}
		return h.garageVerify(ctx, manifest)
	default:
		return fmt.Errorf("unknown object-storage-proof command %q", args[0])
	}
}

func newHarness(ctx context.Context, cfg config, _ *slog.Logger, source *workloadapi.X509Source) (*harness, error) {
	adminHTTPClient, err := workloadauth.MTLSClient(source, cfg.AdminSPIFFEID, http.DefaultTransport)
	if err != nil {
		return nil, fmt.Errorf("admin spiffe client: %w", err)
	}
	secretsHTTPClient, err := workloadauth.MTLSClient(source, cfg.SecretsSPIFFEID, http.DefaultTransport)
	if err != nil {
		return nil, fmt.Errorf("secrets spiffe client: %w", err)
	}
	runtimeSecretsClient, err := secretsclient.New(
		cfg.SecretsURL,
		secretsclient.WithHTTPClient(secretsHTTPClient),
	)
	if err != nil {
		return nil, fmt.Errorf("runtime secrets client: %w", err)
	}
	secretCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values, err := runtimeSecretsClient.ResolvePlatformRuntimeSecrets(secretCtx, []string{
		secretsclient.ObjectStorageGarageProxyAccessKeyIDName,
		secretsclient.ObjectStorageGarageProxySecretAccessKeyName,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve object-storage runtime secrets: %w", err)
	}
	h := &harness{
		cfg:                  cfg,
		source:               source,
		adminHTTPClient:      adminHTTPClient,
		GarageProxyAccessKey: strings.TrimSpace(values[secretsclient.ObjectStorageGarageProxyAccessKeyIDName]),
		GarageProxySecretKey: strings.TrimSpace(values[secretsclient.ObjectStorageGarageProxySecretAccessKeyName]),
	}
	if h.GarageProxyAccessKey == "" || h.GarageProxySecretKey == "" {
		return nil, errors.New("object-storage runtime secrets were incomplete")
	}
	return h, nil
}

func (h *harness) runProof(ctx context.Context, runID string) (*runSummary, error) {
	ctx, span := tracer.Start(ctx, "object_storage.proof.run")
	defer span.End()
	span.SetAttributes(attribute.String("forge_metal.proof_run_id", runID))

	actorID, err := h.actorSPIFFEID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.cleanupStaleProofMTLSPrincipals(ctx, actorID); err != nil {
		return nil, err
	}
	resourceHash := shortHash(runID)
	primaryBucketName := "proof-" + resourceHash + "-primary"
	secondaryBucketName := "proof-" + resourceHash + "-other"
	aliasName := "proof-" + resourceHash + "-alias"
	primaryOrgID := "proof-org-" + resourceHash
	secondaryOrgID := "proof-other-" + resourceHash

	quotaBytes := int64(32 << 20)
	quotaObjects := int64(128)
	primaryBucket, err := h.createBucket(ctx, primaryOrgID, primaryBucketName, &quotaBytes, &quotaObjects)
	if err != nil {
		return nil, err
	}
	primaryCleanup := func() { _ = h.deleteBucket(context.Background(), primaryBucket.BucketID) }
	defer func() { primaryCleanup() }()

	secondaryBucket, err := h.createBucket(ctx, secondaryOrgID, secondaryBucketName, nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.deleteBucket(context.Background(), secondaryBucket.BucketID) }()

	if _, err := h.listBuckets(ctx); err != nil {
		return nil, err
	}
	if _, err := h.getBucket(ctx, primaryBucket.BucketID); err != nil {
		return nil, err
	}
	if _, err := h.updateBucket(ctx, primaryBucket.BucketID, &quotaBytes, &quotaObjects); err != nil {
		return nil, err
	}

	alias, err := h.createAlias(ctx, primaryBucket.BucketID, aliasName, "proof", "object-storage-proof")
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.deleteAlias(context.Background(), primaryBucket.BucketID, alias.Alias) }()
	aliases, err := h.listAliases(ctx, primaryBucket.BucketID)
	if err != nil {
		return nil, err
	}
	if !containsAlias(aliases, alias.Alias) {
		return nil, fmt.Errorf("alias %s was not returned from list aliases", alias.Alias)
	}

	staticCredential, err := h.createStaticCredential(ctx, primaryBucket.BucketID, "proof-static-"+resourceHash)
	if err != nil {
		return nil, err
	}
	credentials, err := h.listCredentials(ctx, primaryBucket.BucketID)
	if err != nil {
		return nil, err
	}
	if !containsCredential(credentials, staticCredential.AccessKeyID) {
		return nil, fmt.Errorf("access key %s was not returned from list credentials", staticCredential.AccessKeyID)
	}
	if err := h.createMTLSPrincipal(ctx, primaryBucket.BucketID, "proof-mtls-"+resourceHash, actorID); err != nil {
		return nil, err
	}

	staticClient, rawStaticHTTPClient, err := h.newFacadeStaticClients(ctx, staticCredential.AccessKeyID, staticCredential.SecretAccessKey)
	if err != nil {
		return nil, err
	}
	mtlsClient, err := h.newFacadeMTLSClient(ctx)
	if err != nil {
		return nil, err
	}

	if _, err := exerciseS3Positive(ctx, staticClient, alias.Alias, "static"); err != nil {
		return nil, err
	}
	if _, err := exerciseS3Positive(ctx, mtlsClient, alias.Alias, "mtls"); err != nil {
		return nil, err
	}

	if err := h.expectSignedStatus(ctx, rawStaticHTTPClient, staticCredential.AccessKeyID, "wrong-"+staticCredential.SecretAccessKey, http.MethodHead, alias.Alias, "", nil, nil, http.StatusForbidden); err != nil {
		return nil, err
	}
	if err := h.expectSignedStatus(ctx, rawStaticHTTPClient, staticCredential.AccessKeyID, staticCredential.SecretAccessKey, http.MethodHead, secondaryBucket.BucketName, "", nil, nil, http.StatusForbidden); err != nil {
		return nil, err
	}
	if err := h.revokeStaticCredential(ctx, staticCredential.AccessKeyID); err != nil {
		return nil, err
	}
	if err := h.expectSignedStatus(ctx, rawStaticHTTPClient, staticCredential.AccessKeyID, staticCredential.SecretAccessKey, http.MethodHead, alias.Alias, "", nil, nil, http.StatusForbidden); err != nil {
		return nil, err
	}

	if err := h.deleteAlias(ctx, primaryBucket.BucketID, alias.Alias); err != nil {
		return nil, err
	}
	if err := h.deleteBucket(ctx, secondaryBucket.BucketID); err != nil {
		return nil, err
	}
	if err := h.deleteBucket(ctx, primaryBucket.BucketID); err != nil {
		return nil, err
	}
	primaryCleanup = func() {}

	traceID := ""
	if sc := oteltrace.SpanContextFromContext(ctx); sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}
	return &runSummary{
		RunID:               runID,
		TraceID:             traceID,
		PrimaryBucketID:     primaryBucket.BucketID,
		PrimaryBucketName:   primaryBucket.BucketName,
		SecondaryBucketID:   secondaryBucket.BucketID,
		SecondaryBucketName: secondaryBucket.BucketName,
		Alias:               alias.Alias,
		ActorSPIFFEID:       actorID,
		StaticAccessKeyID:   staticCredential.AccessKeyID,
	}, nil
}

func (h *harness) garageSeed(ctx context.Context, runID string) (*garageSeedManifest, error) {
	ctx, span := tracer.Start(ctx, "object_storage.proof.garage_seed")
	defer span.End()
	span.SetAttributes(attribute.String("forge_metal.proof_run_id", runID))

	resourceHash := shortHash(runID)
	bucket, err := h.createBucket(ctx, "garage-proof-"+resourceHash, "garage-"+resourceHash, nil, nil)
	if err != nil {
		return nil, err
	}
	directClient, err := h.newGarageStaticClient(ctx, h.GarageProxyAccessKey, h.GarageProxySecretKey)
	if err != nil {
		return nil, err
	}
	objects := make([]garageObjectProof, 0, 3)
	for _, key := range []string{"seed/one.txt", "seed/two.txt", "seed/three.txt"} {
		payload := []byte(strings.TrimPrefix(key, "seed/") + "-" + runID)
		objects = append(objects, garageObjectProof{Key: key, SHA256: sha256Bytes(payload)})
	}
	for _, item := range objects {
		payload := []byte(strings.TrimPrefix(item.Key, "seed/") + "-" + runID)
		if _, err := directClient.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket.BucketName),
			Key:    aws.String(item.Key),
			Body:   bytes.NewReader(payload),
		}); err != nil {
			return nil, err
		}
	}
	traceID := ""
	if sc := oteltrace.SpanContextFromContext(ctx); sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}
	return &garageSeedManifest{
		RunID:       runID,
		TraceID:     traceID,
		BucketID:    bucket.BucketID,
		BucketName:  bucket.BucketName,
		ObjectProof: objects,
	}, nil
}

func (h *harness) garageVerify(ctx context.Context, manifest garageSeedManifest) error {
	ctx, span := tracer.Start(ctx, "object_storage.proof.garage_verify")
	defer span.End()
	span.SetAttributes(attribute.String("forge_metal.proof_run_id", manifest.RunID))

	directClient, err := h.newGarageStaticClient(ctx, h.GarageProxyAccessKey, h.GarageProxySecretKey)
	if err != nil {
		return err
	}
	for _, item := range manifest.ObjectProof {
		resp, err := directClient.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(manifest.BucketName),
			Key:    aws.String(item.Key),
		})
		if err != nil {
			return err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != item.SHA256 {
			return fmt.Errorf("garage object %s content hash mismatch", item.Key)
		}
	}
	for _, item := range manifest.ObjectProof {
		if _, err := directClient.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(manifest.BucketName),
			Key:    aws.String(item.Key),
		}); err != nil {
			return err
		}
	}
	return h.deleteBucket(ctx, manifest.BucketID)
}

func (h *harness) actorSPIFFEID(ctx context.Context) (string, error) {
	if h == nil || h.source == nil {
		return "", errors.New("object-storage-proof x509 source is required")
	}
	svid, err := h.source.GetX509SVID()
	if err != nil {
		return "", fmt.Errorf("fetch proof x509-svid: %w", err)
	}
	return svid.ID.String(), nil
}

func (h *harness) createBucket(ctx context.Context, orgID, bucketName string, quotaBytes, quotaObjects *int64) (bucketView, error) {
	var out bucketView
	quotaBytesWire, err := quotaDecimalUint64(quotaBytes)
	if err != nil {
		return out, err
	}
	quotaObjectsWire, err := quotaDecimalUint64(quotaObjects)
	if err != nil {
		return out, err
	}
	err = h.doAdminJSON(ctx, http.MethodPost, "/api/v1/buckets", map[string]any{
		"org_id":          orgID,
		"bucket_name":     bucketName,
		"quota_bytes":     quotaBytesWire,
		"quota_objects":   quotaObjectsWire,
		"lifecycle_rules": []any{},
	}, &out, http.StatusOK)
	return out, err
}

func (h *harness) updateBucket(ctx context.Context, bucketID string, quotaBytes, quotaObjects *int64) (bucketView, error) {
	var out bucketView
	quotaBytesWire, err := quotaDecimalUint64(quotaBytes)
	if err != nil {
		return out, err
	}
	quotaObjectsWire, err := quotaDecimalUint64(quotaObjects)
	if err != nil {
		return out, err
	}
	err = h.doAdminJSON(ctx, http.MethodPut, "/api/v1/buckets/"+bucketID, map[string]any{
		"quota_bytes":     quotaBytesWire,
		"quota_objects":   quotaObjectsWire,
		"lifecycle_rules": []any{},
	}, &out, http.StatusOK)
	return out, err
}

func quotaDecimalUint64(value *int64) (*apiwire.DecimalUint64, error) {
	if value == nil {
		return nil, nil
	}
	if *value < 0 {
		return nil, fmt.Errorf("quota must be non-negative")
	}
	decimal := apiwire.Uint64(uint64(*value))
	return &decimal, nil
}

func (h *harness) getBucket(ctx context.Context, bucketID string) (bucketView, error) {
	var out bucketView
	err := h.doAdminJSON(ctx, http.MethodGet, "/api/v1/buckets/"+bucketID, nil, &out, http.StatusOK)
	return out, err
}

func (h *harness) listBuckets(ctx context.Context) ([]bucketView, error) {
	var out []bucketView
	err := h.doAdminJSON(ctx, http.MethodGet, "/api/v1/buckets", nil, &out, http.StatusOK)
	return out, err
}

func (h *harness) deleteBucket(ctx context.Context, bucketID string) error {
	return h.doAdminJSON(ctx, http.MethodDelete, "/api/v1/buckets/"+bucketID, nil, nil, http.StatusNoContent)
}

func (h *harness) createAlias(ctx context.Context, bucketID, alias, prefix, serviceTag string) (aliasView, error) {
	var out aliasView
	err := h.doAdminJSON(ctx, http.MethodPost, "/api/v1/buckets/"+bucketID+"/aliases", map[string]any{
		"alias":       alias,
		"prefix":      prefix,
		"service_tag": serviceTag,
	}, &out, http.StatusOK)
	return out, err
}

func (h *harness) listAliases(ctx context.Context, bucketID string) ([]aliasView, error) {
	var out []aliasView
	err := h.doAdminJSON(ctx, http.MethodGet, "/api/v1/buckets/"+bucketID+"/aliases", nil, &out, http.StatusOK)
	return out, err
}

func (h *harness) deleteAlias(ctx context.Context, bucketID, alias string) error {
	return h.doAdminJSON(ctx, http.MethodDelete, "/api/v1/buckets/"+bucketID+"/aliases/"+url.PathEscape(alias), nil, nil, http.StatusNoContent)
}

func (h *harness) createStaticCredential(ctx context.Context, bucketID, displayName string) (credentialSecretView, error) {
	var out credentialSecretView
	err := h.doAdminJSON(ctx, http.MethodPost, "/api/v1/buckets/"+bucketID+"/access-keys", map[string]any{
		"display_name": displayName,
	}, &out, http.StatusOK)
	return out, err
}

func (h *harness) listCredentials(ctx context.Context, bucketID string) ([]credentialView, error) {
	var out []credentialView
	err := h.doAdminJSON(ctx, http.MethodGet, "/api/v1/buckets/"+bucketID+"/credentials", nil, &out, http.StatusOK)
	return out, err
}

func (h *harness) createMTLSPrincipal(ctx context.Context, bucketID, displayName, spiffeSubject string) error {
	return h.doAdminJSON(ctx, http.MethodPost, "/api/v1/buckets/"+bucketID+"/mtls-principals", map[string]any{
		"display_name":   displayName,
		"spiffe_subject": spiffeSubject,
	}, nil, http.StatusOK)
}

func (h *harness) revokeMTLSPrincipal(ctx context.Context, bucketID, credentialID string) error {
	return h.doAdminJSON(ctx, http.MethodDelete, "/api/v1/buckets/"+bucketID+"/mtls-principals/"+credentialID, nil, nil, http.StatusNoContent)
}

func (h *harness) revokeStaticCredential(ctx context.Context, accessKeyID string) error {
	return h.doAdminJSON(ctx, http.MethodDelete, "/api/v1/access-keys/"+accessKeyID, nil, nil, http.StatusNoContent)
}

func (h *harness) cleanupStaleProofMTLSPrincipals(ctx context.Context, actorID string) error {
	buckets, err := h.listBuckets(ctx)
	if err != nil {
		return err
	}
	for _, bucket := range buckets {
		if !strings.HasPrefix(bucket.BucketName, "proof-") {
			continue
		}
		credentials, err := h.listCredentials(ctx, bucket.BucketID)
		if err != nil {
			return err
		}
		for _, credential := range credentials {
			if credential.AuthMode != "spiffe_mtls" || credential.Status != "active" {
				continue
			}
			if credential.SPIFFESubject != actorID || !strings.HasPrefix(credential.DisplayName, "proof-mtls-") {
				continue
			}
			if err := h.revokeMTLSPrincipal(ctx, bucket.BucketID, credential.CredentialID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *harness) doAdminJSON(ctx context.Context, method, path string, body any, out any, wantStatus int) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(h.cfg.AdminURL, "/")+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.adminHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != wantStatus {
		return fmt.Errorf("admin %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}

func (h *harness) newFacadeStaticClients(ctx context.Context, accessKeyID, secretAccessKey string) (*s3.Client, *http.Client, error) {
	httpClient, err := newHTTPClientWithCA(h.cfg.S3CACertPath)
	if err != nil {
		return nil, nil, err
	}
	client, err := newS3Client(ctx, h.cfg.S3URL, h.cfg.GarageRegion, httpClient, credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""))
	if err != nil {
		return nil, nil, err
	}
	return client, httpClient, nil
}

func (h *harness) newFacadeMTLSClient(ctx context.Context) (*s3.Client, error) {
	tlsConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, h.source, h.cfg.S3CACertPath)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	httpClient := &http.Client{
		Transport: otelhttp.NewTransport(transport),
		Timeout:   5 * time.Second,
	}
	return newS3Client(ctx, h.cfg.S3URL, h.cfg.GarageRegion, httpClient, aws.AnonymousCredentials{} /* anonymous mTLS */)
}

func (h *harness) newGarageStaticClient(ctx context.Context, accessKeyID, secretAccessKey string) (*s3.Client, error) {
	endpoint, err := selectReachableEndpoint(ctx, h.cfg.GarageS3URLs)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport.(*http.Transport).Clone()),
		Timeout:   5 * time.Second,
	}
	return newS3Client(ctx, endpoint, h.cfg.GarageRegion, httpClient, credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""))
}

func (h *harness) expectSignedStatus(ctx context.Context, httpClient *http.Client, accessKeyID, secretAccessKey, method, bucket, key string, query url.Values, body []byte, wantStatus int) error {
	resp, err := signedRawS3Request(ctx, httpClient, h.cfg.S3URL, h.cfg.GarageRegion, accessKeyID, secretAccessKey, method, bucket, key, query, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("expected %d from %s %s/%s, got %d: %s", wantStatus, method, bucket, key, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}

func loadConfigFromEnv() (config, error) {
	adminSPIFFEID, err := workloadauth.ParseID(requireEnv("OBJECT_STORAGE_PROOF_ADMIN_SPIFFE_ID"))
	if err != nil {
		return config{}, err
	}
	secretsSPIFFEID, err := workloadauth.ParseID(requireEnv("OBJECT_STORAGE_PROOF_SECRETS_SPIFFE_ID"))
	if err != nil {
		return config{}, err
	}
	return config{
		AdminURL:        requireEnv("OBJECT_STORAGE_PROOF_ADMIN_URL"),
		AdminSPIFFEID:   adminSPIFFEID,
		S3URL:           requireEnv("OBJECT_STORAGE_PROOF_S3_URL"),
		S3CACertPath:    requireEnv("OBJECT_STORAGE_PROOF_S3_CA_CERT"),
		GarageS3URLs:    splitEnvList(requireEnv("OBJECT_STORAGE_PROOF_GARAGE_S3_URLS")),
		GarageRegion:    envOr("OBJECT_STORAGE_PROOF_GARAGE_REGION", "garage"),
		SecretsURL:      requireEnv("OBJECT_STORAGE_PROOF_SECRETS_URL"),
		SecretsSPIFFEID: secretsSPIFFEID,
	}, nil
}

func selectReachableEndpoint(ctx context.Context, endpoints []string) (string, error) {
	if len(endpoints) == 0 {
		return "", errors.New("at least one Garage S3 endpoint is required")
	}
	dialer := &net.Dialer{Timeout: 200 * time.Millisecond}
	var lastErr error
	for _, endpoint := range endpoints {
		parsed, err := url.Parse(strings.TrimSpace(endpoint))
		if err != nil {
			lastErr = err
			continue
		}
		conn, err := dialer.DialContext(ctx, "tcp", parsed.Host)
		if err != nil {
			lastErr = err
			continue
		}
		_ = conn.Close()
		return endpoint, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no reachable Garage S3 endpoints")
	}
	return "", lastErr
}

func splitEnvList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func newS3Client(ctx context.Context, endpoint, region string, httpClient *http.Client, provider aws.CredentialsProvider) (*s3.Client, error) {
	cfg, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(strings.TrimSpace(region)),
		awscfg.WithHTTPClient(httpClient),
		awscfg.WithCredentialsProvider(provider),
	)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.UsePathStyle = true
		options.BaseEndpoint = aws.String(strings.TrimSpace(endpoint))
		options.RetryMaxAttempts = 1
		// The Go SDK defaults HTTPS S3 writes to aws-chunked trailer checksums; phase 2 only proves plain SigV4.
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	}), nil
}

func exerciseS3Positive(ctx context.Context, client *s3.Client, bucket, mode string) (s3AccessResult, error) {
	ctx, span := tracer.Start(ctx, "object_storage.proof.s3_positive")
	defer span.End()
	span.SetAttributes(
		attribute.String("forge_metal.s3_bucket", bucket),
		attribute.String("forge_metal.s3_mode", mode),
	)
	objectKey := mode + "-object.txt"
	objectBody := []byte("object-storage-proof " + mode + " object")
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectKey),
		Body:   bytes.NewReader(objectBody),
	}); err != nil {
		return s3AccessResult{}, err
	}
	if _, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectKey),
	}); err != nil {
		return s3AccessResult{}, err
	}
	getObject, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	data, err := io.ReadAll(getObject.Body)
	getObject.Body.Close()
	if err != nil {
		return s3AccessResult{}, err
	}
	if !bytes.Equal(data, objectBody) {
		return s3AccessResult{}, fmt.Errorf("object body mismatch for %s", objectKey)
	}
	listOutput, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(mode + "-"),
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	if !containsS3Key(listOutput.Contents, objectKey) {
		return s3AccessResult{}, fmt.Errorf("list objects did not contain %s", objectKey)
	}

	multipartKey := mode + "-multipart.bin"
	createMultipart, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(multipartKey),
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	partOne := []byte("object-storage-proof-" + mode + "-part-one-")
	partTwo := []byte("object-storage-proof-" + mode + "-part-two")
	uploadPartOne, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(multipartKey),
		UploadId:   createMultipart.UploadId,
		PartNumber: aws.Int32(1),
		Body:       bytes.NewReader(partOne),
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	uploadPartTwo, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(multipartKey),
		UploadId:   createMultipart.UploadId,
		PartNumber: aws.Int32(2),
		Body:       bytes.NewReader(partTwo),
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	listParts, err := client.ListParts(ctx, &s3.ListPartsInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(multipartKey),
		UploadId: createMultipart.UploadId,
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	if len(listParts.Parts) != 2 {
		return s3AccessResult{}, fmt.Errorf("expected 2 multipart parts, found %d", len(listParts.Parts))
	}
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(multipartKey),
		UploadId: createMultipart.UploadId,
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: []s3types.CompletedPart{
				{ETag: uploadPartOne.ETag, PartNumber: aws.Int32(1)},
				{ETag: uploadPartTwo.ETag, PartNumber: aws.Int32(2)},
			},
		},
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	getMultipart, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(multipartKey),
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	multipartBody, err := io.ReadAll(getMultipart.Body)
	getMultipart.Body.Close()
	if err != nil {
		return s3AccessResult{}, err
	}
	if !bytes.Equal(multipartBody, append(partOne, partTwo...)) {
		return s3AccessResult{}, fmt.Errorf("multipart body mismatch for %s", multipartKey)
	}

	abortKey := mode + "-abort.bin"
	abortMultipart, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(abortKey),
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	if _, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(abortKey),
		UploadId:   abortMultipart.UploadId,
		PartNumber: aws.Int32(1),
		Body:       bytes.NewReader([]byte("abort-part")),
	}); err != nil {
		return s3AccessResult{}, err
	}
	uploads, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(mode + "-"),
	})
	if err != nil {
		return s3AccessResult{}, err
	}
	if !containsMultipartKey(uploads.Uploads, abortKey) {
		return s3AccessResult{}, fmt.Errorf("multipart uploads did not contain %s", abortKey)
	}
	if _, err := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(abortKey),
		UploadId: abortMultipart.UploadId,
	}); err != nil {
		return s3AccessResult{}, err
	}
	if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectKey),
	}); err != nil {
		return s3AccessResult{}, err
	}
	if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(multipartKey),
	}); err != nil {
		return s3AccessResult{}, err
	}
	return s3AccessResult{
		ObjectKey:          objectKey,
		MultipartObjectKey: multipartKey,
	}, nil
}

func signedRawS3Request(ctx context.Context, httpClient *http.Client, endpoint, region, accessKeyID, secretAccessKey, method, bucket, key string, query url.Values, body []byte) (*http.Response, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, err
	}
	parsed.Path = "/" + bucket
	if key != "" {
		parsed.Path += "/" + key
	}
	if query != nil {
		parsed.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		req.Body = http.NoBody
	}
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	signer := awsv4.NewSigner()
	if err := signer.SignHTTP(ctx, aws.Credentials{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		Source:          "object-storage-proof",
	}, req, "UNSIGNED-PAYLOAD", "s3", region, time.Now().UTC(), func(options *awsv4.SignerOptions) {
		options.DisableURIPathEscaping = true
	}); err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func newHTTPClientWithCA(caPath string) (*http.Client, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse ca bundle %s: no certificates found", caPath)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	return &http.Client{
		Transport: otelhttp.NewTransport(transport),
		Timeout:   5 * time.Second,
	}, nil
}

func readGarageManifest(path string) (garageSeedManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return garageSeedManifest{}, err
	}
	var manifest garageSeedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return garageSeedManifest{}, err
	}
	if strings.TrimSpace(manifest.BucketID) == "" || strings.TrimSpace(manifest.BucketName) == "" {
		return garageSeedManifest{}, errors.New("garage manifest was incomplete")
	}
	return manifest, nil
}

func containsAlias(aliases []aliasView, alias string) bool {
	for _, item := range aliases {
		if item.Alias == alias {
			return true
		}
	}
	return false
}

func containsCredential(credentials []credentialView, accessKeyID string) bool {
	for _, item := range credentials {
		if item.AccessKeyID == accessKeyID {
			return true
		}
	}
	return false
}

func containsS3Key(objects []s3types.Object, want string) bool {
	for _, item := range objects {
		if strings.TrimSpace(aws.ToString(item.Key)) == want {
			return true
		}
	}
	return false
}

func containsMultipartKey(uploads []s3types.MultipartUpload, want string) bool {
	for _, item := range uploads {
		if strings.TrimSpace(aws.ToString(item.Key)) == want {
			return true
		}
	}
	return false
}

func sha256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha256Bytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:12]
}

func defaultRunID(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102T150405Z")
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func requireEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic(name + " is required")
	}
	return value
}

func envOr(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
