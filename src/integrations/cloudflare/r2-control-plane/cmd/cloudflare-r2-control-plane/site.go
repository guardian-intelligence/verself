package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/verself/integrations/cloudflare/r2-control-plane/internal/r2control"
)

type siteArtifactConfig struct {
	AccountID  string
	Bucket     string
	KeyPrefix  string
	SitePrefix string
	Region     string
}

const cloudflareAccountConfigVersion = "verself.cloudflare.account.v1"

type cloudflareAccountConfig struct {
	AccountID                 string
	DeploymentArtifactsBucket string
	RecoveryBucket            string
}

func loadSiteConfig(repoRoot, site string) (siteArtifactConfig, error) {
	path := filepath.Join(repoRoot, "src", "host", "sites", site, "site.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return siteArtifactConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var raw struct {
		ArtifactDelivery struct {
			Kind                   string `json:"kind"`
			Bucket                 string `json:"bucket"`
			KeyPrefix              string `json:"key_prefix"`
			CloudflareAccountID    string `json:"cloudflare_account_id"`
			CloudflareAccountIDEnv string `json:"cloudflare_account_id_env"`
			ChecksumAlgorithm      string `json:"checksum_algorithm"`
			Public                 *bool  `json:"public"`
		} `json:"artifact_delivery"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return siteArtifactConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if raw.ArtifactDelivery.Kind != "cloudflare_r2_control_plane" {
		return siteArtifactConfig{}, fmt.Errorf("%s: artifact_delivery.kind must be cloudflare_r2_control_plane", path)
	}
	if raw.ArtifactDelivery.Public == nil || *raw.ArtifactDelivery.Public {
		return siteArtifactConfig{}, fmt.Errorf("%s: artifact_delivery.public must be false", path)
	}
	if strings.TrimSpace(raw.ArtifactDelivery.Bucket) != "" {
		return siteArtifactConfig{}, fmt.Errorf("%s: artifact_delivery.bucket belongs to src/integrations/cloudflare/account.json", path)
	}
	if strings.TrimSpace(raw.ArtifactDelivery.CloudflareAccountID) != "" || strings.TrimSpace(raw.ArtifactDelivery.CloudflareAccountIDEnv) != "" {
		return siteArtifactConfig{}, fmt.Errorf("%s: artifact_delivery.cloudflare_account_id belongs to src/integrations/cloudflare/account.json", path)
	}
	cloudflare, err := loadCloudflareAccountConfig(repoRoot)
	if err != nil {
		return siteArtifactConfig{}, err
	}
	keyPrefix := strings.Trim(raw.ArtifactDelivery.KeyPrefix, "/")
	if keyPrefix == "" {
		keyPrefix = "sha256"
	}
	sitePrefix, err := artifactSitePrefix(site)
	if err != nil {
		return siteArtifactConfig{}, err
	}
	return siteArtifactConfig{
		AccountID:  cloudflare.AccountID,
		Bucket:     cloudflare.DeploymentArtifactsBucket,
		KeyPrefix:  keyPrefix,
		SitePrefix: sitePrefix,
		Region:     "auto",
	}, nil
}

func siteArtifactConfigFromConfig(cfg config) (siteArtifactConfig, error) {
	accountID, err := resolveCloudflareAccountID(cfg.accountID, "")
	if err != nil {
		return siteArtifactConfig{}, err
	}
	bucket := strings.TrimSpace(cfg.bucket)
	if !r2control.IsR2BucketName(bucket) {
		return siteArtifactConfig{}, fmt.Errorf("bucket must be a valid lowercase R2 bucket name")
	}
	region := strings.TrimSpace(cfg.region)
	if region == "" {
		region = "auto"
	}
	keyPrefix := strings.Trim(cfg.keyPrefix, "/")
	if keyPrefix == "" {
		keyPrefix = "sha256"
	}
	sitePrefix, err := artifactSitePrefix(cfg.site)
	if err != nil {
		return siteArtifactConfig{}, err
	}
	return siteArtifactConfig{
		AccountID:  accountID,
		Bucket:     bucket,
		KeyPrefix:  keyPrefix,
		SitePrefix: sitePrefix,
		Region:     region,
	}, nil
}

func loadCloudflareAccountConfig(repoRoot string) (cloudflareAccountConfig, error) {
	path := filepath.Join(repoRoot, "src", "integrations", "cloudflare", "account.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return cloudflareAccountConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var raw struct {
		Version          string `json:"version"`
		ControlPlaneSite string `json:"control_plane_site"`
		AccountID        string `json:"account_id"`
		R2               struct {
			DeploymentArtifactsBucket string `json:"deployment_artifacts_bucket"`
			RecoveryBucket            string `json:"recovery_bucket"`
		} `json:"r2"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return cloudflareAccountConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if raw.Version != cloudflareAccountConfigVersion {
		return cloudflareAccountConfig{}, fmt.Errorf("%s: version must be %s", path, cloudflareAccountConfigVersion)
	}
	if raw.ControlPlaneSite != "prod" {
		return cloudflareAccountConfig{}, fmt.Errorf("%s: control_plane_site must be prod", path)
	}
	accountID, err := resolveCloudflareAccountID(raw.AccountID, "")
	if err != nil {
		return cloudflareAccountConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	deploymentBucket := strings.TrimSpace(raw.R2.DeploymentArtifactsBucket)
	if !r2control.IsR2BucketName(deploymentBucket) {
		return cloudflareAccountConfig{}, fmt.Errorf("%s: r2.deployment_artifacts_bucket must be a valid lowercase R2 bucket name", path)
	}
	recoveryBucket := strings.TrimSpace(raw.R2.RecoveryBucket)
	if !r2control.IsR2BucketName(recoveryBucket) {
		return cloudflareAccountConfig{}, fmt.Errorf("%s: r2.recovery_bucket must be a valid lowercase R2 bucket name", path)
	}
	return cloudflareAccountConfig{
		AccountID:                 accountID,
		DeploymentArtifactsBucket: deploymentBucket,
		RecoveryBucket:            recoveryBucket,
	}, nil
}

func resolveCloudflareAccountID(direct, envName string) (string, error) {
	direct = strings.TrimSpace(direct)
	envName = strings.TrimSpace(envName)
	if direct != "" && envName != "" {
		return "", errors.New("cloudflare account ID must be direct or env-backed, not both")
	}
	accountID := direct
	if envName != "" {
		accountID = strings.TrimSpace(os.Getenv(envName))
		if accountID == "" {
			return "", fmt.Errorf("cloudflare account ID env %s is unset", envName)
		}
	}
	accountID = strings.ToLower(accountID)
	if !r2control.IsCloudflareAccountID(accountID) {
		return "", errors.New("cloudflare account ID must be a 32-character hex value")
	}
	return accountID, nil
}

func artifactSitePrefix(site string) (string, error) {
	site = strings.TrimSpace(site)
	if site == "" {
		return "", errors.New("site is required for artifact object prefix")
	}
	if site == "." || site == ".." || strings.Contains(site, "/") || strings.Contains(site, "\\") {
		return "", fmt.Errorf("site %q cannot be used as an artifact object prefix segment", site)
	}
	for _, r := range site {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return "", fmt.Errorf("site %q cannot be used as an artifact object prefix segment", site)
		}
	}
	return site, nil
}
