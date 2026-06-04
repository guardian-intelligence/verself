package sitebootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/verself/integrations/cloudflare/control-plane/r2control"
)

const bootstrapR2CredentialTimeout = 45 * time.Second

const bootstrapR2CredentialRetryInterval = 2 * time.Second

var objectStorageR2RuntimeSecretNames = map[string]string{
	"admin_access_key_id":     "object-storage-service.r2.admin_access_key_id",
	"admin_secret_access_key": "object-storage-service.r2.admin_secret_access_key",
	"proxy_access_key_id":     "object-storage-service.r2.proxy_access_key_id",
	"proxy_secret_access_key": "object-storage-service.r2.proxy_secret_access_key",
}

func provisionBootstrapCloudflareObjectStorage(ctx context.Context, site string, input bootstrapCloudflareObjectStorageInput) (map[string]string, error) {
	site = strings.TrimSpace(site)
	if site == "" {
		return nil, fmt.Errorf("site is required")
	}
	client, err := r2control.NewCloudflareAPIClient(input.AccountAdminAPIToken, bootstrapR2CredentialTimeout)
	if err != nil {
		return nil, err
	}
	verified, err := client.VerifyAccountToken(ctx, input.AccountID)
	if err != nil {
		return nil, fmt.Errorf("verify Cloudflare account-admin token: %w", err)
	}
	if verified.Status != "" && verified.Status != "active" {
		return nil, fmt.Errorf("Cloudflare account-admin token status is %q", verified.Status)
	}
	if _, _, err := client.EnsureR2Bucket(ctx, input.AccountID, input.ObjectStorageBucket); err != nil {
		return nil, fmt.Errorf("ensure Cloudflare R2 bucket %s: %w", input.ObjectStorageBucket, err)
	}
	expiresOn := time.Now().UTC().Add(input.ChildTokenTTL)
	suffix := time.Now().UTC().Format("20060102T150405Z")
	adminToken, err := client.CreateR2BucketTokenWithPermissions(ctx, input.AccountID, input.ObjectStorageBucket, "verself-"+site+"-bootstrap-object-storage-admin-"+suffix, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, expiresOn)
	if err != nil {
		return nil, fmt.Errorf("create Cloudflare R2 object-storage admin token: %w", err)
	}
	persisted := false
	defer cleanupCloudflareBootstrapTokensOnError(ctx, &persisted, client, input.AccountID, adminToken)

	proxyToken, err := client.CreateR2BucketTokenWithPermissions(ctx, input.AccountID, input.ObjectStorageBucket, "verself-"+site+"-bootstrap-object-storage-proxy-"+suffix, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, expiresOn)
	if err != nil {
		return nil, fmt.Errorf("create Cloudflare R2 object-storage proxy token: %w", err)
	}
	defer cleanupCloudflareBootstrapTokensOnError(ctx, &persisted, client, input.AccountID, proxyToken)

	if err := verifyBootstrapR2Token(ctx, input.AccountID, input.ObjectStorageBucket, site, "admin", adminToken); err != nil {
		return nil, err
	}
	if err := verifyBootstrapR2Token(ctx, input.AccountID, input.ObjectStorageBucket, site, "proxy", proxyToken); err != nil {
		return nil, err
	}
	persisted = true
	return map[string]string{
		objectStorageR2RuntimeSecretNames["admin_access_key_id"]:     adminToken.S3AccessKeyID,
		objectStorageR2RuntimeSecretNames["admin_secret_access_key"]: adminToken.S3SecretKey,
		objectStorageR2RuntimeSecretNames["proxy_access_key_id"]:     proxyToken.S3AccessKeyID,
		objectStorageR2RuntimeSecretNames["proxy_secret_access_key"]: proxyToken.S3SecretKey,
	}, nil
}

func verifyBootstrapR2Token(ctx context.Context, accountID, bucket, site, role string, token r2control.CreatedAPIToken) error {
	r2, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(accountID),
		Region:          "auto",
		AccessKeyID:     token.S3AccessKeyID,
		SecretAccessKey: token.S3SecretKey,
		Source:          "verself-bootstrap-" + role,
		Timeout:         bootstrapR2CredentialTimeout,
	})
	if err != nil {
		return err
	}
	body := make([]byte, 32)
	if _, err := rand.Read(body); err != nil {
		return fmt.Errorf("generate R2 verification body: %w", err)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	key := "bootstrap/" + safeRemoteArtifactName(site) + "/r2-credential-verification/" + safeRemoteArtifactName(role) + "-" + digest + ".bin"
	return retryBootstrapR2CredentialPropagation(ctx, role, func() error {
		return verifyBootstrapR2TokenOnce(ctx, r2, bucket, key, body, digest, role)
	})
}

func verifyBootstrapR2TokenOnce(ctx context.Context, r2 *r2control.R2Client, bucket, key string, body []byte, digest, role string) error {
	status, err := r2.PutObject(ctx, bucket, key, bytes.NewReader(body), digest)
	if err != nil {
		return fmt.Errorf("verify Cloudflare R2 %s token PUT: %w", role, err)
	}
	if status < 200 || status >= 300 {
		return bootstrapR2VerificationStatusError{operation: "put verification object", status: status}
	}
	status, err = r2.HeadObject(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("verify Cloudflare R2 %s token HEAD: %w", role, err)
	}
	if status != http.StatusOK {
		return bootstrapR2VerificationStatusError{operation: "head verification object", status: status}
	}
	status, got, err := r2.GetObject(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("verify Cloudflare R2 %s token GET: %w", role, err)
	}
	if status != http.StatusOK {
		return bootstrapR2VerificationStatusError{operation: "get verification object", status: status}
	}
	if !bytes.Equal(got, body) {
		return fmt.Errorf("verify Cloudflare R2 %s token object body mismatch", role)
	}
	return nil
}

type bootstrapR2VerificationStatusError struct {
	operation string
	status    int
}

func (e bootstrapR2VerificationStatusError) Error() string {
	return fmt.Sprintf("%s returned status %d", e.operation, e.status)
}

func retryBootstrapR2CredentialPropagation(ctx context.Context, role string, fn func() error) error {
	retryCtx, cancel := context.WithTimeout(ctx, bootstrapR2CredentialTimeout)
	defer cancel()
	for {
		err := fn()
		if err == nil {
			return nil
		}
		if !retryableBootstrapR2CredentialPropagation(err) {
			return err
		}
		timer := time.NewTimer(bootstrapR2CredentialRetryInterval)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return fmt.Errorf("verify Cloudflare R2 %s token did not complete before propagation timeout: %w", role, err)
		case <-timer.C:
		}
	}
}

func retryableBootstrapR2CredentialPropagation(err error) bool {
	var apiErr r2control.APIStatusError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden
	}
	var responseErr r2control.StatusError
	if errors.As(err, &responseErr) {
		return responseErr.StatusCode == http.StatusUnauthorized || responseErr.StatusCode == http.StatusForbidden
	}
	var statusErr bootstrapR2VerificationStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusUnauthorized || statusErr.status == http.StatusForbidden
	}
	return false
}

func cleanupCloudflareBootstrapTokensOnError(ctx context.Context, persisted *bool, client *r2control.CloudflareAPIClient, accountID string, token r2control.CreatedAPIToken) {
	if persisted != nil && *persisted {
		return
	}
	if strings.TrimSpace(token.ID) == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bootstrapR2CredentialTimeout)
	defer cancel()
	_ = client.DeleteAccountToken(cleanupCtx, accountID, token.ID)
}

func mergeBootstrapRuntimeSecretImports(left, right map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for _, source := range []map[string]string{left, right} {
		names := make([]string, 0, len(source))
		for name := range source {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := strings.TrimSpace(source[name])
			if value == "" {
				return nil, fmt.Errorf("bootstrap runtime secret %s value is empty", name)
			}
			if existing, ok := out[name]; ok && existing != value {
				return nil, fmt.Errorf("bootstrap runtime secret %s was provided by multiple inputs with different values", name)
			}
			out[name] = value
		}
	}
	return out, nil
}
