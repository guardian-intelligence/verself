package iam

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// Permission is the product permission name checked through iam-service.
//
// In the Zanzibar/SpiceDB model this names a product operation permission such
// as "sandbox:execution:read". It is not a Cedar-style action expression and
// it is not a raw SpiceDB object/relation string.
type Permission string

func (p Permission) String() string { return string(p) }

// LogValue renders named-string values as plain strings for the otelslog
// bridge. Without this, the bridge emits `"unhandled: (iam.Permission) v"`.
func (p Permission) LogValue() slog.Value { return slog.StringValue(string(p)) }

// ResourceKind is the stable product resource kind used for OpenAPI metadata,
// audit rows, and operator reports.
//
// This is intentionally only a kind ("api_credential", "execution"), not a
// SpiceDB resource reference. Resource IDs are derived at the service boundary
// from request/response DTOs or service state.
type ResourceKind string

func (r ResourceKind) String() string { return string(r) }
func (r ResourceKind) LogValue() slog.Value {
	return slog.StringValue(string(r))
}

// Action is the human and audit verb for an operation.
//
// Zanzibar checks still use Permission. Action exists so OpenAPI, audit logs,
// and generated docs can describe the operation without parsing a permission
// string.
type Action string

const (
	ActionArchive  Action = "archive"
	ActionCancel   Action = "cancel"
	ActionConnect  Action = "connect"
	ActionCreate   Action = "create"
	ActionDecrypt  Action = "decrypt"
	ActionDelete   Action = "delete"
	ActionDisable  Action = "disable"
	ActionDownload Action = "download"
	ActionEncrypt  Action = "encrypt"
	ActionInvite   Action = "invite"
	ActionIngest   Action = "ingest"
	ActionList     Action = "list"
	ActionManage   Action = "manage"
	ActionPause    Action = "pause"
	ActionQuery    Action = "query"
	ActionRead     Action = "read"
	ActionRegister Action = "register"
	ActionResolve  Action = "resolve"
	ActionResume   Action = "resume"
	ActionRestore  Action = "restore"
	ActionRevoke   Action = "revoke"
	ActionRoll     Action = "roll"
	ActionRotate   Action = "rotate"
	ActionSearch   Action = "search"
	ActionSet      Action = "set"
	ActionSign     Action = "sign"
	ActionSync     Action = "sync"
	ActionTest     Action = "test"
	ActionUpdate   Action = "update"
	ActionVerify   Action = "verify"
	ActionWrite    Action = "write"
	ActionExport   Action = "export"
	ActionErase    Action = "erase"
)

func (a Action) String() string { return string(a) }
func (a Action) LogValue() slog.Value {
	return slog.StringValue(string(a))
}

// OrgScope describes how the enforcement point derives the organization or
// subject boundary for the authorization check.
//
// It is a small enum instead of an arbitrary expression language. Resource
// ownership is still validated by the owning service after the permission check
// passes.
type OrgScope string

const (
	// OrgScopeTokenOrgID checks against the selected org_id carried by the
	// validated Zitadel access token.
	OrgScopeTokenOrgID OrgScope = "token_org_id"
	// OrgScopeRequestSubject discovers accessible organizations from the
	// request subject instead of from token-carried authorization claims.
	OrgScopeRequestSubject OrgScope = "request_subject"
	// OrgScopeTokenSubject is for subject-scoped personal surfaces such as
	// profile, notifications, and mailbox APIs.
	OrgScopeTokenSubject OrgScope = "token_subject"
	// OrgScopePathOrgID checks against an org_id path parameter. Callers must
	// still compare the path org to the selected token org before touching data.
	OrgScopePathOrgID OrgScope = "path_org_id"
	// OrgScopeBodyOrgID checks against an org_id carried in the request body.
	OrgScopeBodyOrgID OrgScope = "body_org_id"
	// OrgScopeRequestOrgID is used by SPIFFE-authenticated service projections
	// whose DTO includes an origin organization.
	OrgScopeRequestOrgID OrgScope = "request_org_id"
	// OrgScopeRequestSubjectID is used by SPIFFE-authenticated service
	// projections whose DTO is scoped to an origin subject.
	OrgScopeRequestSubjectID OrgScope = "request_subject_id"
	// OrgScopeRequestID is used by internal status lookups where the request ID
	// resolves to the ownership boundary inside the target service.
	OrgScopeRequestID OrgScope = "request_id"
	// OrgScopeCheckoutGrant is used by checkout archive downloads authorized by
	// a short-lived checkout grant rather than a bearer token organization.
	OrgScopeCheckoutGrant OrgScope = "checkout_grant"
)

func (s OrgScope) String() string { return string(s) }
func (s OrgScope) LogValue() slog.Value {
	return slog.StringValue(string(s))
}

// RateLimitClass is a service-owned throttle bucket name. The shared policy
// type validates its shape, while each service owns the actual numeric rule.
type RateLimitClass string

func (c RateLimitClass) String() string { return string(c) }
func (c RateLimitClass) LogValue() slog.Value {
	return slog.StringValue(string(c))
}

// IdempotencyPolicy describes where a public mutation carries its retry key.
type IdempotencyPolicy string

const (
	IdempotencyNone           IdempotencyPolicy = ""
	IdempotencyHeaderKey      IdempotencyPolicy = "idempotency_key_header"
	IdempotencyRequestBodyKey IdempotencyPolicy = "request_body_idempotency_key"
)

func (p IdempotencyPolicy) String() string { return string(p) }
func (p IdempotencyPolicy) LogValue() slog.Value {
	return slog.StringValue(string(p))
}

// AuditEvent is the stable dotted event name emitted to logs and governance
// audit sinks, for example "iam.api_credential.create".
//
// Event categories are deliberately not part of OperationPolicy. If governance
// needs OCSF or SIEM categories, that taxonomy belongs in the audit pipeline as
// a mapping from stable AuditEvent values.
type AuditEvent string

func (e AuditEvent) String() string { return string(e) }
func (e AuditEvent) LogValue() slog.Value {
	return slog.StringValue(string(e))
}

// OperationPolicy is the shared contract for product-service HTTP operations
// that require IAM enforcement.
//
// The fields are intentionally concrete and finite: authorization permission,
// resource/action audit metadata, scope derivation, throttling class,
// idempotency requirement, audit event, and request body budget. It does not
// embed a customer policy language and it does not expose raw SpiceDB tuples.
type OperationPolicy struct {
	Permission     Permission
	Resource       ResourceKind
	Action         Action
	OrgScope       OrgScope
	RateLimitClass RateLimitClass
	Idempotency    IdempotencyPolicy
	AuditEvent     AuditEvent
	BodyLimitBytes int64
}

// ValidateHTTPOperation rejects incomplete or weakly-typed policy metadata.
// Services call it at route registration time and panic on failure so catalog
// drift is caught during startup and OpenAPI generation.
func (p OperationPolicy) ValidateHTTPOperation(method, operationID string) error {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return fmt.Errorf("operation policy: operation_id is required")
	}
	if err := validatePermission("permission", string(p.Permission)); err != nil {
		return fmt.Errorf("%s: %w", operationID, err)
	}
	if err := validateToken("resource", string(p.Resource), tokenLowerUnderscore); err != nil {
		return fmt.Errorf("%s: %w", operationID, err)
	}
	if err := validateAction(p.Action); err != nil {
		return fmt.Errorf("%s: %w", operationID, err)
	}
	if err := validateOrgScope(p.OrgScope); err != nil {
		return fmt.Errorf("%s: %w", operationID, err)
	}
	if err := validateToken("rate_limit_class", string(p.RateLimitClass), tokenLowerUnderscore); err != nil {
		return fmt.Errorf("%s: %w", operationID, err)
	}
	if err := validateIdempotency(p.Idempotency); err != nil {
		return fmt.Errorf("%s: %w", operationID, err)
	}
	if err := validateAuditEvent("audit_event", string(p.AuditEvent)); err != nil {
		return fmt.Errorf("%s: %w", operationID, err)
	}
	if p.BodyLimitBytes < 0 {
		return fmt.Errorf("%s: request body limit must not be negative", operationID)
	}
	if OperationRequiresBodyBudget(method) && p.BodyLimitBytes <= 0 {
		return fmt.Errorf("%s: mutating operation must declare request body limit", operationID)
	}
	return nil
}

// OpenAPIExtension returns the canonical x-verself-iam extension payload.
func (p OperationPolicy) OpenAPIExtension() map[string]any {
	out := map[string]any{
		"permission":       string(p.Permission),
		"resource":         string(p.Resource),
		"action":           string(p.Action),
		"org_scope":        string(p.OrgScope),
		"rate_limit_class": string(p.RateLimitClass),
		"audit_event":      string(p.AuditEvent),
	}
	if p.Idempotency != IdempotencyNone {
		out["idempotency"] = string(p.Idempotency)
	}
	if p.BodyLimitBytes > 0 {
		out["request_body_max_bytes"] = p.BodyLimitBytes
	}
	return out
}

// OperationRequiresBodyBudget reports whether an HTTP method can carry
// side-effecting request content and therefore must have an explicit body
// budget.
func OperationRequiresBodyBudget(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validatePermission(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !strings.Contains(value, ":") {
		return fmt.Errorf("%s must contain a product namespace", field)
	}
	segments := strings.Split(value, ":")
	for _, segment := range segments {
		if err := validateToken(field, segment, tokenLowerDashUnderscore); err != nil {
			return err
		}
	}
	return nil
}

func validateAction(action Action) error {
	switch action {
	case ActionArchive, ActionCancel, ActionConnect, ActionCreate, ActionDecrypt,
		ActionDelete, ActionDisable, ActionDownload, ActionEncrypt, ActionInvite,
		ActionIngest, ActionList, ActionManage, ActionPause, ActionQuery, ActionRead, ActionRegister, ActionResolve, ActionResume,
		ActionRestore, ActionRevoke, ActionRoll, ActionRotate, ActionSearch,
		ActionSet, ActionSign, ActionSync, ActionTest, ActionUpdate, ActionVerify,
		ActionWrite, ActionExport, ActionErase:
		return nil
	case "":
		return fmt.Errorf("action is required")
	default:
		return fmt.Errorf("unsupported action %q", action)
	}
}

func validateOrgScope(scope OrgScope) error {
	switch scope {
	case OrgScopeTokenOrgID, OrgScopeRequestSubject, OrgScopeTokenSubject,
		OrgScopePathOrgID, OrgScopeBodyOrgID, OrgScopeRequestOrgID,
		OrgScopeRequestSubjectID, OrgScopeRequestID, OrgScopeCheckoutGrant:
		return nil
	case "":
		return fmt.Errorf("org_scope is required")
	default:
		return fmt.Errorf("unsupported org_scope %q", scope)
	}
}

func validateIdempotency(policy IdempotencyPolicy) error {
	switch policy {
	case IdempotencyNone, IdempotencyHeaderKey, IdempotencyRequestBodyKey:
		return nil
	default:
		return fmt.Errorf("unsupported idempotency policy %q", policy)
	}
}

func validateAuditEvent(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !strings.Contains(value, ".") {
		return fmt.Errorf("%s must contain dotted product event segments", field)
	}
	segments := strings.Split(value, ".")
	for _, segment := range segments {
		if err := validateToken(field, segment, tokenLowerDashUnderscore); err != nil {
			return err
		}
	}
	return nil
}

type tokenAlphabet int

const (
	tokenLowerUnderscore tokenAlphabet = iota
	tokenLowerDashUnderscore
)

func validateToken(field, value string, alphabet tokenAlphabet) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value[0] < 'a' || value[0] > 'z' {
		return fmt.Errorf("%s must start with a lowercase letter", field)
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= '0' && ch <= '9':
		case ch == '_':
		case ch == '-' && alphabet == tokenLowerDashUnderscore:
		default:
			return fmt.Errorf("%s contains unsupported character %q", field, ch)
		}
	}
	return nil
}
