package supplychain

import "sort"

type AdmittedCatalog struct {
	SchemaVersion int                       `json:"schema_version"`
	Artifacts     []AdmittedCatalogArtifact `json:"artifacts"`
}

type AdmittedCatalogArtifact struct {
	ID                    string `json:"id"`
	SourcePath            string `json:"source_path"`
	SourceKind            string `json:"source_kind"`
	Surface               string `json:"surface"`
	Artifact              string `json:"artifact"`
	UpstreamURL           string `json:"upstream_url"`
	InstallURL            string `json:"install_url"`
	Digest                string `json:"digest"`
	StorageURI            string `json:"storage_uri"`
	OCIRepository         string `json:"oci_repository"`
	OCIManifestDigest     string `json:"oci_manifest_digest"`
	OCIMediaType          string `json:"oci_media_type"`
	SignatureDigest       string `json:"signature_digest"`
	AttestationDigest     string `json:"attestation_digest"`
	SBOMDigest            string `json:"sbom_digest"`
	ProvenanceDigest      string `json:"provenance_digest"`
	ScannerResultDigest   string `json:"scanner_result_digest"`
	ScannerName           string `json:"scanner_name"`
	ScannerVersion        string `json:"scanner_version"`
	ScannerDatabaseDigest string `json:"scanner_database_digest"`
	GUACSubject           string `json:"guac_subject"`
}

func CatalogFromPolicy(policy Policy) AdmittedCatalog {
	artifacts := make([]AdmittedCatalogArtifact, 0, len(policy.Artifacts))
	for _, artifact := range policy.Artifacts {
		if artifact.Admission.State != AdmissionAdmitted || !sourceRequiresAdmission(artifact.SourceKind) {
			continue
		}
		artifacts = append(artifacts, AdmittedCatalogArtifact{
			ID:                    artifact.ID,
			SourcePath:            artifact.SourcePath,
			SourceKind:            artifact.SourceKind,
			Surface:               artifact.Surface,
			Artifact:              artifact.Artifact,
			UpstreamURL:           artifact.Admission.UpstreamURL,
			InstallURL:            artifact.Admission.InstallURL,
			Digest:                artifact.Admission.Digest,
			StorageURI:            artifact.Admission.StorageURI,
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
		})
	}
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Surface != artifacts[j].Surface {
			return artifacts[i].Surface < artifacts[j].Surface
		}
		if artifacts[i].SourcePath != artifacts[j].SourcePath {
			return artifacts[i].SourcePath < artifacts[j].SourcePath
		}
		return artifacts[i].Artifact < artifacts[j].Artifact
	})
	return AdmittedCatalog{
		SchemaVersion: 1,
		Artifacts:     artifacts,
	}
}
