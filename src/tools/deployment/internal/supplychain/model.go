package supplychain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultPolicyPath = "src/host-configuration/supply-chain/__generated/policy.json"
	PnpmWorkspacePath = "src/frontends/viteplus-monorepo/pnpm-workspace.yaml"

	ResultAccepted    = "accepted"
	ResultProvisional = "provisional"
	ResultRejected    = "rejected"

	AdmissionAdmitted    = "admitted"
	AdmissionProvisional = "provisional"
)

// Finding is one install, fetch, registry, or package-source boundary found in
// the repo. SourcePath is repo-relative and stable enough to use in policy.
type Finding struct {
	ID          string `json:"id"`
	SourcePath  string `json:"source_path"`
	Line        uint32 `json:"line"`
	SourceKind  string `json:"source_kind"`
	Surface     string `json:"surface"`
	Artifact    string `json:"artifact"`
	UpstreamURL string `json:"upstream_url"`
	Digest      string `json:"digest"`
	Evidence    string `json:"evidence"`
}

type Report struct {
	Findings []Finding `json:"findings"`
}

type Requirements struct {
	RejectUntrackedFetchPaths bool   `json:"reject_untracked_fetch_paths"`
	RequireDigestForBytes     bool   `json:"require_digest_for_bytes"`
	MinimumAgeDays            uint16 `json:"minimum_age_days"`
}

type AdmissionMetadata struct {
	State                 string `json:"state"`
	UpstreamURL           string `json:"upstream_url"`
	InstallURL            string `json:"install_url"`
	Digest                string `json:"digest"`
	ReleasedAt            string `json:"released_at"`
	ObservedAt            string `json:"observed_at"`
	MinimumAgeResult      string `json:"minimum_age_result"`
	ScannerResults        string `json:"scanner_results"`
	ScannerName           string `json:"scanner_name"`
	ScannerVersion        string `json:"scanner_version"`
	ScannerDatabaseDigest string `json:"scanner_database_digest"`
	SBOMURI               string `json:"sbom_uri"`
	SBOMDigest            string `json:"sbom_digest"`
	ProvenanceURI         string `json:"provenance_uri"`
	ProvenanceDigest      string `json:"provenance_digest"`
	OCIRepository         string `json:"oci_repository"`
	OCIManifestDigest     string `json:"oci_manifest_digest"`
	OCIMediaType          string `json:"oci_media_type"`
	SignatureDigest       string `json:"signature_digest"`
	AttestationDigest     string `json:"attestation_digest"`
	ScannerResultDigest   string `json:"scanner_result_digest"`
	GUACSubject           string `json:"guac_subject"`
	TUFTargetPath         string `json:"tuf_target_path"`
	StorageURI            string `json:"storage_uri"`
}

type PolicyArtifact struct {
	ID         string            `json:"id"`
	SourcePath string            `json:"source_path"`
	SourceKind string            `json:"source_kind"`
	Surface    string            `json:"surface"`
	Artifact   string            `json:"artifact"`
	Admission  AdmissionMetadata `json:"admission"`
}

type Policy struct {
	SchemaVersion int              `json:"schema_version"`
	Requirements  Requirements     `json:"requirements"`
	Artifacts     []PolicyArtifact `json:"artifacts"`
}

type FindingResult struct {
	Finding               Finding
	PolicyResult          string
	PolicyReason          string
	AdmissionState        string
	InstallURL            string
	MinimumAgeResult      string
	ScannerResults        string
	OCIRepository         string
	OCIManifestDigest     string
	OCIMediaType          string
	SignatureDigest       string
	AttestationDigest     string
	SBOMDigest            string
	ProvenanceDigest      string
	ScannerResultDigest   string
	ScannerName           string
	ScannerVersion        string
	ScannerDatabaseDigest string
	GUACSubject           string
	TUFTargetPath         string
	StorageURI            string
}

type Evaluation struct {
	Results     []FindingResult
	Accepted    uint32
	Provisional uint32
	Rejected    uint32
}

func (e Evaluation) HasRejected() bool {
	return e.Rejected > 0
}

func (e Evaluation) HasProvisional() bool {
	return e.Provisional > 0
}

func SortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.SourcePath != b.SourcePath {
			return a.SourcePath < b.SourcePath
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.SourceKind != b.SourceKind {
			return a.SourceKind < b.SourceKind
		}
		if a.Artifact != b.Artifact {
			return a.Artifact < b.Artifact
		}
		return a.UpstreamURL < b.UpstreamURL
	})
}

func NewPolicyFromReport(report Report) Policy {
	findings := append([]Finding{}, report.Findings...)
	SortFindings(findings)
	artifacts := make([]PolicyArtifact, 0, len(findings))
	for _, f := range findings {
		artifacts = append(artifacts, PolicyArtifact{
			ID:         f.ID,
			SourcePath: f.SourcePath,
			SourceKind: f.SourceKind,
			Surface:    f.Surface,
			Artifact:   f.Artifact,
			Admission: AdmissionMetadata{
				State:            AdmissionProvisional,
				UpstreamURL:      f.UpstreamURL,
				InstallURL:       "",
				Digest:           f.Digest,
				ReleasedAt:       "",
				ObservedAt:       "",
				MinimumAgeResult: "not_evaluated",
				ScannerResults:   "not_evaluated",
				ScannerName:      "",
				ScannerVersion:   "",
				SBOMURI:          "",
				SBOMDigest:       "",
				ProvenanceURI:    "",
				ProvenanceDigest: "",
				OCIRepository:    "",
				TUFTargetPath:    "",
				StorageURI:       "",
			},
		})
	}
	return Policy{
		SchemaVersion: 1,
		Requirements: Requirements{
			RejectUntrackedFetchPaths: true,
			RequireDigestForBytes:     true,
			MinimumAgeDays:            3,
		},
		Artifacts: artifacts,
	}
}

func MakeID(sourcePath string, line uint32, sourceKind, artifact string) string {
	raw := strings.Join([]string{filepath.ToSlash(sourcePath), fmt.Sprint(line), sourceKind, artifact}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	prefix := slug(filepath.Base(sourcePath) + "-" + sourceKind + "-" + artifact)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(sum[:])[:12])
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.ToLower(s)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "artifact"
	}
	if len(s) > 72 {
		return strings.Trim(s[:72], "-")
	}
	return s
}
