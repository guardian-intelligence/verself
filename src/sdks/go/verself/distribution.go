package verself

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	distributioncore "github.com/verself/verself-go/internal/transport/distribution"
)

const (
	DistributionProductMksk       = "mksk"
	DistributionProductVerselfCLI = "verself-cli"

	DistributionChannelStable = "stable"
	DistributionChannelCanary = "canary"

	DistributionFlavorDefault = "default"
)

type DistributionEvidence struct {
	EvidenceKind      string    `json:"evidence_kind"`
	PredicateType     string    `json:"predicate_type"`
	SubjectDigest     string    `json:"subject_digest"`
	DocumentDigest    string    `json:"document_digest"`
	OCIReferrerDigest string    `json:"oci_referrer_digest"`
	CreatedAt         time.Time `json:"created_at"`
}

type DistributionVerification struct {
	Decision         string                 `json:"decision"`
	Reason           string                 `json:"reason"`
	BuilderID        string                 `json:"builder_id"`
	SignerIdentity   string                 `json:"signer_identity"`
	SourceRepository string                 `json:"source_repository"`
	SourceCommit     string                 `json:"source_commit"`
	SourceRef        string                 `json:"source_ref"`
	Evidence         []DistributionEvidence `json:"evidence"`
	CheckedAt        time.Time              `json:"checked_at"`
}

type DistributionReplication struct {
	State             string    `json:"state"`
	OriginRegistryURL string    `json:"origin_registry_url"`
	PublicRegistryURL string    `json:"public_registry_url"`
	CheckedAt         time.Time `json:"checked_at"`
}

type DistributionArtifact struct {
	ArtifactDigest     string                   `json:"artifact_digest"`
	ArtifactDigestRef  string                   `json:"artifact_digest_ref"`
	ResourceName       string                   `json:"resourceName"`
	PackageName        string                   `json:"package_name"`
	PackageVersion     string                   `json:"package_version"`
	PlatformOS         string                   `json:"platform_os"`
	PlatformArch       string                   `json:"platform_arch"`
	Flavor             string                   `json:"flavor"`
	OCIRepository      string                   `json:"oci_repository"`
	PublicOCIReference string                   `json:"public_oci_reference"`
	OCIMediaType       string                   `json:"oci_media_type"`
	OCISizeBytes       int64                    `json:"oci_size_bytes"`
	State              string                   `json:"state"`
	RetentionClass     string                   `json:"retention_class"`
	Verification       DistributionVerification `json:"verification"`
	Replication        DistributionReplication  `json:"replication"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
	QuarantinedAt      *time.Time               `json:"quarantined_at,omitempty"`
	QuarantineReason   string                   `json:"quarantine_reason,omitempty"`
}

type DistributionTarget struct {
	ResourceName       string     `json:"resourceName"`
	PackageName        string     `json:"package_name"`
	ChannelName        string     `json:"channel_name"`
	PlatformOS         string     `json:"platform_os"`
	PlatformArch       string     `json:"platform_arch"`
	Flavor             string     `json:"flavor"`
	ArtifactDigest     string     `json:"artifact_digest"`
	ArtifactDigestRef  string     `json:"artifact_digest_ref"`
	PackageVersion     string     `json:"package_version"`
	State              string     `json:"state"`
	PublicOCIReference string     `json:"public_oci_reference"`
	DownloadURL        string     `json:"download_url"`
	PublishedAt        time.Time  `json:"published_at"`
	SupersededAt       *time.Time `json:"superseded_at,omitempty"`
	SupersededByDigest string     `json:"superseded_by_digest,omitempty"`
}

type DistributionUpdateCheck struct {
	UpdateAvailable bool               `json:"update_available"`
	Target          DistributionTarget `json:"target"`
	Traceparent     string             `json:"traceparent"`
}

type DistributionChannelTargets struct {
	Targets       []DistributionTarget `json:"targets"`
	NextPageToken string               `json:"next_page_token,omitempty"`
	Traceparent   string               `json:"traceparent"`
}

type DistributionCheckUpdateInput struct {
	PackageName      string
	ChannelName      string
	PlatformOS       string
	PlatformArch     string
	Flavor           string
	InstalledDigest  string
	InstalledVersion string
}

type DistributionResolveTargetInput struct {
	PackageName  string
	ChannelName  string
	PlatformOS   string
	PlatformArch string
	Flavor       string
}

type DistributionGetArtifactInput struct {
	ArtifactDigest string
}

type DistributionListTargetsOptions struct {
	PackageName  string
	ChannelName  string
	PlatformOS   string
	PlatformArch string
	Flavor       string
	PageSize     int32
	PageToken    string
}

type DistributionClient struct {
	client *distributioncore.Client
}

func (c *DistributionClient) CheckUpdate(ctx context.Context, input DistributionCheckUpdateInput) (DistributionUpdateCheck, error) {
	if c == nil || c.client == nil {
		return DistributionUpdateCheck{}, fmt.Errorf("verself sdk: distribution client is not initialized")
	}
	response, err := c.client.CheckUpdate(ctx, distributioncore.CheckUpdateRequest{
		PackageName:      strings.TrimSpace(input.PackageName),
		ChannelName:      strings.TrimSpace(input.ChannelName),
		PlatformOS:       strings.TrimSpace(input.PlatformOS),
		PlatformArch:     strings.TrimSpace(input.PlatformArch),
		Flavor:           strings.TrimSpace(input.Flavor),
		InstalledDigest:  strings.TrimSpace(input.InstalledDigest),
		InstalledVersion: strings.TrimSpace(input.InstalledVersion),
	})
	if err != nil {
		return DistributionUpdateCheck{}, err
	}
	if response.Result == nil {
		return DistributionUpdateCheck{}, distributionAPIError("check update", response.StatusCode, response.Problem, response.Body)
	}
	return DistributionUpdateCheck{
		UpdateAvailable: response.Result.UpdateAvailable,
		Target:          distributionTargetFromCore(response.Result.Target),
		Traceparent:     response.Result.Traceparent,
	}, nil
}

func (c *DistributionClient) ResolveTarget(ctx context.Context, input DistributionResolveTargetInput) (DistributionTarget, error) {
	if c == nil || c.client == nil {
		return DistributionTarget{}, fmt.Errorf("verself sdk: distribution client is not initialized")
	}
	response, err := c.client.ResolveTarget(ctx, distributioncore.ResolveTargetRequest{
		PackageName:  strings.TrimSpace(input.PackageName),
		ChannelName:  strings.TrimSpace(input.ChannelName),
		PlatformOS:   strings.TrimSpace(input.PlatformOS),
		PlatformArch: strings.TrimSpace(input.PlatformArch),
		Flavor:       strings.TrimSpace(input.Flavor),
	})
	if err != nil {
		return DistributionTarget{}, err
	}
	if response.Result == nil {
		return DistributionTarget{}, distributionAPIError("resolve target", response.StatusCode, response.Problem, response.Body)
	}
	return distributionTargetFromCore(response.Result.Target), nil
}

func (c *DistributionClient) GetArtifact(ctx context.Context, input DistributionGetArtifactInput) (DistributionArtifact, error) {
	if c == nil || c.client == nil {
		return DistributionArtifact{}, fmt.Errorf("verself sdk: distribution client is not initialized")
	}
	response, err := c.client.GetArtifact(ctx, distributioncore.GetArtifactRequest{
		ArtifactDigestRef: distributionDigestRef(input.ArtifactDigest),
	})
	if err != nil {
		return DistributionArtifact{}, err
	}
	if response.Result == nil {
		return DistributionArtifact{}, distributionAPIError("get artifact", response.StatusCode, response.Problem, response.Body)
	}
	return distributionArtifactFromCore(response.Result.Artifact), nil
}

func (c *DistributionClient) ListTargets(ctx context.Context, options DistributionListTargetsOptions) (DistributionChannelTargets, error) {
	if c == nil || c.client == nil {
		return DistributionChannelTargets{}, fmt.Errorf("verself sdk: distribution client is not initialized")
	}
	response, err := c.client.ListTargets(ctx, distributioncore.ListTargetsRequest{
		PackageName:  strings.TrimSpace(options.PackageName),
		ChannelName:  strings.TrimSpace(options.ChannelName),
		PlatformOS:   strings.TrimSpace(options.PlatformOS),
		PlatformArch: strings.TrimSpace(options.PlatformArch),
		Flavor:       strings.TrimSpace(options.Flavor),
		PageSize:     options.PageSize,
		PageToken:    strings.TrimSpace(options.PageToken),
	})
	if err != nil {
		return DistributionChannelTargets{}, err
	}
	if response.Result == nil {
		return DistributionChannelTargets{}, distributionAPIError("list targets", response.StatusCode, response.Problem, response.Body)
	}
	targets := make([]DistributionTarget, 0, len(response.Result.Targets))
	for _, target := range response.Result.Targets {
		targets = append(targets, distributionTargetFromCore(target))
	}
	return DistributionChannelTargets{
		Targets:       targets,
		NextPageToken: response.Result.NextPageToken,
		Traceparent:   response.Result.Traceparent,
	}, nil
}

func distributionTargetFromCore(input distributioncore.TargetRecord) DistributionTarget {
	return DistributionTarget{
		ResourceName:       input.ResourceName,
		PackageName:        input.PackageName,
		ChannelName:        input.ChannelName,
		PlatformOS:         input.PlatformOS,
		PlatformArch:       input.PlatformArch,
		Flavor:             input.Flavor,
		ArtifactDigest:     input.ArtifactDigest,
		ArtifactDigestRef:  input.ArtifactDigestRef,
		PackageVersion:     input.PackageVersion,
		State:              input.State,
		PublicOCIReference: input.PublicOCIReference,
		DownloadURL:        input.DownloadURL,
		PublishedAt:        parseSDKTime(input.PublishedAt),
		SupersededAt:       parseSDKTimePointer(input.SupersededAt),
		SupersededByDigest: stringFromPointer(input.SupersededByDigest),
	}
}

func distributionArtifactFromCore(input distributioncore.ArtifactRecord) DistributionArtifact {
	evidence := make([]DistributionEvidence, 0, len(input.Verification.Evidence))
	for _, item := range input.Verification.Evidence {
		evidence = append(evidence, DistributionEvidence{
			EvidenceKind:      item.EvidenceKind,
			PredicateType:     item.PredicateType,
			SubjectDigest:     item.SubjectDigest,
			DocumentDigest:    item.DocumentDigest,
			OCIReferrerDigest: item.OCIReferrerDigest,
			CreatedAt:         parseSDKTime(item.CreatedAt),
		})
	}
	return DistributionArtifact{
		ArtifactDigest:     input.ArtifactDigest,
		ArtifactDigestRef:  input.ArtifactDigestRef,
		ResourceName:       input.ResourceName,
		PackageName:        input.PackageName,
		PackageVersion:     input.PackageVersion,
		PlatformOS:         input.PlatformOS,
		PlatformArch:       input.PlatformArch,
		Flavor:             input.Flavor,
		OCIRepository:      input.OCIRepository,
		PublicOCIReference: input.PublicOCIReference,
		OCIMediaType:       input.OCIMediaType,
		OCISizeBytes:       input.OCISizeBytes,
		State:              input.State,
		RetentionClass:     input.RetentionClass,
		Verification: DistributionVerification{
			Decision:         input.Verification.Decision,
			Reason:           input.Verification.Reason,
			BuilderID:        input.Verification.BuilderID,
			SignerIdentity:   input.Verification.SignerIdentity,
			SourceRepository: input.Verification.SourceRepository,
			SourceCommit:     input.Verification.SourceCommit,
			SourceRef:        input.Verification.SourceRef,
			Evidence:         evidence,
			CheckedAt:        parseSDKTime(input.Verification.CheckedAt),
		},
		Replication: DistributionReplication{
			State:             input.Replication.State,
			OriginRegistryURL: input.Replication.OriginRegistryURL,
			PublicRegistryURL: input.Replication.PublicRegistryURL,
			CheckedAt:         parseSDKTime(input.Replication.CheckedAt),
		},
		CreatedAt:        parseSDKTime(input.CreatedAt),
		UpdatedAt:        parseSDKTime(input.UpdatedAt),
		QuarantinedAt:    parseSDKTimePointer(input.QuarantinedAt),
		QuarantineReason: stringFromPointer(input.QuarantineReason),
	}
}

func distributionDigestRef(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "sha256:") {
		return "sha256-" + strings.TrimPrefix(trimmed, "sha256:")
	}
	return trimmed
}

func distributionAPIError(operation string, statusCode int, model *distributioncore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	metadata := apiErrorMetadata{}
	if model != nil {
		title = model.Title
		detail = model.Detail
		if model.Code != nil {
			metadata.Code = *model.Code
		}
		if model.RequestID != nil {
			metadata.RequestID = *model.RequestID
		}
		if model.Traceparent != nil {
			metadata.Traceparent = *model.Traceparent
		}
	}
	if title == nil || detail == nil {
		var problem struct {
			Title       *string `json:"title"`
			Detail      *string `json:"detail"`
			Code        *string `json:"code"`
			RequestID   *string `json:"requestId"`
			Traceparent *string `json:"traceparent"`
		}
		if len(body) > 0 {
			_ = json.Unmarshal(body, &problem)
		}
		if title == nil {
			title = problem.Title
		}
		if detail == nil {
			detail = problem.Detail
		}
		if metadata.Code == "" && problem.Code != nil {
			metadata.Code = *problem.Code
		}
		if metadata.RequestID == "" && problem.RequestID != nil {
			metadata.RequestID = *problem.RequestID
		}
		if metadata.Traceparent == "" && problem.Traceparent != nil {
			metadata.Traceparent = *problem.Traceparent
		}
	}
	return apiErrorFields("Distribution API", operation, statusCode, title, detail, body, metadata)
}
