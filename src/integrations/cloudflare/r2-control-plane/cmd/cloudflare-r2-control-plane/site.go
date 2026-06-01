package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/verself/integrations/cloudflare/r2-control-plane/internal/r2control"
	"github.com/verself/tools/controllerstate"
)

type siteArtifactConfig struct {
	AccountID          string
	Bucket             string
	KeyPrefix          string
	GetterSourcePrefix string
	Region             string
	AuthTokenFile      string
}

func loadSiteConfig(repoRoot, site string) (siteArtifactConfig, error) {
	path := filepath.Join(repoRoot, "src", "host", "sites", site, "site.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return siteArtifactConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var raw struct {
		ArtifactDelivery struct {
			Kind                   string            `json:"kind"`
			Bucket                 string            `json:"bucket"`
			KeyPrefix              string            `json:"key_prefix"`
			CloudflareAccountID    string            `json:"cloudflare_account_id"`
			CloudflareAccountIDEnv string            `json:"cloudflare_account_id_env"`
			ControlPlaneTokenFile  string            `json:"control_plane_token_file"`
			GetterOptions          map[string]string `json:"getter_options"`
			ChecksumAlgorithm      string            `json:"checksum_algorithm"`
			Public                 *bool             `json:"public"`
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
	accountID, err := resolveCloudflareAccountID(raw.ArtifactDelivery.CloudflareAccountID, raw.ArtifactDelivery.CloudflareAccountIDEnv)
	if err != nil {
		return siteArtifactConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	bucket := strings.TrimSpace(raw.ArtifactDelivery.Bucket)
	if !r2control.IsR2BucketName(bucket) {
		return siteArtifactConfig{}, fmt.Errorf("%s: artifact_delivery.bucket must be a valid lowercase R2 bucket name", path)
	}
	region := strings.TrimSpace(raw.ArtifactDelivery.GetterOptions["region"])
	if region == "" {
		region = "auto"
	}
	tokenFile := strings.TrimSpace(raw.ArtifactDelivery.ControlPlaneTokenFile)
	if tokenFile == "" {
		return siteArtifactConfig{}, fmt.Errorf("%s: artifact_delivery.control_plane_token_file is required", path)
	}
	tokenFile, err = controllerstate.ConfigPath(repoRoot, tokenFile)
	if err != nil {
		return siteArtifactConfig{}, fmt.Errorf("%s: artifact_delivery.control_plane_token_file: %w", path, err)
	}
	keyPrefix := strings.Trim(raw.ArtifactDelivery.KeyPrefix, "/")
	if keyPrefix == "" {
		keyPrefix = "sha256"
	}
	return siteArtifactConfig{
		AccountID:          accountID,
		Bucket:             bucket,
		KeyPrefix:          keyPrefix,
		GetterSourcePrefix: "s3::https://" + accountID + ".r2.cloudflarestorage.com/" + bucket,
		Region:             region,
		AuthTokenFile:      tokenFile,
	}, nil
}

func resolveCloudflareAccountID(direct, envName string) (string, error) {
	direct = strings.TrimSpace(direct)
	envName = strings.TrimSpace(envName)
	if direct != "" && envName != "" {
		return "", errors.New("artifact_delivery must declare only one of cloudflare_account_id or cloudflare_account_id_env")
	}
	accountID := direct
	if envName != "" {
		accountID = strings.TrimSpace(os.Getenv(envName))
		if accountID == "" {
			return "", fmt.Errorf("artifact_delivery.cloudflare_account_id_env %s is unset", envName)
		}
	}
	accountID = strings.ToLower(accountID)
	if !r2control.IsCloudflareAccountID(accountID) {
		return "", errors.New("artifact_delivery.cloudflare_account_id must be a 32-character hex Cloudflare account ID")
	}
	return accountID, nil
}
