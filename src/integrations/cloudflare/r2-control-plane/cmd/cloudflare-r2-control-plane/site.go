package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/verself/deployment-tools/r2control"
)

type siteArtifactConfig struct {
	AccountID string
	Bucket    string
	Region    string
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
			CloudflareAccountID    string            `json:"cloudflare_account_id"`
			CloudflareAccountIDEnv string            `json:"cloudflare_account_id_env"`
			GetterOptions          map[string]string `json:"getter_options"`
			ChecksumAlgorithm      string            `json:"checksum_algorithm"`
			Public                 *bool             `json:"public"`
		} `json:"artifact_delivery"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return siteArtifactConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if raw.ArtifactDelivery.Kind != "cloudflare_r2_s3" {
		return siteArtifactConfig{}, fmt.Errorf("%s: artifact_delivery.kind must be cloudflare_r2_s3", path)
	}
	accountID, err := resolveCloudflareAccountID(raw.ArtifactDelivery.CloudflareAccountID, raw.ArtifactDelivery.CloudflareAccountIDEnv)
	if err != nil {
		return siteArtifactConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	region := strings.TrimSpace(raw.ArtifactDelivery.GetterOptions["region"])
	if region == "" {
		region = "auto"
	}
	return siteArtifactConfig{
		AccountID: accountID,
		Bucket:    raw.ArtifactDelivery.Bucket,
		Region:    region,
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
