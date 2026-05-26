package contractapi

import "context"

type OperationDescriptor struct {
	ShapeID             string
	OperationID         string
	Method              string
	Path                string
	DefaultStatus       int
	Readonly            bool
	Paginated           bool
	Identity            IdentityDescriptor
	Authorization       AuthorizationDescriptor
	Audit               AuditDescriptor
	RateLimitBucket     string
	RequestBodyMaxBytes int64
	Idempotency         IdempotencyDescriptor
	Problems            []ProblemDescriptor
}

type IdentityDescriptor struct {
	Mode       string
	Audience   string
	Principals []string
}

type AuthorizationDescriptor struct {
	Permission         string
	OrganizationSource string
	OrganizationMember string
}

type AuditDescriptor struct {
	Event    string
	Resource string
	Action   string
}

type IdempotencyDescriptor struct {
	Policy string
	Header string
	Member string
}

type ProblemDescriptor struct {
	ShapeID string
	Type    string
	Code    string
	Status  int
}

type Operation[Input any, Output any] struct {
	Descriptor OperationDescriptor
}

type Handler[Input any, Output any] func(context.Context, *Input) (*Output, error)

type (
	IdempotencyKey    string
	ResourceName      string
	ReleasePackageID  string
	ReleaseVersionID  string
	ReleaseArtifactID string
	ReleaseEvidenceID string
)

type (
	ReleaseInstallOptionID string
	ReleaseChannelID       string
	ReleasePublicationID   string
)

type RegisterPackageInputBody struct {
	OrgID                string `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	PackageName          string `json:"package_name" required:"true" minLength:"1" maxLength:"128"`
	PackageKind          string `json:"package_kind" required:"true" minLength:"1" maxLength:"64"`
	RepoPath             string `json:"repo_path" required:"true" minLength:"1" maxLength:"4096"`
	BazelTarget          string `json:"bazel_target" required:"true" minLength:"1" maxLength:"4096"`
	OwnerTeam            string `json:"owner_team" required:"true" minLength:"1" maxLength:"128"`
	DefaultVersionScheme string `json:"default_version_scheme" required:"true" minLength:"1" maxLength:"64"`
	Visibility           string `json:"visibility" required:"true" minLength:"1" maxLength:"128"`
	PolicyRef            string `json:"policy_ref" required:"true" minLength:"1" maxLength:"4096"`
}

type RegisterPackageInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           RegisterPackageInputBody
}

type CreateVersionInputBody struct {
	CanonicalVersion string `json:"canonical_version" required:"true" minLength:"1" maxLength:"128"`
	VersionScheme    string `json:"version_scheme" required:"true" minLength:"1" maxLength:"64"`
	VersionKind      string `json:"version_kind" required:"true" minLength:"1" maxLength:"64"`
	ReleaseSequence  int64  `json:"release_sequence" required:"true" minimum:"0"`
	SourceRepository string `json:"source_repository" required:"true" minLength:"1" maxLength:"4096"`
	SourceCommit     string `json:"source_commit" required:"true" minLength:"1" maxLength:"128"`
	SourceRef        string `json:"source_ref" required:"true" minLength:"1" maxLength:"4096"`
}

type CreateVersionInput struct {
	PackageID      ReleasePackageID `path:"package_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey   `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateVersionInputBody
}

type ReleaseVersionPathInput struct {
	ReleaseVersionID ReleaseVersionID `path:"release_version_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type AttachArtifactInputBody struct {
	ArtifactRole  string `json:"artifact_role" required:"true" minLength:"1" maxLength:"128"`
	PlatformOS    string `json:"platform_os" required:"true" minLength:"1" maxLength:"128"`
	PlatformArch  string `json:"platform_arch" required:"true" minLength:"1" maxLength:"128"`
	OCIRepository string `json:"oci_repository" required:"true" minLength:"1" maxLength:"4096"`
	OCIDigest     string `json:"oci_digest" required:"true" minLength:"1" maxLength:"4096"`
	OCIMediaType  string `json:"oci_media_type" required:"true" minLength:"1" maxLength:"4096"`
	OCISizeBytes  int64  `json:"oci_size_bytes" required:"true" minimum:"0"`
	RegistryURL   string `json:"registry_url" required:"true" minLength:"1" maxLength:"4096"`
}

type AttachArtifactInput struct {
	ReleaseVersionID ReleaseVersionID `path:"release_version_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey   IdempotencyKey   `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body             AttachArtifactInputBody
}

type AttachEvidenceInputBody struct {
	ArtifactID        ReleaseArtifactID `json:"artifact_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	EvidenceKind      string            `json:"evidence_kind" required:"true" minLength:"1" maxLength:"128"`
	PredicateType     string            `json:"predicate_type" required:"true" minLength:"1" maxLength:"4096"`
	SubjectDigest     string            `json:"subject_digest" required:"true" minLength:"1" maxLength:"4096"`
	DocumentDigest    string            `json:"document_digest" required:"true" minLength:"1" maxLength:"4096"`
	OCIReferrerDigest string            `json:"oci_referrer_digest" required:"true" minLength:"1" maxLength:"4096"`
}

type AttachEvidenceInput struct {
	ReleaseVersionID ReleaseVersionID `path:"release_version_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey   IdempotencyKey   `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body             AttachEvidenceInputBody
}

type VerifyReleaseInputBody struct {
	PolicyRef      string `json:"policy_ref" required:"true" minLength:"1" maxLength:"4096"`
	PolicyDigest   string `json:"policy_digest" required:"true" minLength:"1" maxLength:"4096"`
	BuilderID      string `json:"builder_id" required:"true" minLength:"1" maxLength:"4096"`
	SignerIdentity string `json:"signer_identity" required:"true" minLength:"1" maxLength:"4096"`
}

type VerifyReleaseInput struct {
	ReleaseVersionID ReleaseVersionID `path:"release_version_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey   IdempotencyKey   `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body             VerifyReleaseInputBody
}

type ApproveReleaseInputBody struct {
	ActorID string `json:"actor_id" required:"true" minLength:"1" maxLength:"4096"`
	Reason  string `json:"reason" required:"true" minLength:"1" maxLength:"4096"`
}

type ApproveReleaseInput struct {
	ReleaseVersionID ReleaseVersionID `path:"release_version_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey   IdempotencyKey   `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body             ApproveReleaseInputBody
}

type PromoteChannelInputBody struct {
	ReleaseVersionID ReleaseVersionID `json:"release_version_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ChannelKind      string           `json:"channel_kind" required:"true" minLength:"1" maxLength:"64"`
	Visibility       string           `json:"visibility" required:"true" minLength:"1" maxLength:"128"`
	PolicyRef        string           `json:"policy_ref" required:"true" minLength:"1" maxLength:"4096"`
	ActorID          string           `json:"actor_id" required:"true" minLength:"1" maxLength:"4096"`
	Reason           string           `json:"reason" required:"true" minLength:"1" maxLength:"4096"`
}

type PromoteChannelInput struct {
	PackageID      ReleasePackageID `path:"package_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ChannelName    string           `path:"channel_name" required:"true" minLength:"1" maxLength:"4096"`
	IdempotencyKey IdempotencyKey   `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           PromoteChannelInputBody
}

type CreatePublicationInputBody struct {
	ProviderKind    string `json:"provider_kind" required:"true" minLength:"1" maxLength:"128"`
	ProviderPackage string `json:"provider_package" required:"true" minLength:"1" maxLength:"4096"`
	ProviderChannel string `json:"provider_channel" required:"true" minLength:"1" maxLength:"4096"`
	RequestedBy     string `json:"requested_by" required:"true" minLength:"1" maxLength:"4096"`
}

type CreatePublicationInput struct {
	ReleaseVersionID ReleaseVersionID `path:"release_version_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey   IdempotencyKey   `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body             CreatePublicationInputBody
}

type PublicationPathInput struct {
	PublicationID  ReleasePublicationID `path:"publication_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey       `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type RecordReceiptInputBody struct {
	ProviderResourceURL string `json:"provider_resource_url" required:"true" minLength:"1" maxLength:"4096"`
	ProviderVersion     string `json:"provider_version" required:"true" minLength:"1" maxLength:"4096"`
	ProviderDigest      string `json:"provider_digest" required:"true" minLength:"1" maxLength:"4096"`
	ReceiptDigest       string `json:"receipt_digest" required:"true" minLength:"1" maxLength:"4096"`
}

type RecordReceiptInput struct {
	PublicationID  ReleasePublicationID `path:"publication_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	IdempotencyKey IdempotencyKey       `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           RecordReceiptInputBody
}

type PackageRecord struct {
	PackageID            ReleasePackageID `json:"package_id" required:"true"`
	ResourceName         ResourceName     `json:"resourceName" required:"true"`
	OrgID                string           `json:"org_id" required:"true"`
	PackageName          string           `json:"package_name" required:"true"`
	PackageKind          string           `json:"package_kind" required:"true"`
	RepoPath             string           `json:"repo_path" required:"true"`
	BazelTarget          string           `json:"bazel_target" required:"true"`
	OwnerTeam            string           `json:"owner_team" required:"true"`
	DefaultVersionScheme string           `json:"default_version_scheme" required:"true"`
	Visibility           string           `json:"visibility" required:"true"`
	PolicyRef            string           `json:"policy_ref" required:"true"`
	CreatedAt            string           `json:"created_at" required:"true"`
	UpdatedAt            string           `json:"updated_at" required:"true"`
}

type VersionRecord struct {
	ReleaseVersionID ReleaseVersionID `json:"release_version_id" required:"true"`
	ResourceName     ResourceName     `json:"resourceName" required:"true"`
	PackageID        ReleasePackageID `json:"package_id" required:"true"`
	CanonicalVersion string           `json:"canonical_version" required:"true"`
	VersionScheme    string           `json:"version_scheme" required:"true"`
	VersionKind      string           `json:"version_kind" required:"true"`
	ReleaseSequence  int64            `json:"release_sequence" required:"true"`
	SourceRepository string           `json:"source_repository" required:"true"`
	SourceCommit     string           `json:"source_commit" required:"true"`
	SourceRef        string           `json:"source_ref" required:"true"`
	State            string           `json:"state" required:"true"`
	LifecycleStatus  string           `json:"lifecycle_status" required:"true"`
	CreatedAt        string           `json:"created_at" required:"true"`
	UpdatedAt        string           `json:"updated_at" required:"true"`
}

type ArtifactRecord struct {
	ArtifactID       ReleaseArtifactID `json:"artifact_id" required:"true"`
	ReleaseVersionID ReleaseVersionID  `json:"release_version_id" required:"true"`
	ArtifactRole     string            `json:"artifact_role" required:"true"`
	PlatformOS       string            `json:"platform_os" required:"true"`
	PlatformArch     string            `json:"platform_arch" required:"true"`
	OCIRepository    string            `json:"oci_repository" required:"true"`
	OCIDigest        string            `json:"oci_digest" required:"true"`
	OCIMediaType     string            `json:"oci_media_type" required:"true"`
	OCISizeBytes     int64             `json:"oci_size_bytes" required:"true"`
	RegistryURL      string            `json:"registry_url" required:"true"`
	CreatedAt        string            `json:"created_at" required:"true"`
}

type EvidenceRecord struct {
	EvidenceID        ReleaseEvidenceID `json:"evidence_id" required:"true"`
	ReleaseVersionID  ReleaseVersionID  `json:"release_version_id" required:"true"`
	ArtifactID        ReleaseArtifactID `json:"artifact_id" required:"true"`
	EvidenceKind      string            `json:"evidence_kind" required:"true"`
	PredicateType     string            `json:"predicate_type" required:"true"`
	SubjectDigest     string            `json:"subject_digest" required:"true"`
	DocumentDigest    string            `json:"document_digest" required:"true"`
	OCIReferrerDigest string            `json:"oci_referrer_digest" required:"true"`
	VerificationState string            `json:"verification_state" required:"true"`
	CreatedAt         string            `json:"created_at" required:"true"`
}

type InstallOptionRecord struct {
	InstallOptionID      ReleaseInstallOptionID `json:"install_option_id" required:"true"`
	ReleaseVersionID     ReleaseVersionID       `json:"release_version_id" required:"true"`
	OptionKind           string                 `json:"option_kind" required:"true"`
	Status               string                 `json:"status" required:"true"`
	DisplayOrder         int32                  `json:"display_order" required:"true"`
	CommandTemplate      string                 `json:"command_template" required:"true"`
	VerificationTemplate string                 `json:"verification_template" required:"true"`
	OCIRepository        string                 `json:"oci_repository" required:"true"`
	OCIDigest            string                 `json:"oci_digest" required:"true"`
	ProviderKind         string                 `json:"provider_kind" required:"true"`
	ProviderRef          string                 `json:"provider_ref" required:"true"`
}

type PublicationRecord struct {
	PublicationID    ReleasePublicationID `json:"publication_id" required:"true"`
	ReleaseVersionID ReleaseVersionID     `json:"release_version_id" required:"true"`
	ProviderKind     string               `json:"provider_kind" required:"true"`
	ProviderPackage  string               `json:"provider_package" required:"true"`
	ProviderChannel  string               `json:"provider_channel" required:"true"`
	RequestedBy      string               `json:"requested_by" required:"true"`
	State            string               `json:"state" required:"true"`
	IdempotencyKey   string               `json:"idempotency_key" required:"true"`
	CreatedAt        string               `json:"created_at" required:"true"`
	UpdatedAt        string               `json:"updated_at" required:"true"`
}

type PackageOutputBody struct {
	Package PackageRecord `json:"package" required:"true"`
}

type PackageOutput struct {
	Body PackageOutputBody
}

type VersionOutputBody struct {
	Release VersionRecord `json:"release" required:"true"`
}

type VersionOutput struct {
	Body VersionOutputBody
}

type ArtifactOutputBody struct {
	Artifact ArtifactRecord `json:"artifact" required:"true"`
}

type ArtifactOutput struct {
	Body ArtifactOutputBody
}

type EvidenceOutputBody struct {
	Evidence EvidenceRecord `json:"evidence" required:"true"`
}

type EvidenceOutput struct {
	Body EvidenceOutputBody
}

type VerificationOutputBody struct {
	Release  VersionRecord `json:"release" required:"true"`
	Decision string        `json:"decision" required:"true"`
	Reason   string        `json:"reason" required:"true"`
}

type VerificationOutput struct {
	Body VerificationOutputBody
}

type ChannelOutputBody struct {
	ChannelID               ReleaseChannelID `json:"channel_id" required:"true"`
	CurrentReleaseVersionID ReleaseVersionID `json:"current_release_version_id" required:"true"`
	ChannelName             string           `json:"channel_name" required:"true"`
}

type ChannelOutput struct {
	Body ChannelOutputBody
}

type InstallOptionsOutputBody struct {
	InstallOptions []InstallOptionRecord `json:"install_options" required:"true"`
}

type InstallOptionsOutput struct {
	Body InstallOptionsOutputBody
}

type PublicationOutputBody struct {
	Publication PublicationRecord `json:"publication" required:"true"`
}

type PublicationOutput struct {
	Body PublicationOutputBody
}

var Operations = []OperationDescriptor{
	RegisterPackage.Descriptor,
	CreateVersion.Descriptor,
	GetVersion.Descriptor,
	AttachArtifact.Descriptor,
	AttachEvidence.Descriptor,
	Verify.Descriptor,
	Approve.Descriptor,
	PromoteChannel.Descriptor,
	ListInstallOptions.Descriptor,
	CreatePublication.Descriptor,
	DispatchPublication.Descriptor,
	RecordReceipt.Descriptor,
	ReconcilePublication.Descriptor,
}

var (
	RegisterPackage      = Operation[RegisterPackageInput, PackageOutput]{Descriptor: descriptor("verself.release.v1#RegisterReleasePackage", "register-release-package", "POST", "/internal/v1/release/packages", 201, false, "release.package.register", "release_package", "create", "release:package:write", true)}
	CreateVersion        = Operation[CreateVersionInput, VersionOutput]{Descriptor: descriptor("verself.release.v1#CreateReleaseVersion", "create-release-version", "POST", "/internal/v1/release/packages/{package_id}/versions", 201, false, "release.version.create", "release_version", "create", "release:version:write", true)}
	GetVersion           = Operation[ReleaseVersionPathInput, VersionOutput]{Descriptor: descriptor("verself.release.v1#GetReleaseVersion", "get-release-version", "GET", "/internal/v1/release/versions/{release_version_id}", 200, true, "release.version.read", "release_version", "read", "release:version:read", false)}
	AttachArtifact       = Operation[AttachArtifactInput, ArtifactOutput]{Descriptor: descriptor("verself.release.v1#AttachReleaseArtifact", "attach-release-artifact", "POST", "/internal/v1/release/versions/{release_version_id}/artifacts", 201, false, "release.artifact.attach", "release_artifact", "write", "release:version:write", true)}
	AttachEvidence       = Operation[AttachEvidenceInput, EvidenceOutput]{Descriptor: descriptor("verself.release.v1#AttachReleaseEvidence", "attach-release-evidence", "POST", "/internal/v1/release/versions/{release_version_id}/evidence", 201, false, "release.evidence.attach", "release_evidence", "write", "release:version:write", true)}
	Verify               = Operation[VerifyReleaseInput, VerificationOutput]{Descriptor: descriptor("verself.release.v1#VerifyRelease", "verify-release", "POST", "/internal/v1/release/versions/{release_version_id}/verify", 200, false, "release.version.verify", "release_version", "verify", "release:version:write", true)}
	Approve              = Operation[ApproveReleaseInput, VersionOutput]{Descriptor: descriptor("verself.release.v1#ApproveRelease", "approve-release", "POST", "/internal/v1/release/versions/{release_version_id}/approve", 200, false, "release.version.approve", "release_version", "set", "release:version:write", true)}
	PromoteChannel       = Operation[PromoteChannelInput, ChannelOutput]{Descriptor: descriptor("verself.release.v1#PromoteReleaseChannel", "promote-release-channel", "POST", "/internal/v1/release/packages/{package_id}/channels/{channel_name}/promote", 200, false, "release.channel.promote", "release_channel", "set", "release:version:write", true)}
	ListInstallOptions   = Operation[ReleaseVersionPathInput, InstallOptionsOutput]{Descriptor: descriptor("verself.release.v1#ListReleaseInstallOptions", "list-release-install-options", "GET", "/internal/v1/release/versions/{release_version_id}/install-options", 200, true, "release.install_options.list", "release_install_option", "list", "release:version:read", false)}
	CreatePublication    = Operation[CreatePublicationInput, PublicationOutput]{Descriptor: descriptor("verself.release.v1#CreateReleasePublication", "create-release-publication", "POST", "/internal/v1/release/versions/{release_version_id}/publications", 201, false, "release.publication.create", "release_publication", "create", "release:publication:write", true)}
	DispatchPublication  = Operation[PublicationPathInput, PublicationOutput]{Descriptor: descriptor("verself.release.v1#DispatchReleasePublication", "dispatch-release-publication", "POST", "/internal/v1/release/publications/{publication_id}/dispatch", 200, false, "release.publication.dispatch", "release_publication", "sync", "release:publication:write", true)}
	RecordReceipt        = Operation[RecordReceiptInput, PublicationOutput]{Descriptor: descriptor("verself.release.v1#RecordReleasePublicationReceipt", "record-release-publication-receipt", "POST", "/internal/v1/release/publications/{publication_id}/receipts", 201, false, "release.publication.receipt.record", "release_publication", "write", "release:publication:write", true)}
	ReconcilePublication = Operation[PublicationPathInput, PublicationOutput]{Descriptor: descriptor("verself.release.v1#ReconcileReleasePublication", "reconcile-release-publication", "POST", "/internal/v1/release/publications/{publication_id}/reconcile", 200, false, "release.publication.reconcile", "release_publication", "sync", "release:publication:write", true)}
)

func descriptor(shapeID string, operationID string, method string, path string, status int, readonly bool, auditEvent string, resource string, action string, permission string, idempotent bool) OperationDescriptor {
	desc := OperationDescriptor{
		ShapeID:             shapeID,
		OperationID:         operationID,
		Method:              method,
		Path:                path,
		DefaultStatus:       status,
		Readonly:            readonly,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "release-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: permission, OrganizationSource: "request_id"},
		Audit:               AuditDescriptor{Event: auditEvent, Resource: resource, Action: action},
		RateLimitBucket:     "release_mutation",
		RequestBodyMaxBytes: 16384,
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
		},
	}
	if readonly {
		desc.RateLimitBucket = "read"
		desc.RequestBodyMaxBytes = 0
	}
	if idempotent {
		desc.Idempotency = IdempotencyDescriptor{Policy: "idempotency-key", Header: "Idempotency-Key"}
	}
	return desc
}
