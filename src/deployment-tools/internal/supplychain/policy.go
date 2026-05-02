package supplychain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func LoadPolicy(repoRoot, policyPath string) (Policy, error) {
	if policyPath == "" {
		policyPath = DefaultPolicyPath
	}
	if !filepath.IsAbs(policyPath) {
		policyPath = filepath.Join(repoRoot, policyPath)
	}
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		return Policy{}, fmt.Errorf("supplychain: read policy %s: %w", policyPath, err)
	}
	var policy Policy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return Policy{}, fmt.Errorf("supplychain: parse policy %s: %w", policyPath, err)
	}
	if err := validatePolicy(policy); err != nil {
		return Policy{}, fmt.Errorf("supplychain: policy %s: %w", policyPath, err)
	}
	return policy, nil
}

func WritePolicy(path string, policy Policy) error {
	raw, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return fmt.Errorf("supplychain: marshal policy: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("supplychain: create policy dir: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("supplychain: write policy %s: %w", path, err)
	}
	return nil
}

func Evaluate(report Report, policy Policy, strictAdmitted bool) (Evaluation, error) {
	byID := map[string]PolicyArtifact{}
	for _, artifact := range policy.Artifacts {
		byID[artifact.ID] = artifact
	}
	results := make([]FindingResult, 0, len(report.Findings))
	eval := Evaluation{}
	observed := map[string]bool{}
	for _, f := range report.Findings {
		observed[f.ID] = true
		res := FindingResult{
			Finding:        f,
			PolicyResult:   ResultRejected,
			PolicyReason:   "untracked direct artifact source",
			AdmissionState: "",
		}
		artifact, ok := byID[f.ID]
		if !ok {
			results = append(results, res)
			eval.Rejected++
			continue
		}
		res.AdmissionState = artifact.Admission.State
		res.TUFTargetPath = artifact.Admission.TUFTargetPath
		res.StorageURI = artifact.Admission.StorageURI
		switch {
		case artifact.SourcePath != f.SourcePath:
			res.PolicyReason = fmt.Sprintf("source path mismatch: policy=%s observed=%s", artifact.SourcePath, f.SourcePath)
		case artifact.SourceKind != f.SourceKind:
			res.PolicyReason = fmt.Sprintf("source kind mismatch: policy=%s observed=%s", artifact.SourceKind, f.SourceKind)
		case artifact.Surface != f.Surface:
			res.PolicyReason = fmt.Sprintf("surface mismatch: policy=%s observed=%s", artifact.Surface, f.Surface)
		case artifact.Artifact != f.Artifact:
			res.PolicyReason = fmt.Sprintf("artifact name mismatch: policy=%s observed=%s", artifact.Artifact, f.Artifact)
		case artifact.Admission.UpstreamURL != f.UpstreamURL:
			res.PolicyReason = fmt.Sprintf("upstream URL mismatch: policy=%s observed=%s", artifact.Admission.UpstreamURL, f.UpstreamURL)
		case artifact.Admission.Digest != f.Digest:
			res.PolicyReason = fmt.Sprintf("digest mismatch: policy=%s observed=%s", artifact.Admission.Digest, f.Digest)
		case strictAdmitted && artifact.Admission.State != AdmissionAdmitted:
			res.PolicyReason = "artifact is tracked but not admitted"
		case artifact.Admission.State == AdmissionAdmitted:
			if policy.Requirements.RequireDigestForBytes && requiresDigest(f.SourceKind) && f.Digest == "" {
				res.PolicyReason = "admitted byte artifact source has no sha256 digest"
				break
			}
			res.PolicyResult = ResultAccepted
			res.PolicyReason = "artifact admitted by policy"
			eval.Accepted++
		default:
			res.PolicyResult = ResultProvisional
			res.PolicyReason = "artifact tracked for cutover but not yet admitted"
			eval.Provisional++
		}
		if res.PolicyResult == ResultRejected {
			eval.Rejected++
		}
		results = append(results, res)
	}
	for _, artifact := range policy.Artifacts {
		if observed[artifact.ID] || artifact.SourceKind != "pnpm_setting" {
			continue
		}
		eval.Rejected++
		results = append(results, FindingResult{
			Finding: Finding{
				ID:         artifact.ID,
				SourcePath: artifact.SourcePath,
				SourceKind: artifact.SourceKind,
				Surface:    artifact.Surface,
				Artifact:   artifact.Artifact,
				Digest:     artifact.Admission.Digest,
			},
			PolicyResult:   ResultRejected,
			PolicyReason:   "required pnpm supply-chain control is missing",
			AdmissionState: artifact.Admission.State,
			TUFTargetPath:  artifact.Admission.TUFTargetPath,
			StorageURI:     artifact.Admission.StorageURI,
		})
	}
	eval.Results = results
	return eval, nil
}

func validatePolicy(policy Policy) error {
	if policy.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d", policy.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, artifact := range policy.Artifacts {
		if artifact.ID == "" {
			return fmt.Errorf("artifact id is required")
		}
		if seen[artifact.ID] {
			return fmt.Errorf("duplicate artifact id %q", artifact.ID)
		}
		seen[artifact.ID] = true
		if artifact.SourcePath == "" || artifact.SourceKind == "" || artifact.Surface == "" || artifact.Artifact == "" {
			return fmt.Errorf("artifact %s has incomplete source identity", artifact.ID)
		}
		if artifact.Admission.UpstreamURL == "" && requiresUpstream(artifact.SourceKind) {
			return fmt.Errorf("artifact %s missing admission.upstream_url", artifact.ID)
		}
		switch artifact.Admission.State {
		case AdmissionAdmitted, AdmissionProvisional:
		default:
			return fmt.Errorf("artifact %s has unsupported admission state %q", artifact.ID, artifact.Admission.State)
		}
		if artifact.Admission.State == AdmissionAdmitted {
			if artifact.Admission.TUFTargetPath == "" {
				return fmt.Errorf("artifact %s admitted without tuf_target_path", artifact.ID)
			}
			if artifact.Admission.StorageURI == "" {
				return fmt.Errorf("artifact %s admitted without storage_uri", artifact.ID)
			}
			if artifact.Admission.MinimumAgeResult == "" || artifact.Admission.ScannerResults == "" {
				return fmt.Errorf("artifact %s admitted without age/scanner results", artifact.ID)
			}
		}
	}
	return nil
}

func requiresDigest(sourceKind string) bool {
	switch sourceKind {
	case "bazel_http_file", "bazel_http_archive", "catalog_url", "direct_release_url", "curl_fetch", "wget_fetch":
		return true
	default:
		return false
	}
}

func requiresUpstream(sourceKind string) bool {
	switch sourceKind {
	case "apt_get_update", "apt_get_install", "npm_install", "uv_tool_install", "uvx_from", "go_install", "pip_install", "curl_fetch", "wget_fetch", "pnpm_setting":
		return false
	default:
		return true
	}
}
