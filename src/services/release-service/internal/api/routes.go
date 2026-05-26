package api

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/verself/release-service/internal/contractapi"
	"github.com/verself/release-service/internal/release"
	workloadauth "github.com/verself/service-runtime/workload"
)

var apiTracer = otel.Tracer("release-service/internal/api")

func RegisterInternalRoutes(api huma.API, svc *release.Service, installationID string) {
	register(api, contractapi.RegisterPackage.Descriptor, "Register a release package", registerPackage(svc, installationID))
	register(api, contractapi.CreateVersion.Descriptor, "Create a release version", createVersion(svc, installationID))
	register(api, contractapi.GetVersion.Descriptor, "Get a release version", getVersion(svc, installationID))
	register(api, contractapi.AttachArtifact.Descriptor, "Attach a release artifact", attachArtifact(svc, installationID))
	register(api, contractapi.AttachEvidence.Descriptor, "Attach release evidence", attachEvidence(svc, installationID))
	register(api, contractapi.Verify.Descriptor, "Verify a release", verifyRelease(svc, installationID))
	register(api, contractapi.Approve.Descriptor, "Approve a release", approveRelease(svc, installationID))
	register(api, contractapi.PromoteChannel.Descriptor, "Promote a release channel", promoteChannel(svc))
	register(api, contractapi.ListInstallOptions.Descriptor, "List release install options", listInstallOptions(svc))
	register(api, contractapi.CreatePublication.Descriptor, "Create release publication", createPublication(svc))
	register(api, contractapi.DispatchPublication.Descriptor, "Dispatch release publication", dispatchPublication(svc))
	register(api, contractapi.RecordReceipt.Descriptor, "Record publication receipt", recordReceipt(svc))
	register(api, contractapi.ReconcilePublication.Descriptor, "Reconcile release publication", reconcilePublication(svc))
}

func register[I, O any](api huma.API, desc contractapi.OperationDescriptor, summary string, handler func(context.Context, release.Principal, *I) (*O, error)) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		MaxBodyBytes:  desc.RequestBodyMaxBytes,
		Errors:        contractProblemStatuses(desc.Problems),
		Security:      []map[string][]string{{"mutualTLS": {}}},
		Extensions: map[string]any{
			"x-verself-contract": contractExtension(desc),
		},
	}
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := apiTracer.Start(ctx, "release.api."+desc.OperationID)
		defer span.End()
		span.SetAttributes(
			attribute.String("release.operation_id", desc.OperationID),
			attribute.String("release.audit_event", desc.Audit.Event),
			attribute.String("release.permission", desc.Authorization.Permission),
		)
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			err := unauthorized(ctx)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		out, err := handler(ctx, release.Principal{Actor: peerID.String()}, input)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		return out, nil
	})
}

func registerPackage(svc *release.Service, installationID string) func(context.Context, release.Principal, *contractapi.RegisterPackageInput) (*contractapi.PackageOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.RegisterPackageInput) (*contractapi.PackageOutput, error) {
		pkg, err := svc.RegisterPackage(ctx, principal, release.RegisterPackageRequest{
			OrgID:                input.Body.OrgID,
			PackageName:          input.Body.PackageName,
			PackageKind:          input.Body.PackageKind,
			RepoPath:             input.Body.RepoPath,
			BazelTarget:          input.Body.BazelTarget,
			OwnerTeam:            input.Body.OwnerTeam,
			DefaultVersionScheme: input.Body.DefaultVersionScheme,
			Visibility:           input.Body.Visibility,
			PolicyRef:            input.Body.PolicyRef,
		})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.PackageOutput{Body: contractapi.PackageOutputBody{Package: packageDTO(pkg, installationID)}}, nil
	}
}

func createVersion(svc *release.Service, installationID string) func(context.Context, release.Principal, *contractapi.CreateVersionInput) (*contractapi.VersionOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.CreateVersionInput) (*contractapi.VersionOutput, error) {
		packageID, err := parseUUID(input.PackageID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		version, err := svc.CreateVersion(ctx, principal, release.CreateVersionRequest{
			PackageID:        packageID,
			CanonicalVersion: input.Body.CanonicalVersion,
			VersionScheme:    input.Body.VersionScheme,
			VersionKind:      input.Body.VersionKind,
			ReleaseSequence:  input.Body.ReleaseSequence,
			SourceRepository: input.Body.SourceRepository,
			SourceCommit:     input.Body.SourceCommit,
			SourceRef:        input.Body.SourceRef,
		})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.VersionOutput{Body: contractapi.VersionOutputBody{Release: versionDTO(version, installationID, "")}}, nil
	}
}

func getVersion(svc *release.Service, installationID string) func(context.Context, release.Principal, *contractapi.ReleaseVersionPathInput) (*contractapi.VersionOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.ReleaseVersionPathInput) (*contractapi.VersionOutput, error) {
		releaseVersionID, err := parseUUID(input.ReleaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		result, err := svc.GetRelease(ctx, principal, releaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.VersionOutput{Body: contractapi.VersionOutputBody{Release: versionDTO(result.Version, installationID, result.Package.OrgID)}}, nil
	}
}

func attachArtifact(svc *release.Service, installationID string) func(context.Context, release.Principal, *contractapi.AttachArtifactInput) (*contractapi.ArtifactOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.AttachArtifactInput) (*contractapi.ArtifactOutput, error) {
		releaseVersionID, err := parseUUID(input.ReleaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		artifact, err := svc.AttachArtifact(ctx, principal, release.AttachArtifactRequest{
			ReleaseVersionID: releaseVersionID,
			ArtifactRole:     input.Body.ArtifactRole,
			PlatformOS:       input.Body.PlatformOS,
			PlatformArch:     input.Body.PlatformArch,
			OCIRepository:    input.Body.OCIRepository,
			OCIDigest:        input.Body.OCIDigest,
			OCIMediaType:     input.Body.OCIMediaType,
			OCISizeBytes:     input.Body.OCISizeBytes,
			RegistryURL:      input.Body.RegistryURL,
		})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.ArtifactOutput{Body: contractapi.ArtifactOutputBody{Artifact: artifactDTO(artifact, installationID)}}, nil
	}
}

func attachEvidence(svc *release.Service, installationID string) func(context.Context, release.Principal, *contractapi.AttachEvidenceInput) (*contractapi.EvidenceOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.AttachEvidenceInput) (*contractapi.EvidenceOutput, error) {
		releaseVersionID, err := parseUUID(input.ReleaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		artifactID, err := parseUUID(input.Body.ArtifactID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		evidence, err := svc.AttachEvidence(ctx, principal, release.AttachEvidenceRequest{
			ReleaseVersionID:  releaseVersionID,
			ArtifactID:        artifactID,
			EvidenceKind:      input.Body.EvidenceKind,
			PredicateType:     input.Body.PredicateType,
			SubjectDigest:     input.Body.SubjectDigest,
			DocumentDigest:    input.Body.DocumentDigest,
			OCIReferrerDigest: input.Body.OCIReferrerDigest,
		})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.EvidenceOutput{Body: contractapi.EvidenceOutputBody{Evidence: evidenceDTO(evidence, installationID)}}, nil
	}
}

func verifyRelease(svc *release.Service, installationID string) func(context.Context, release.Principal, *contractapi.VerifyReleaseInput) (*contractapi.VerificationOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.VerifyReleaseInput) (*contractapi.VerificationOutput, error) {
		releaseVersionID, err := parseUUID(input.ReleaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		result, err := svc.Verify(ctx, principal, release.VerifyRequest{
			ReleaseVersionID: releaseVersionID,
			PolicyRef:        input.Body.PolicyRef,
			PolicyDigest:     input.Body.PolicyDigest,
			BuilderID:        input.Body.BuilderID,
			SignerIdentity:   input.Body.SignerIdentity,
		})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.VerificationOutput{Body: contractapi.VerificationOutputBody{Release: versionDTO(result.Version, installationID, ""), Decision: result.Decision, Reason: result.Reason}}, nil
	}
}

func approveRelease(svc *release.Service, installationID string) func(context.Context, release.Principal, *contractapi.ApproveReleaseInput) (*contractapi.VersionOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.ApproveReleaseInput) (*contractapi.VersionOutput, error) {
		releaseVersionID, err := parseUUID(input.ReleaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		version, err := svc.Approve(ctx, principal, release.ApproveRequest{ReleaseVersionID: releaseVersionID, ActorID: input.Body.ActorID, Reason: input.Body.Reason})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.VersionOutput{Body: contractapi.VersionOutputBody{Release: versionDTO(version, installationID, "")}}, nil
	}
}

func promoteChannel(svc *release.Service) func(context.Context, release.Principal, *contractapi.PromoteChannelInput) (*contractapi.ChannelOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.PromoteChannelInput) (*contractapi.ChannelOutput, error) {
		packageID, err := parseUUID(input.PackageID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		releaseVersionID, err := parseUUID(input.Body.ReleaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		channel, err := svc.PromoteChannel(ctx, principal, release.PromoteChannelRequest{
			PackageID:        packageID,
			ChannelName:      input.ChannelName,
			ReleaseVersionID: releaseVersionID,
			ChannelKind:      input.Body.ChannelKind,
			Visibility:       input.Body.Visibility,
			PolicyRef:        input.Body.PolicyRef,
			ActorID:          input.Body.ActorID,
			Reason:           input.Body.Reason,
		})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.ChannelOutput{Body: contractapi.ChannelOutputBody{ChannelID: contractapi.ReleaseChannelID(channel.ChannelID.String()), CurrentReleaseVersionID: contractapi.ReleaseVersionID(channel.CurrentReleaseVersionID.String()), ChannelName: channel.ChannelName}}, nil
	}
}

func listInstallOptions(svc *release.Service) func(context.Context, release.Principal, *contractapi.ReleaseVersionPathInput) (*contractapi.InstallOptionsOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.ReleaseVersionPathInput) (*contractapi.InstallOptionsOutput, error) {
		releaseVersionID, err := parseUUID(input.ReleaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		options, err := svc.ListInstallOptions(ctx, principal, releaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.InstallOptionsOutput{Body: contractapi.InstallOptionsOutputBody{InstallOptions: installOptionDTOs(options)}}, nil
	}
}

func createPublication(svc *release.Service) func(context.Context, release.Principal, *contractapi.CreatePublicationInput) (*contractapi.PublicationOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.CreatePublicationInput) (*contractapi.PublicationOutput, error) {
		releaseVersionID, err := parseUUID(input.ReleaseVersionID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		publication, err := svc.CreatePublication(ctx, principal, release.CreatePublicationRequest{
			ReleaseVersionID: releaseVersionID,
			ProviderKind:     input.Body.ProviderKind,
			ProviderPackage:  input.Body.ProviderPackage,
			ProviderChannel:  input.Body.ProviderChannel,
			RequestedBy:      input.Body.RequestedBy,
			IdempotencyKey:   string(input.IdempotencyKey),
		})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.PublicationOutput{Body: contractapi.PublicationOutputBody{Publication: publicationDTO(publication)}}, nil
	}
}

func dispatchPublication(svc *release.Service) func(context.Context, release.Principal, *contractapi.PublicationPathInput) (*contractapi.PublicationOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.PublicationPathInput) (*contractapi.PublicationOutput, error) {
		publicationID, err := parseUUID(input.PublicationID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		publication, err := svc.DispatchPublication(ctx, principal, publicationID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.PublicationOutput{Body: contractapi.PublicationOutputBody{Publication: publicationDTO(publication)}}, nil
	}
}

func recordReceipt(svc *release.Service) func(context.Context, release.Principal, *contractapi.RecordReceiptInput) (*contractapi.PublicationOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.RecordReceiptInput) (*contractapi.PublicationOutput, error) {
		publicationID, err := parseUUID(input.PublicationID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		publication, err := svc.RecordReceipt(ctx, principal, release.RecordReceiptRequest{
			PublicationID:       publicationID,
			ProviderResourceURL: input.Body.ProviderResourceURL,
			ProviderVersion:     input.Body.ProviderVersion,
			ProviderDigest:      input.Body.ProviderDigest,
			ReceiptDigest:       input.Body.ReceiptDigest,
		})
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.PublicationOutput{Body: contractapi.PublicationOutputBody{Publication: publicationDTO(publication)}}, nil
	}
}

func reconcilePublication(svc *release.Service) func(context.Context, release.Principal, *contractapi.PublicationPathInput) (*contractapi.PublicationOutput, error) {
	return func(ctx context.Context, principal release.Principal, input *contractapi.PublicationPathInput) (*contractapi.PublicationOutput, error) {
		publicationID, err := parseUUID(input.PublicationID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		publication, err := svc.ReconcilePublication(ctx, principal, publicationID)
		if err != nil {
			return nil, releaseError(ctx, err)
		}
		return &contractapi.PublicationOutput{Body: contractapi.PublicationOutputBody{Publication: publicationDTO(publication)}}, nil
	}
}

func parseUUID[T ~string](value T) (uuid.UUID, error) {
	id, err := uuid.Parse(string(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid UUID", release.ErrInvalid)
	}
	return id, nil
}

func packageDTO(pkg release.Package, installationID string) contractapi.PackageRecord {
	return contractapi.PackageRecord{
		PackageID:            contractapi.ReleasePackageID(pkg.PackageID.String()),
		ResourceName:         resourceName(installationID, pkg.OrgID, "releasePackages", pkg.PackageID),
		OrgID:                pkg.OrgID,
		PackageName:          pkg.PackageName,
		PackageKind:          pkg.PackageKind,
		RepoPath:             pkg.RepoPath,
		BazelTarget:          pkg.BazelTarget,
		OwnerTeam:            pkg.OwnerTeam,
		DefaultVersionScheme: pkg.DefaultVersionScheme,
		Visibility:           pkg.Visibility,
		PolicyRef:            pkg.PolicyRef,
		CreatedAt:            formatTime(pkg.CreatedAt),
		UpdatedAt:            formatTime(pkg.UpdatedAt),
	}
}

func versionDTO(version release.Version, installationID string, orgID string) contractapi.VersionRecord {
	return contractapi.VersionRecord{
		ReleaseVersionID: contractapi.ReleaseVersionID(version.ReleaseVersionID.String()),
		ResourceName:     resourceName(installationID, orgID, "releaseVersions", version.ReleaseVersionID),
		PackageID:        contractapi.ReleasePackageID(version.PackageID.String()),
		CanonicalVersion: version.CanonicalVersion,
		VersionScheme:    version.VersionScheme,
		VersionKind:      version.VersionKind,
		ReleaseSequence:  version.ReleaseSequence,
		SourceRepository: version.SourceRepository,
		SourceCommit:     version.SourceCommit,
		SourceRef:        version.SourceRef,
		State:            version.State,
		LifecycleStatus:  version.LifecycleStatus,
		CreatedAt:        formatTime(version.CreatedAt),
		UpdatedAt:        formatTime(version.UpdatedAt),
	}
}

func artifactDTO(artifact release.Artifact, _ string) contractapi.ArtifactRecord {
	return contractapi.ArtifactRecord{
		ArtifactID:       contractapi.ReleaseArtifactID(artifact.ArtifactID.String()),
		ReleaseVersionID: contractapi.ReleaseVersionID(artifact.ReleaseVersionID.String()),
		ArtifactRole:     artifact.ArtifactRole,
		PlatformOS:       artifact.PlatformOS,
		PlatformArch:     artifact.PlatformArch,
		OCIRepository:    artifact.OCIRepository,
		OCIDigest:        artifact.OCIDigest,
		OCIMediaType:     artifact.OCIMediaType,
		OCISizeBytes:     artifact.OCISizeBytes,
		RegistryURL:      artifact.RegistryURL,
		CreatedAt:        formatTime(artifact.CreatedAt),
	}
}

func evidenceDTO(evidence release.Evidence, _ string) contractapi.EvidenceRecord {
	return contractapi.EvidenceRecord{
		EvidenceID:        contractapi.ReleaseEvidenceID(evidence.EvidenceID.String()),
		ReleaseVersionID:  contractapi.ReleaseVersionID(evidence.ReleaseVersionID.String()),
		ArtifactID:        contractapi.ReleaseArtifactID(evidence.ArtifactID.String()),
		EvidenceKind:      evidence.EvidenceKind,
		PredicateType:     evidence.PredicateType,
		SubjectDigest:     evidence.SubjectDigest,
		DocumentDigest:    evidence.DocumentDigest,
		OCIReferrerDigest: evidence.OCIReferrerDigest,
		VerificationState: evidence.VerificationState,
		CreatedAt:         formatTime(evidence.CreatedAt),
	}
}

func installOptionDTOs(options []release.InstallOption) []contractapi.InstallOptionRecord {
	out := make([]contractapi.InstallOptionRecord, 0, len(options))
	for _, option := range options {
		out = append(out, contractapi.InstallOptionRecord{
			InstallOptionID:      contractapi.ReleaseInstallOptionID(option.InstallOptionID.String()),
			ReleaseVersionID:     contractapi.ReleaseVersionID(option.ReleaseVersionID.String()),
			OptionKind:           option.OptionKind,
			Status:               option.Status,
			DisplayOrder:         option.DisplayOrder,
			CommandTemplate:      option.CommandTemplate,
			VerificationTemplate: option.VerificationTemplate,
			OCIRepository:        option.OCIRepository,
			OCIDigest:            option.OCIDigest,
			ProviderKind:         option.ProviderKind,
			ProviderRef:          option.ProviderRef,
		})
	}
	return out
}

func publicationDTO(publication release.Publication) contractapi.PublicationRecord {
	return contractapi.PublicationRecord{
		PublicationID:    contractapi.ReleasePublicationID(publication.PublicationID.String()),
		ReleaseVersionID: contractapi.ReleaseVersionID(publication.ReleaseVersionID.String()),
		ProviderKind:     publication.ProviderKind,
		ProviderPackage:  publication.ProviderPackage,
		ProviderChannel:  publication.ProviderChannel,
		RequestedBy:      publication.RequestedBy,
		State:            publication.State,
		IdempotencyKey:   publication.IdempotencyKey,
		CreatedAt:        formatTime(publication.CreatedAt),
		UpdatedAt:        formatTime(publication.UpdatedAt),
	}
}

func resourceName(installationID string, orgID string, collection string, id uuid.UUID) contractapi.ResourceName {
	if orgID == "" {
		orgID = "internal"
	}
	return contractapi.ResourceName(fmt.Sprintf("urn:verself:%s:orgs/%s/%s/%s", installationID, orgID, collection, id.String()))
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func contractExtension(desc contractapi.OperationDescriptor) map[string]any {
	return map[string]any{
		"shape_id":               desc.ShapeID,
		"operation_id":           desc.OperationID,
		"identity":               desc.Identity.Mode,
		"audience":               desc.Identity.Audience,
		"permission":             desc.Authorization.Permission,
		"organization_source":    desc.Authorization.OrganizationSource,
		"organization_member":    desc.Authorization.OrganizationMember,
		"audit_event":            desc.Audit.Event,
		"resource":               desc.Audit.Resource,
		"action":                 desc.Audit.Action,
		"rate_limit_bucket":      desc.RateLimitBucket,
		"request_body_max_bytes": desc.RequestBodyMaxBytes,
		"idempotency":            desc.Idempotency.Policy,
	}
}

func contractProblemStatuses(problems []contractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	sort.Ints(statuses)
	out := statuses[:0]
	previous := 0
	for _, status := range statuses {
		if status == previous {
			continue
		}
		out = append(out, status)
		previous = status
	}
	return out
}
