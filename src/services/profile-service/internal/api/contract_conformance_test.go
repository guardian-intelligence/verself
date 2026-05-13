package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/profile-service/internal/contractapi"
	"github.com/verself/profile-service/internal/internalcontractapi"
	"github.com/verself/profile-service/internal/profile"
)

func TestPublicOpenAPIConformsToContract(t *testing.T) {
	api := NewAPI(http.NewServeMux(), Config{Version: "test", ListenAddr: "https://profile.api.verself.test", Service: &profile.Service{}})
	assertOperationsConform(t, api.OpenAPI(), "/api/", publicContractOperationsByID(t), assertPublicOperationConforms)
}

func TestInternalOpenAPIConformsToContract(t *testing.T) {
	api := NewInternalAPI(http.NewServeMux(), "test", "https://127.0.0.1:4254", &profile.Service{})
	assertOperationsConform(t, api.OpenAPI(), "/internal/", internalContractOperationsByID(t), assertInternalOperationConforms)
}

func assertOperationsConform[D any](t *testing.T, openAPI *huma.OpenAPI, pathPrefix string, contracts map[string]D, assert func(*testing.T, string, *huma.Operation, D)) {
	t.Helper()
	var checked int
	for path, pathItem := range openAPI.Paths {
		if !strings.HasPrefix(path, pathPrefix) {
			continue
		}
		for _, op := range operationsForPath(pathItem) {
			if op == nil {
				continue
			}
			checked++
			want, ok := contracts[op.OperationID]
			if !ok {
				t.Fatalf("%s %s operation %q is not generated from Smithy", op.Method, path, op.OperationID)
			}
			assert(t, path, op, want)
		}
	}
	if checked != len(contracts) {
		t.Fatalf("checked %d operations with prefix %q, want %d", checked, pathPrefix, len(contracts))
	}
}

func assertPublicOperationConforms(t *testing.T, path string, op *huma.Operation, want contractapi.OperationDescriptor) {
	t.Helper()
	assertOperationTransportConforms(t, path, op, want.Method, want.Path, want.DefaultStatus, want.RequestBodyMaxBytes)
	assertSecurity(t, path, op, "bearerAuth")
	assertContractExtension(t, path, op, contractFields{
		ShapeID:           want.ShapeID,
		OperationID:       want.OperationID,
		Identity:          want.Identity.Mode,
		Audience:          want.Identity.Audience,
		Permission:        want.Authorization.Permission,
		OrganizationSrc:   want.Authorization.OrganizationSource,
		OrganizationMem:   want.Authorization.OrganizationMember,
		AuditEvent:        want.Audit.Event,
		Resource:          want.Audit.Resource,
		Action:            want.Audit.Action,
		RateLimitBucket:   want.RateLimitBucket,
		RequestBodyMax:    want.RequestBodyMaxBytes,
		IdempotencyPolicy: want.Idempotency.Policy,
	})
	for _, problem := range want.Problems {
		assertProblemResponseConforms(t, path, op, problem.Status)
	}
	if want.Idempotency.Policy == string(idempotencyHeaderKey) && !operationHasRequiredParameter(op, "header", "Idempotency-Key") {
		t.Fatalf("%s %s requires Idempotency-Key but does not declare it", op.Method, path)
	}
}

func assertInternalOperationConforms(t *testing.T, path string, op *huma.Operation, want internalcontractapi.OperationDescriptor) {
	t.Helper()
	assertOperationTransportConforms(t, path, op, want.Method, want.Path, want.DefaultStatus, want.RequestBodyMaxBytes)
	assertSecurity(t, path, op, "mutualTLS")
	assertContractExtension(t, path, op, contractFields{
		ShapeID:           want.ShapeID,
		OperationID:       want.OperationID,
		Identity:          want.Identity.Mode,
		Audience:          want.Identity.Audience,
		Permission:        want.Authorization.Permission,
		OrganizationSrc:   want.Authorization.OrganizationSource,
		OrganizationMem:   want.Authorization.OrganizationMember,
		AuditEvent:        want.Audit.Event,
		Resource:          want.Audit.Resource,
		Action:            want.Audit.Action,
		RateLimitBucket:   want.RateLimitBucket,
		RequestBodyMax:    want.RequestBodyMaxBytes,
		IdempotencyPolicy: want.Idempotency.Policy,
	})
	for _, problem := range want.Problems {
		assertProblemResponseConforms(t, path, op, problem.Status)
	}
}

func assertOperationTransportConforms(t *testing.T, path string, op *huma.Operation, method string, contractPath string, defaultStatus int, bodyBudget int64) {
	t.Helper()
	if op.Method != method || path != contractPath {
		t.Fatalf("%s %s drifted from generated contract %s %s", op.Method, path, method, contractPath)
	}
	if op.DefaultStatus != defaultStatus {
		t.Fatalf("%s %s default status = %d, want %d", op.Method, path, op.DefaultStatus, defaultStatus)
	}
	if op.Responses[strconv.Itoa(defaultStatus)] == nil {
		t.Fatalf("%s %s missing default response %d", op.Method, path, defaultStatus)
	}
	if bodyBudget <= 0 {
		if op.RequestBody != nil {
			t.Fatalf("%s %s declares an unexpected request body", op.Method, path)
		}
		return
	}
	if op.MaxBodyBytes != bodyBudget {
		t.Fatalf("%s %s body limit = %d, want %d", op.Method, path, op.MaxBodyBytes, bodyBudget)
	}
	if op.RequestBody == nil {
		t.Fatalf("%s %s missing request body schema", op.Method, path)
	}
}

func assertSecurity(t *testing.T, path string, op *huma.Operation, scheme string) {
	t.Helper()
	if len(op.Security) != 1 || len(op.Security[0]) != 1 || len(op.Security[0][scheme]) != 0 {
		t.Fatalf("%s %s must require %s with no OpenAPI scopes: %#v", op.Method, path, scheme, op.Security)
	}
}

func assertProblemResponseConforms(t *testing.T, path string, op *huma.Operation, status int) {
	t.Helper()
	if op.Responses[strconv.Itoa(status)] == nil {
		t.Fatalf("%s %s missing problem response %d", op.Method, path, status)
	}
}

type contractFields struct {
	ShapeID           string
	OperationID       string
	Identity          string
	Audience          string
	Permission        string
	OrganizationSrc   string
	OrganizationMem   string
	AuditEvent        string
	Resource          string
	Action            string
	RateLimitBucket   string
	RequestBodyMax    int64
	IdempotencyPolicy string
}

func assertContractExtension(t *testing.T, path string, op *huma.Operation, want contractFields) {
	t.Helper()
	raw, ok := op.Extensions["x-verself-contract"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s missing x-verself-contract metadata", op.Method, path)
	}
	for key, wantValue := range map[string]string{
		"shape_id":            want.ShapeID,
		"operation_id":        want.OperationID,
		"identity":            want.Identity,
		"audience":            want.Audience,
		"permission":          want.Permission,
		"organization_source": want.OrganizationSrc,
		"organization_member": want.OrganizationMem,
		"audit_event":         want.AuditEvent,
		"resource":            want.Resource,
		"action":              want.Action,
		"rate_limit_bucket":   want.RateLimitBucket,
		"idempotency":         want.IdempotencyPolicy,
	} {
		if got := raw[key]; got != wantValue {
			t.Fatalf("%s %s %s = %#v, want %#v", op.Method, path, key, got, wantValue)
		}
	}
	if got := contractExtensionInt64(raw["request_body_max_bytes"]); got != want.RequestBodyMax {
		t.Fatalf("%s %s request_body_max_bytes = %#v, want %d", op.Method, path, raw["request_body_max_bytes"], want.RequestBodyMax)
	}
}

func contractExtensionInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func publicContractOperationsByID(t *testing.T) map[string]contractapi.OperationDescriptor {
	t.Helper()
	out := map[string]contractapi.OperationDescriptor{}
	for _, operation := range contractapi.Operations {
		if _, duplicate := out[operation.OperationID]; duplicate {
			t.Fatalf("duplicate generated public operation %q", operation.OperationID)
		}
		out[operation.OperationID] = operation
	}
	if len(out) == 0 {
		t.Fatal("generated profile public contract is empty")
	}
	return out
}

func internalContractOperationsByID(t *testing.T) map[string]internalcontractapi.OperationDescriptor {
	t.Helper()
	out := map[string]internalcontractapi.OperationDescriptor{}
	for _, operation := range internalcontractapi.Operations {
		if _, duplicate := out[operation.OperationID]; duplicate {
			t.Fatalf("duplicate generated internal operation %q", operation.OperationID)
		}
		out[operation.OperationID] = operation
	}
	if len(out) == 0 {
		t.Fatal("generated profile internal contract is empty")
	}
	return out
}

func operationsForPath(pathItem *huma.PathItem) []*huma.Operation {
	return []*huma.Operation{
		pathItem.Get,
		pathItem.Post,
		pathItem.Put,
		pathItem.Patch,
		pathItem.Delete,
		pathItem.Head,
		pathItem.Options,
		pathItem.Trace,
	}
}

func operationHasRequiredParameter(op *huma.Operation, in string, name string) bool {
	for _, param := range op.Parameters {
		if param != nil && param.In == in && param.Name == name && param.Required {
			return true
		}
	}
	return false
}
