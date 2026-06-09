package ociidentity

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var sha256DigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// WorkloadCoordinates are the stable scheduler dimensions for one Nomad task.
type WorkloadCoordinates struct {
	Site           string `json:"site"`
	NomadNamespace string `json:"nomad_namespace"`
	NomadJob       string `json:"nomad_job"`
	NomadGroup     string `json:"nomad_group"`
	NomadTask      string `json:"nomad_task"`
}

// Packet is the stable identity basis a future workload attestor must prove.
type Packet struct {
	Site               string   `json:"site"`
	NomadNamespace     string   `json:"nomad_namespace"`
	NomadJob           string   `json:"nomad_job"`
	NomadGroup         string   `json:"nomad_group"`
	NomadTask          string   `json:"nomad_task"`
	OCIArchiveSHA256   string   `json:"oci_archive_sha256"`
	OCIRootDigests     []string `json:"oci_root_digests"`
	OCIManifestDigests []string `json:"oci_manifest_digests"`
	Selectors          []string `json:"selectors"`
}

// NewPacket builds a SPIRE-selector-shaped identity packet from scheduler
// coordinates and registry-addressable OCI digests.
func NewPacket(workload WorkloadCoordinates, digests OCIDigests) (Packet, error) {
	if err := validateCoordinates(workload); err != nil {
		return Packet{}, err
	}
	if err := validateDigest(digests.ArchiveSHA256); err != nil {
		return Packet{}, fmt.Errorf("ociidentity: OCI archive digest: %w", err)
	}
	if len(digests.RootDigests) == 0 {
		return Packet{}, errors.New("ociidentity: at least one OCI root digest is required")
	}
	for _, digest := range digests.RootDigests {
		if err := validateDigest(digest); err != nil {
			return Packet{}, fmt.Errorf("ociidentity: OCI root digest: %w", err)
		}
	}
	if len(digests.ManifestDigests) == 0 {
		return Packet{}, errors.New("ociidentity: at least one OCI manifest digest is required")
	}
	for _, digest := range digests.ManifestDigests {
		if err := validateDigest(digest); err != nil {
			return Packet{}, fmt.Errorf("ociidentity: OCI manifest digest: %w", err)
		}
	}

	selectors := []string{
		"site:" + workload.Site,
		"nomad:namespace:" + workload.NomadNamespace,
		"nomad:job:" + workload.NomadJob,
		"nomad:group:" + workload.NomadGroup,
		"nomad:task:" + workload.NomadTask,
	}
	for _, digest := range uniqueSorted(digests.RootDigests) {
		selectors = append(selectors, "oci:root-digest:"+digest)
	}
	for _, digest := range uniqueSorted(digests.ManifestDigests) {
		selectors = append(selectors, "oci:manifest-digest:"+digest)
	}

	return Packet{
		Site:               workload.Site,
		NomadNamespace:     workload.NomadNamespace,
		NomadJob:           workload.NomadJob,
		NomadGroup:         workload.NomadGroup,
		NomadTask:          workload.NomadTask,
		OCIArchiveSHA256:   digests.ArchiveSHA256,
		OCIRootDigests:     uniqueSorted(digests.RootDigests),
		OCIManifestDigests: uniqueSorted(digests.ManifestDigests),
		Selectors:          selectors,
	}, nil
}

func validateCoordinates(workload WorkloadCoordinates) error {
	fields := []struct {
		name  string
		value string
	}{
		{"site", workload.Site},
		{"nomad namespace", workload.NomadNamespace},
		{"nomad job", workload.NomadJob},
		{"nomad group", workload.NomadGroup},
		{"nomad task", workload.NomadTask},
	}
	for _, field := range fields {
		if field.value == "" {
			return fmt.Errorf("ociidentity: %s is required", field.name)
		}
		if strings.ContainsAny(field.value, ":\n\r\t ") {
			return fmt.Errorf("ociidentity: %s %q contains a selector delimiter or whitespace", field.name, field.value)
		}
	}
	return nil
}

func validateDigest(digest string) error {
	if !sha256DigestPattern.MatchString(digest) {
		return fmt.Errorf("%q is not a sha256 digest", digest)
	}
	return nil
}
