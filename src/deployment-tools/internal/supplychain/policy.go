package supplychain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
			continue
		}
		res.AdmissionState = artifact.Admission.State
		res.InstallURL = artifact.Admission.InstallURL
		res.MinimumAgeResult = artifact.Admission.MinimumAgeResult
		res.ScannerResults = artifact.Admission.ScannerResults
		res.OCIRepository = artifact.Admission.OCIRepository
		res.OCIManifestDigest = artifact.Admission.OCIManifestDigest
		res.OCIMediaType = artifact.Admission.OCIMediaType
		res.SignatureDigest = artifact.Admission.SignatureDigest
		res.AttestationDigest = artifact.Admission.AttestationDigest
		res.SBOMDigest = artifact.Admission.SBOMDigest
		res.ProvenanceDigest = artifact.Admission.ProvenanceDigest
		res.ScannerResultDigest = artifact.Admission.ScannerResultDigest
		res.ScannerName = artifact.Admission.ScannerName
		res.ScannerVersion = artifact.Admission.ScannerVersion
		res.ScannerDatabaseDigest = artifact.Admission.ScannerDatabaseDigest
		res.GUACSubject = artifact.Admission.GUACSubject
		res.TUFTargetPath = artifact.Admission.TUFTargetPath
		res.StorageURI = artifact.Admission.StorageURI
		expectedSourceURL := artifact.Admission.UpstreamURL
		if artifact.Admission.State == AdmissionAdmitted && artifact.Admission.InstallURL != "" {
			expectedSourceURL = artifact.Admission.InstallURL
		}
		switch {
		case artifact.SourcePath != f.SourcePath:
			res.PolicyReason = fmt.Sprintf("source path mismatch: policy=%s observed=%s", artifact.SourcePath, f.SourcePath)
		case artifact.SourceKind != f.SourceKind:
			res.PolicyReason = fmt.Sprintf("source kind mismatch: policy=%s observed=%s", artifact.SourceKind, f.SourceKind)
		case artifact.Surface != f.Surface:
			res.PolicyReason = fmt.Sprintf("surface mismatch: policy=%s observed=%s", artifact.Surface, f.Surface)
		case artifact.Artifact != f.Artifact:
			res.PolicyReason = fmt.Sprintf("artifact name mismatch: policy=%s observed=%s", artifact.Artifact, f.Artifact)
		case expectedSourceURL != f.UpstreamURL:
			res.PolicyReason = fmt.Sprintf("install source URL mismatch: policy=%s observed=%s", expectedSourceURL, f.UpstreamURL)
		case artifact.Admission.Digest != f.Digest:
			res.PolicyReason = fmt.Sprintf("digest mismatch: policy=%s observed=%s", artifact.Admission.Digest, f.Digest)
		case isAcceptedPolicyControl(f, artifact, policy):
			res.PolicyResult = ResultAccepted
			res.PolicyReason = "policy control satisfied"
		case strictAdmitted && artifact.Admission.State != AdmissionAdmitted:
			res.PolicyReason = "artifact is tracked but not admitted"
		case artifact.Admission.State == AdmissionAdmitted:
			if policy.Requirements.RequireDigestForBytes && requiresDigest(f.SourceKind) && f.Digest == "" {
				res.PolicyReason = "admitted byte artifact source has no sha256 digest"
				break
			}
			if artifact.Admission.OCIRepository == "" || artifact.Admission.OCIManifestDigest == "" || artifact.Admission.OCIMediaType == "" {
				res.PolicyReason = "admitted artifact missing OCI repository, manifest digest, or media type"
				break
			}
			if artifact.Admission.SignatureDigest == "" || artifact.Admission.AttestationDigest == "" {
				res.PolicyReason = "admitted artifact missing admission signature or attestation digest"
				break
			}
			if artifact.Admission.SBOMDigest == "" || artifact.Admission.ProvenanceDigest == "" || artifact.Admission.ScannerResultDigest == "" {
				res.PolicyReason = "admitted artifact missing SBOM, provenance, or scanner evidence digest"
				break
			}
			if artifact.Admission.MinimumAgeResult != "passed" || artifact.Admission.ScannerResults != "passed" {
				res.PolicyReason = "admitted artifact has non-passing age or scanner result"
				break
			}
			if artifact.Admission.StorageURI == "" {
				res.PolicyReason = "admitted artifact missing storage URI"
				break
			}
			res.PolicyResult = ResultAccepted
			res.PolicyReason = "artifact admitted by policy"
		default:
			res.PolicyResult = ResultProvisional
			res.PolicyReason = "artifact tracked for cutover but not yet admitted"
		}
		results = append(results, res)
	}
	for _, artifact := range policy.Artifacts {
		if observed[artifact.ID] || artifact.SourceKind != "pnpm_setting" {
			continue
		}
		results = append(results, FindingResult{
			Finding: Finding{
				ID:         artifact.ID,
				SourcePath: artifact.SourcePath,
				SourceKind: artifact.SourceKind,
				Surface:    artifact.Surface,
				Artifact:   artifact.Artifact,
				Digest:     artifact.Admission.Digest,
			},
			PolicyResult:          ResultRejected,
			PolicyReason:          "required pnpm supply-chain control is missing",
			AdmissionState:        artifact.Admission.State,
			InstallURL:            artifact.Admission.InstallURL,
			MinimumAgeResult:      artifact.Admission.MinimumAgeResult,
			ScannerResults:        artifact.Admission.ScannerResults,
			OCIRepository:         artifact.Admission.OCIRepository,
			OCIManifestDigest:     artifact.Admission.OCIManifestDigest,
			OCIMediaType:          artifact.Admission.OCIMediaType,
			SignatureDigest:       artifact.Admission.SignatureDigest,
			AttestationDigest:     artifact.Admission.AttestationDigest,
			SBOMDigest:            artifact.Admission.SBOMDigest,
			ProvenanceDigest:      artifact.Admission.ProvenanceDigest,
			ScannerResultDigest:   artifact.Admission.ScannerResultDigest,
			ScannerName:           artifact.Admission.ScannerName,
			ScannerVersion:        artifact.Admission.ScannerVersion,
			ScannerDatabaseDigest: artifact.Admission.ScannerDatabaseDigest,
			GUACSubject:           artifact.Admission.GUACSubject,
			TUFTargetPath:         artifact.Admission.TUFTargetPath,
			StorageURI:            artifact.Admission.StorageURI,
		})
	}
	results = enforcePnpmControls(policy, results)
	eval := Evaluation{}
	eval.Results = results
	for _, result := range results {
		switch result.PolicyResult {
		case ResultAccepted:
			eval.Accepted++
		case ResultProvisional:
			eval.Provisional++
		case ResultRejected:
			eval.Rejected++
		}
	}
	return eval, nil
}

func enforcePnpmControls(policy Policy, results []FindingResult) []FindingResult {
	byArtifact := map[string][]int{}
	for i, result := range results {
		if result.Finding.SourceKind != "pnpm_setting" {
			continue
		}
		byArtifact[result.Finding.Artifact] = append(byArtifact[result.Finding.Artifact], i)
	}

	reject := func(idx int, reason string) {
		results[idx].PolicyResult = ResultRejected
		results[idx].PolicyReason = reason
	}
	appendMissing := func(artifact, reason string) {
		results = append(results, FindingResult{
			Finding: Finding{
				ID:         MakeID(PnpmWorkspacePath, 0, "pnpm_setting", artifact),
				SourcePath: PnpmWorkspacePath,
				SourceKind: "pnpm_setting",
				Surface:    "developer-only",
				Artifact:   artifact,
			},
			PolicyResult: ResultRejected,
			PolicyReason: reason,
		})
	}
	requireExact := func(artifact, want string) {
		indexes := byArtifact[artifact]
		if len(indexes) == 0 {
			appendMissing(artifact, "required pnpm hardening setting is missing")
			return
		}
		for _, idx := range indexes {
			got := strings.TrimPrefix(results[idx].Finding.Digest, "value:")
			if got != want {
				reject(idx, fmt.Sprintf("required pnpm hardening setting %s=%s, observed %s", artifact, want, got))
			}
		}
	}

	requireExact("pnpm.strictDepBuilds", "true")
	requireExact("pnpm.dangerouslyAllowAllBuilds", "false")
	requireExact("pnpm.verifyDepsBeforeRun", "error")
	requireExact("pnpm.packageManagerStrict", "true")
	requireExact("pnpm.packageManagerStrictVersion", "true")

	minimumAgeArtifact := "pnpm.minimumReleaseAge"
	minimumAgeIndexes := byArtifact[minimumAgeArtifact]
	if len(minimumAgeIndexes) == 0 {
		appendMissing(minimumAgeArtifact, "required pnpm dependency age quarantine setting is missing")
	} else {
		minimumMinutes := int(policy.Requirements.MinimumAgeDays) * 24 * 60
		for _, idx := range minimumAgeIndexes {
			gotRaw := strings.TrimPrefix(results[idx].Finding.Digest, "value:")
			got, err := strconv.Atoi(gotRaw)
			if err != nil {
				reject(idx, fmt.Sprintf("pnpm.minimumReleaseAge is not an integer minute count: %s", gotRaw))
				continue
			}
			if got < minimumMinutes {
				reject(idx, fmt.Sprintf("pnpm.minimumReleaseAge=%d below policy minimum %d", got, minimumMinutes))
			}
		}
	}

	allowTrue := map[string]int{}
	allowFalse := map[string]int{}
	onlyBuilt := map[string]int{}
	for artifact, indexes := range byArtifact {
		if len(indexes) == 0 {
			continue
		}
		idx := indexes[0]
		switch {
		case strings.HasPrefix(artifact, "pnpm.allowBuilds."):
			matcher := strings.TrimPrefix(artifact, "pnpm.allowBuilds.")
			value := strings.TrimPrefix(results[idx].Finding.Digest, "value:")
			switch value {
			case "true":
				allowTrue[matcher] = idx
			case "false":
				allowFalse[matcher] = idx
			default:
				reject(idx, fmt.Sprintf("pnpm allowBuilds matcher %s has unsupported value %s", matcher, value))
			}
		case strings.HasPrefix(artifact, "pnpm.onlyBuiltDependencies."):
			matcher := strings.TrimPrefix(artifact, "pnpm.onlyBuiltDependencies.")
			if results[idx].Finding.Digest != "listed" {
				reject(idx, fmt.Sprintf("pnpm onlyBuiltDependencies matcher %s has unsupported marker %s", matcher, results[idx].Finding.Digest))
			}
			onlyBuilt[matcher] = idx
		}
	}
	for matcher, idx := range allowTrue {
		if _, ok := onlyBuilt[matcher]; !ok {
			reject(idx, fmt.Sprintf("pnpm allowBuilds permits %s but rules_js onlyBuiltDependencies omits it", matcher))
		}
	}
	for matcher, idx := range onlyBuilt {
		if _, ok := allowTrue[matcher]; !ok {
			reject(idx, fmt.Sprintf("rules_js onlyBuiltDependencies lists %s without matching pnpm allowBuilds permission", matcher))
		}
	}
	for matcher, allowIdx := range allowFalse {
		if onlyIdx, ok := onlyBuilt[matcher]; ok {
			reject(allowIdx, fmt.Sprintf("pnpm allowBuilds denies %s but rules_js onlyBuiltDependencies lists it", matcher))
			reject(onlyIdx, fmt.Sprintf("rules_js onlyBuiltDependencies lists %s despite pnpm allowBuilds denial", matcher))
		}
	}
	return results
}

func isAcceptedPolicyControl(f Finding, artifact PolicyArtifact, policy Policy) bool {
	if f.SourceKind == "pnpm_setting" {
		return true
	}
	if f.SourceKind == "registry_url" {
		return isLocalRegistryURL(f.UpstreamURL) || isVerdaccioUpstreamRegistry(f)
	}
	if (f.SourceKind == "curl_fetch" || f.SourceKind == "wget_fetch") && strings.Contains(f.SourcePath, "/scripts/security/") {
		return true
	}
	if !sourceRequiresAdmission(f.SourceKind) {
		return artifact.Admission.State == AdmissionProvisional || artifact.Admission.State == AdmissionAdmitted
	}
	_ = policy
	return false
}

func isLocalRegistryURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "http://127.0.0.1:") ||
		strings.HasPrefix(raw, "https://127.0.0.1:") ||
		strings.HasPrefix(raw, "http://localhost:") ||
		strings.HasPrefix(raw, "https://localhost:")
}

func isVerdaccioUpstreamRegistry(f Finding) bool {
	return strings.Contains(f.SourcePath, "/verdaccio/templates/") &&
		f.Artifact == "npmjs-registry" &&
		strings.TrimRight(f.UpstreamURL, "/") == "https://registry.npmjs.org"
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
			if artifact.Admission.StorageURI == "" {
				return fmt.Errorf("artifact %s admitted without storage_uri", artifact.ID)
			}
			if artifact.Admission.OCIRepository == "" || artifact.Admission.OCIManifestDigest == "" || artifact.Admission.OCIMediaType == "" {
				return fmt.Errorf("artifact %s admitted without complete OCI metadata", artifact.ID)
			}
			if artifact.Admission.SignatureDigest == "" || artifact.Admission.AttestationDigest == "" {
				return fmt.Errorf("artifact %s admitted without signature and attestation digests", artifact.ID)
			}
			if artifact.Admission.SBOMDigest == "" || artifact.Admission.ProvenanceDigest == "" || artifact.Admission.ScannerResultDigest == "" {
				return fmt.Errorf("artifact %s admitted without SBOM, provenance, and scanner evidence digests", artifact.ID)
			}
			if artifact.Admission.MinimumAgeResult != "passed" || artifact.Admission.ScannerResults != "passed" {
				return fmt.Errorf("artifact %s admitted without passing age/scanner results", artifact.ID)
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

func sourceRequiresAdmission(sourceKind string) bool {
	switch sourceKind {
	case "bazel_http_file", "bazel_http_archive", "catalog_url", "bootstrap_url", "direct_release_url", "curl_fetch", "wget_fetch", "apt_get_update", "apt_get_install", "npm_install", "uv_tool_install", "uvx_from", "go_install", "pip_install":
		return true
	default:
		return false
	}
}
