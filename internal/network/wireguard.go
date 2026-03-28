package network

import "github.com/forge-metal/forge-metal/internal/config"

// PeerConfig represents a WireGuard peer entry for a single node.
type PeerConfig struct {
	PublicKey  string
	Endpoint  string
	AllowedIP string
}

// GenerateConfig produces a wg-quick compatible configuration for a node.
func GenerateConfig(cfg config.WireGuardConfig, privateKey string, peers []PeerConfig) string {
	// TODO: generate wg-quick config from structured data
	return ""
}
