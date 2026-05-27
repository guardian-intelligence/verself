package contractapi

import (
	"context"
)

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
	SDK                 SDKDescriptor
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

type SDKDescriptor struct {
	Module    string
	Method    string
	Paginated bool
	Retryable bool
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

type DisplayName string

type IdempotencyKey string

type PageSize int

type PageToken string

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type TraceParent string

type EmailAddress string

type MemberInviteAcceptanceToken string

type SignupIntentID string

type SignupStartStatus string

type SignupVerificationToken string

type AuthWarningCode string

type Password string

type LoginURL string

type SubjectId string

type BrowserRedirectPath string

type GivenName string

type FamilyName string

type MemberID string

type MemberResourceName string

type OrgID string

type OrgSlug string

type OrganizationResourceName string

type OrganizationVersion int

type PermissionName string

type PolicyEtag string

type PolicyVersion int

type IAMRoleName string

type IAMMemberName string

type Members []MemberSummary

type Organizations []OrganizationSummary

type Permissions []PermissionName

type IAMPolicyBindings []IAMPolicyBinding

type InviteMemberRoles []IAMRoleName

type MemberInvitationStatus string

type ConflictError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type IdempotencyPayloadMismatchError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type PermissionDeniedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type RateLimitedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ResourceNotFoundError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ServiceUnavailableError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type UnauthenticatedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ValidationFailedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type SignupVerificationInvalidError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type SignupVerificationExpiredError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type SignupVerificationAlreadyUsedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type SignupMaterializingError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type SignupStateConflictError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type SignupAccountExistsError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type OrganizationSlugUnavailableError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"2048"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type GetMemberInput struct {
	OrgID    OrgID    `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	MemberID MemberID `path:"memberId" required:"true" pattern:"^member_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type GetOrganizationInput struct {
	OrgID OrgID `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type ListMembersInput struct {
	OrgID     OrgID     `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	PageSize  PageSize  `query:"page_size" minimum:"1" maximum:"500"`
	PageToken PageToken `query:"page_token" minLength:"1" maxLength:"4096"`
}

type ListOrganizationsInput struct {
	PageSize  PageSize  `query:"page_size" minimum:"1" maximum:"500"`
	PageToken PageToken `query:"page_token" minLength:"1" maxLength:"4096"`
}

type CreateOrganizationInputBody struct {
	DisplayName DisplayName `json:"displayName" required:"true" minLength:"1" maxLength:"120"`
	Slug        *OrgSlug    `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
}

type CreateOrganizationInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateOrganizationInputBody
}

type MemberSummary struct {
	OrgID        OrgID              `json:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	MemberID     MemberID           `json:"memberId" required:"true" pattern:"^member_[0-9A-HJKMNP-TV-Z]{26}$"`
	ResourceName MemberResourceName `json:"resourceName" required:"true" pattern:"^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}/members/member_[0-9A-HJKMNP-TV-Z]{26}$"`
	Email        EmailAddress       `json:"email" required:"true" minLength:"3" maxLength:"320"`
	DisplayName  DisplayName        `json:"displayName" required:"true" minLength:"1" maxLength:"120"`
}

type MemberInvitationSummary struct {
	OrgID        OrgID                  `json:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	MemberID     MemberID               `json:"memberId" required:"true" pattern:"^member_[0-9A-HJKMNP-TV-Z]{26}$"`
	ResourceName MemberResourceName     `json:"resourceName" required:"true" pattern:"^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}/members/member_[0-9A-HJKMNP-TV-Z]{26}$"`
	Email        EmailAddress           `json:"email" required:"true" minLength:"3" maxLength:"320"`
	Status       MemberInvitationStatus `json:"status" required:"true" minLength:"1" maxLength:"32"`
	Roles        InviteMemberRoles      `json:"roles" required:"true" minItems:"1" maxItems:"8"`
}

type OrganizationSummary struct {
	OrgID        OrgID                    `json:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	ResourceName OrganizationResourceName `json:"resourceName" required:"true" pattern:"^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}$"`
	Slug         *OrgSlug                 `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName  DisplayName              `json:"displayName" required:"true" minLength:"1" maxLength:"120"`
	Version      OrganizationVersion      `json:"version" required:"true" minimum:"1" maximum:"2147483647"`
}

type IAMPolicy struct {
	Version  PolicyVersion     `json:"version" required:"true" minimum:"1" maximum:"2147483647"`
	Bindings IAMPolicyBindings `json:"bindings" required:"true"`
	Etag     *PolicyEtag       `json:"etag,omitempty" minLength:"1" maxLength:"256"`
}

type IAMPolicyBinding struct {
	Role    IAMRoleName     `json:"role" required:"true" minLength:"1" maxLength:"128"`
	Members []IAMMemberName `json:"members" required:"true"`
}

type UpdateOrganizationInputBody struct {
	Slug        *OrgSlug            `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	DisplayName *DisplayName        `json:"displayName,omitempty" minLength:"1" maxLength:"120"`
	Version     OrganizationVersion `json:"version" required:"true" minimum:"1" maximum:"2147483647"`
}

type UpdateOrganizationInput struct {
	OrgID          OrgID          `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           UpdateOrganizationInputBody
}

type GetIamPolicyInput struct {
	OrgID OrgID `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type SetIamPolicyInputBody struct {
	Policy IAMPolicy `json:"policy" required:"true"`
}

type SetIamPolicyInput struct {
	OrgID          OrgID          `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           SetIamPolicyInputBody
}

type TestIamPermissionsInputBody struct {
	Permissions Permissions `json:"permissions" required:"true"`
}

type TestIamPermissionsInput struct {
	OrgID OrgID `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	Body  TestIamPermissionsInputBody
}

type InviteMemberInputBody struct {
	Email      EmailAddress      `json:"email" required:"true" minLength:"3" maxLength:"320"`
	GivenName  *GivenName        `json:"givenName,omitempty" minLength:"1" maxLength:"100"`
	FamilyName *FamilyName       `json:"familyName,omitempty" minLength:"1" maxLength:"100"`
	Roles      InviteMemberRoles `json:"roles,omitempty" minItems:"1" maxItems:"8"`
}

type InviteMemberInput struct {
	OrgID          OrgID          `path:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           InviteMemberInputBody
}

type AcceptMemberInviteInputBody struct {
	AcceptanceToken MemberInviteAcceptanceToken `json:"acceptanceToken" required:"true" minLength:"32" maxLength:"512"`
}

type AcceptMemberInviteInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           AcceptMemberInviteInputBody
}

type CheckOrganizationSlugAvailabilityInput struct {
	Slug OrgSlug `path:"slug" required:"true" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
}

type StartSignupInputBody struct {
	Email                   EmailAddress `json:"email" required:"true" minLength:"3" maxLength:"320"`
	OrganizationDisplayName DisplayName  `json:"organizationDisplayName" required:"true" minLength:"1" maxLength:"120"`
	OrganizationSlug        *OrgSlug     `json:"organizationSlug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	GivenName               *GivenName   `json:"givenName,omitempty" minLength:"1" maxLength:"100"`
	FamilyName              *FamilyName  `json:"familyName,omitempty" minLength:"1" maxLength:"100"`
}

type StartSignupInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           StartSignupInputBody
}

type VerifySignupInputBody struct {
	VerificationToken       SignupVerificationToken `json:"verificationToken" required:"true" minLength:"32" maxLength:"512"`
	InitialPassword         Password                `json:"initialPassword" required:"true" minLength:"8" maxLength:"4096"`
	OrganizationDisplayName *DisplayName            `json:"organizationDisplayName,omitempty" minLength:"1" maxLength:"120"`
	OrganizationSlug        *OrgSlug                `json:"organizationSlug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
}

type VerifySignupInput struct {
	SignupIntentID SignupIntentID `path:"signupIntentId" required:"true" pattern:"^signup_[0-9A-HJKMNP-TV-Z]{26}$"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           VerifySignupInputBody
}

type ListOrganizationsOutputBody struct {
	Organizations Organizations `json:"organizations" required:"true"`
	NextPageToken *PageToken    `json:"nextPageToken,omitempty" minLength:"1" maxLength:"4096"`
}

type ListOrganizationsOutput struct {
	Body ListOrganizationsOutputBody
}

type CreateOrganizationOutput struct {
	Body OrganizationSummary
}

type GetOrganizationOutput struct {
	Body OrganizationSummary
}

type UpdateOrganizationOutput struct {
	Body OrganizationSummary
}

type ListMembersOutputBody struct {
	Members       Members    `json:"members" required:"true"`
	NextPageToken *PageToken `json:"nextPageToken,omitempty" minLength:"1" maxLength:"4096"`
}

type ListMembersOutput struct {
	Body ListMembersOutputBody
}

type GetMemberOutput struct {
	Body MemberSummary
}

type InviteMemberOutput struct {
	Body MemberInvitationSummary
}

type AcceptMemberInviteOutput struct {
	Body MemberInviteAcceptanceSummary
}

type OrganizationSlugAvailability struct {
	Slug      OrgSlug `json:"slug" required:"true" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	Available bool    `json:"available" required:"true"`
}

type CheckOrganizationSlugAvailabilityOutput struct {
	Body OrganizationSlugAvailability
}

type SignupStartResult struct {
	Message               string            `json:"message" required:"true"`
	Status                SignupStartStatus `json:"status" required:"true" minLength:"1" maxLength:"64"`
	VerificationExpiresAt string            `json:"verificationExpiresAt" required:"true"`
}

type StartSignupOutput struct {
	Body SignupStartResult
}

type VerifySignupResult struct {
	Organization OrganizationSummary         `json:"organization" required:"true"`
	LoginURL     LoginURL                    `json:"loginUrl" required:"true" minLength:"1" maxLength:"2048"`
	LoginIntent  *RequiredAccountLoginIntent `json:"loginIntent,omitempty"`
	Warnings     AuthWarnings                `json:"warnings,omitempty"`
}

type VerifySignupOutput struct {
	Body VerifySignupResult
}

type AuthWarning struct {
	Code    AuthWarningCode `json:"code" required:"true" minLength:"1" maxLength:"128" pattern:"^[a-z][a-z0-9_.]*$"`
	Message string          `json:"message" required:"true"`
}

type AuthWarnings []AuthWarning

type RequiredAccountLoginIntent struct {
	LoginURL        LoginURL            `json:"loginUrl" required:"true" minLength:"1" maxLength:"2048"`
	Purpose         string              `json:"purpose" required:"true" minLength:"1" maxLength:"64"`
	RequiredSubject SubjectId           `json:"requiredSubject" required:"true" minLength:"1" maxLength:"512"`
	RequiredEmail   EmailAddress        `json:"requiredEmail" required:"true" minLength:"3" maxLength:"320"`
	RequiredOrgID   OrgID               `json:"requiredOrgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	RedirectTo      BrowserRedirectPath `json:"redirectTo,omitempty" minLength:"1" maxLength:"2048"`
}

type MemberInviteAcceptanceSummary struct {
	OrgID        OrgID                       `json:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	MemberID     MemberID                    `json:"memberId" required:"true" pattern:"^member_[0-9A-HJKMNP-TV-Z]{26}$"`
	ResourceName MemberResourceName          `json:"resourceName" required:"true" pattern:"^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}/members/member_[0-9A-HJKMNP-TV-Z]{26}$"`
	LoginURL     LoginURL                    `json:"loginUrl" required:"true" minLength:"1" maxLength:"2048"`
	LoginIntent  *RequiredAccountLoginIntent `json:"loginIntent,omitempty"`
}

type GetIamPolicyOutput struct {
	Body IAMPolicy
}

type SetIamPolicyOutput struct {
	Body IAMPolicy
}

type TestIamPermissionsOutput struct {
	Body TestIamPermissionsOutputBody
}

type TestIamPermissionsOutputBody struct {
	Permissions Permissions `json:"permissions" required:"true"`
}

var Operations = []OperationDescriptor{
	GetAuthContext.Descriptor,
	CreateDeviceSession.Descriptor,
	ListDeviceSessions.Descriptor,
	RevokeDeviceSession.Descriptor,
	ListAccountConnections.Descriptor,
	RemoveAccountConnection.Descriptor,
	AcceptMemberInvite.Descriptor,
	CheckOrganizationSlugAvailability.Descriptor,
	StartSignup.Descriptor,
	VerifySignup.Descriptor,
	ListOrganizations.Descriptor,
	CreateOrganization.Descriptor,
	GetOrganization.Descriptor,
	UpdateOrganization.Descriptor,
	ListMembers.Descriptor,
	GetMember.Descriptor,
	InviteMember.Descriptor,
	GetIamPolicy.Descriptor,
	SetIamPolicy.Descriptor,
	TestIamPermissions.Descriptor,
}

var CheckOrganizationSlugAvailability = Operation[CheckOrganizationSlugAvailabilityInput, CheckOrganizationSlugAvailabilityOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#CheckOrganizationSlugAvailability",
		OperationID:         "check-organization-slug-availability",
		Method:              "GET",
		Path:                "/api/v1/organization-slugs/{slug}/availability",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "public", Audience: "verself-api", Principals: []string{"browser", "cli", "anonymous"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization_slug:check", OrganizationSource: "installation", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.organization_slug.check", Resource: "organization", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "orgs", Method: "checkSlugAvailability", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var AcceptMemberInvite = Operation[AcceptMemberInviteInput, AcceptMemberInviteOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#AcceptMemberInvite",
		OperationID:         "accept-member-invite",
		Method:              "POST",
		Path:                "/api/v1/auth/invites/accept",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "public", Audience: "verself-api", Principals: []string{"browser", "cli", "anonymous"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:member_invite:accept", OrganizationSource: "installation", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.member_invite.accept", Resource: "member", Action: "verify"},
		RateLimitBucket:     "signup_mutation",
		RequestBodyMaxBytes: 16384,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "auth", Method: "acceptMemberInvite", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var StartSignup = Operation[StartSignupInput, StartSignupOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#StartSignup",
		OperationID:         "start-signup",
		Method:              "POST",
		Path:                "/api/v1/signup-intents",
		DefaultStatus:       202,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "public", Audience: "verself-api", Principals: []string{"browser", "cli", "anonymous"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:signup_intent:create", OrganizationSource: "installation", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.signup_intent.create", Resource: "signup_intent", Action: "create"},
		RateLimitBucket:     "signup_mutation",
		RequestBodyMaxBytes: 16384,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "signup", Method: "start", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var VerifySignup = Operation[VerifySignupInput, VerifySignupOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#VerifySignup",
		OperationID:         "verify-signup",
		Method:              "POST",
		Path:                "/api/v1/signup-intents/{signupIntentId}/verification",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "public", Audience: "verself-api", Principals: []string{"browser", "cli", "anonymous"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:signup_intent:verify", OrganizationSource: "installation", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.signup_intent.verify", Resource: "signup_intent", Action: "verify"},
		RateLimitBucket:     "signup_mutation",
		RequestBodyMaxBytes: 16384,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "signup", Method: "verify", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.iam.v1#OrganizationSlugUnavailableError", Type: "urn:verself:problem:iam:organization_slug_unavailable", Code: "iam.organization_slug.unavailable", Status: 409},
			{ShapeID: "verself.iam.v1#PasswordBreachedError", Type: "urn:verself:problem:iam:password_breached", Code: "iam.password.breached", Status: 400},
			{ShapeID: "verself.iam.v1#PasswordTooLongError", Type: "urn:verself:problem:iam:password_too_long", Code: "iam.password.too_long", Status: 400},
			{ShapeID: "verself.iam.v1#PasswordTooShortError", Type: "urn:verself:problem:iam:password_too_short", Code: "iam.password.too_short", Status: 400},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.iam.v1#SignupAccountExistsError", Type: "urn:verself:problem:iam:signup_account_exists", Code: "iam.signup.account_exists", Status: 409},
			{ShapeID: "verself.iam.v1#SignupMaterializingError", Type: "urn:verself:problem:iam:signup_materializing", Code: "iam.signup.materializing", Status: 409},
			{ShapeID: "verself.iam.v1#SignupStateConflictError", Type: "urn:verself:problem:iam:signup_state_conflict", Code: "iam.signup.state_conflict", Status: 409},
			{ShapeID: "verself.iam.v1#SignupVerificationAlreadyUsedError", Type: "urn:verself:problem:iam:signup_verification_already_used", Code: "iam.signup.verification.already_used", Status: 409},
			{ShapeID: "verself.iam.v1#SignupVerificationExpiredError", Type: "urn:verself:problem:iam:signup_verification_expired", Code: "iam.signup.verification.expired", Status: 400},
			{ShapeID: "verself.iam.v1#SignupVerificationInvalidError", Type: "urn:verself:problem:iam:signup_verification_invalid", Code: "iam.signup.verification.invalid", Status: 400},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ListOrganizations = Operation[ListOrganizationsInput, ListOrganizationsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#ListOrganizations",
		OperationID:         "list-organizations",
		Method:              "GET",
		Path:                "/api/v1/orgs",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization:list", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.organization.list", Resource: "organization", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "orgs", Method: "list", Paginated: true, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateOrganization = Operation[CreateOrganizationInput, CreateOrganizationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#CreateOrganization",
		OperationID:         "create-organization",
		Method:              "POST",
		Path:                "/api/v1/orgs",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization:create", OrganizationSource: "request_subject_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.organization.create", Resource: "organization", Action: "create"},
		RateLimitBucket:     "signup_mutation",
		RequestBodyMaxBytes: 16384,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "orgs", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var GetOrganization = Operation[GetOrganizationInput, GetOrganizationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#GetOrganization",
		OperationID:         "get-organization",
		Method:              "GET",
		Path:                "/api/v1/orgs/{orgId}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization:read", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.organization.read", Resource: "organization", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "orgs", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var UpdateOrganization = Operation[UpdateOrganizationInput, UpdateOrganizationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#UpdateOrganization",
		OperationID:         "update-organization",
		Method:              "PATCH",
		Path:                "/api/v1/orgs/{orgId}",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:organization:update", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.organization.update", Resource: "organization", Action: "update"},
		RateLimitBucket:     "iam_mutation",
		RequestBodyMaxBytes: 16384,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "orgs", Method: "update", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ListMembers = Operation[ListMembersInput, ListMembersOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#ListMembers",
		OperationID:         "list-members",
		Method:              "GET",
		Path:                "/api/v1/orgs/{orgId}/members",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:member:list", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.member.list", Resource: "member", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "members", Method: "list", Paginated: true, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetMember = Operation[GetMemberInput, GetMemberOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#GetMember",
		OperationID:         "get-member",
		Method:              "GET",
		Path:                "/api/v1/orgs/{orgId}/members/{memberId}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:member:read", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.member.read", Resource: "member", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "members", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var InviteMember = Operation[InviteMemberInput, InviteMemberOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#InviteMember",
		OperationID:         "invite-member",
		Method:              "POST",
		Path:                "/api/v1/orgs/{orgId}/members:invite",
		DefaultStatus:       202,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:member:invite", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.member.invite", Resource: "member", Action: "invite"},
		RateLimitBucket:     "iam_invite",
		RequestBodyMaxBytes: 16384,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "members", Method: "invite", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var GetIamPolicy = Operation[GetIamPolicyInput, GetIamPolicyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#GetIamPolicy",
		OperationID:         "get-iam-policy",
		Method:              "GET",
		Path:                "/api/v1/orgs/{orgId}/iamPolicy",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:policy:get", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.policy.get", Resource: "organization", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 8192,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "iamPolicies", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var SetIamPolicy = Operation[SetIamPolicyInput, SetIamPolicyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#SetIamPolicy",
		OperationID:         "set-iam-policy",
		Method:              "POST",
		Path:                "/api/v1/orgs/{orgId}/iamPolicy:set",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:policy:set", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.policy.set", Resource: "organization", Action: "set"},
		RateLimitBucket:     "iam_mutation",
		RequestBodyMaxBytes: 32768,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "iamPolicies", Method: "set", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var TestIamPermissions = Operation[TestIamPermissionsInput, TestIamPermissionsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#TestIamPermissions",
		OperationID:         "test-iam-permissions",
		Method:              "POST",
		Path:                "/api/v1/orgs/{orgId}/iamPolicy:testPermissions",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:policy:test", OrganizationSource: "input_member", OrganizationMember: "orgId"},
		Audit:               AuditDescriptor{Event: "iam.policy.test", Resource: "organization", Action: "test"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 8192,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "iamPolicies", Method: "testPermissions", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	AcceptMemberInvite(context.Context, *AcceptMemberInviteInput) (*AcceptMemberInviteOutput, error)
	StartSignup(context.Context, *StartSignupInput) (*StartSignupOutput, error)
	VerifySignup(context.Context, *VerifySignupInput) (*VerifySignupOutput, error)
	ListOrganizations(context.Context, *ListOrganizationsInput) (*ListOrganizationsOutput, error)
	CreateOrganization(context.Context, *CreateOrganizationInput) (*CreateOrganizationOutput, error)
	GetOrganization(context.Context, *GetOrganizationInput) (*GetOrganizationOutput, error)
	UpdateOrganization(context.Context, *UpdateOrganizationInput) (*UpdateOrganizationOutput, error)
	ListMembers(context.Context, *ListMembersInput) (*ListMembersOutput, error)
	GetMember(context.Context, *GetMemberInput) (*GetMemberOutput, error)
	InviteMember(context.Context, *InviteMemberInput) (*InviteMemberOutput, error)
	GetIamPolicy(context.Context, *GetIamPolicyInput) (*GetIamPolicyOutput, error)
	SetIamPolicy(context.Context, *SetIamPolicyInput) (*SetIamPolicyOutput, error)
	TestIamPermissions(context.Context, *TestIamPermissionsInput) (*TestIamPermissionsOutput, error)
}

type ListOrganizationsHandler = Handler[ListOrganizationsInput, ListOrganizationsOutput]

type AcceptMemberInviteHandler = Handler[AcceptMemberInviteInput, AcceptMemberInviteOutput]

type StartSignupHandler = Handler[StartSignupInput, StartSignupOutput]

type VerifySignupHandler = Handler[VerifySignupInput, VerifySignupOutput]

type CreateOrganizationHandler = Handler[CreateOrganizationInput, CreateOrganizationOutput]

type GetOrganizationHandler = Handler[GetOrganizationInput, GetOrganizationOutput]

type UpdateOrganizationHandler = Handler[UpdateOrganizationInput, UpdateOrganizationOutput]

type ListMembersHandler = Handler[ListMembersInput, ListMembersOutput]

type GetMemberHandler = Handler[GetMemberInput, GetMemberOutput]

type InviteMemberHandler = Handler[InviteMemberInput, InviteMemberOutput]

type GetIamPolicyHandler = Handler[GetIamPolicyInput, GetIamPolicyOutput]

type SetIamPolicyHandler = Handler[SetIamPolicyInput, SetIamPolicyOutput]

type TestIamPermissionsHandler = Handler[TestIamPermissionsInput, TestIamPermissionsOutput]
