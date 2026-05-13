package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/governance-service/internal/contractapi"
	"github.com/verself/governance-service/internal/governance"
	"github.com/verself/governance-service/internal/internalcontractapi"
)

func TestPublicOpenAPIConformsToContract(t *testing.T) {
	api := NewAPI(http.NewServeMux(), "test", "https://governance.api.verself.test", &governance.Service{InstallationID: "inst_test"})
	openAPI := api.OpenAPI()
	contracts := publicContractOperationsByID(t)

	var checked int
	for path, pathItem := range openAPI.Paths {
		if !strings.HasPrefix(path, "/api/") {
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
			assertPublicOperationConforms(t, path, op, want)
		}
	}
	if checked != len(contracts) {
		t.Fatalf("checked %d public operations, want %d", checked, len(contracts))
	}
}

func TestInternalOpenAPIConformsToContract(t *testing.T) {
	api := NewInternalAPI(http.NewServeMux(), "test", "https://127.0.0.1:4254", &governance.Service{})
	openAPI := api.OpenAPI()
	contracts := internalContractOperationsByID(t)

	var checked int
	for path, pathItem := range openAPI.Paths {
		if !strings.HasPrefix(path, "/internal/") {
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
			assertInternalOperationConforms(t, path, op, want)
		}
	}
	if checked != len(contracts) {
		t.Fatalf("checked %d internal operations, want %d", checked, len(contracts))
	}
}

func assertPublicOperationConforms(t *testing.T, path string, op *huma.Operation, want contractapi.OperationDescriptor) {
	t.Helper()
	assertOperationTransportConforms(t, path, op, want.Method, want.Path, want.DefaultStatus, want.RequestBodyMaxBytes)
	assertBearerSecurity(t, path, op)
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
	if len(op.Security) != 1 || len(op.Security[0]) != 1 || len(op.Security[0]["mutualTLS"]) != 0 {
		t.Fatalf("%s %s must require mutualTLS with no OpenAPI scopes: %#v", op.Method, path, op.Security)
	}
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
	response := op.Responses[strconv.Itoa(defaultStatus)]
	if response == nil {
		t.Fatalf("%s %s missing default response %d", op.Method, path, defaultStatus)
	}
	assertResponseHasSchema(t, path, op, response.Content)
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
	assertResponseHasSchema(t, path, op, op.RequestBody.Content)
}

func assertBearerSecurity(t *testing.T, path string, op *huma.Operation) {
	t.Helper()
	if len(op.Security) != 1 || len(op.Security[0]) != 1 || len(op.Security[0]["bearerAuth"]) != 0 {
		t.Fatalf("%s %s must require bearerAuth with no OpenAPI scopes: %#v", op.Method, path, op.Security)
	}
}

func assertResponseHasSchema(t *testing.T, path string, op *huma.Operation, content map[string]*huma.MediaType) {
	t.Helper()
	for _, media := range content {
		if media == nil || media.Schema == nil {
			continue
		}
		schema := media.Schema
		if schema.Ref != "" || schema.Type != "" || len(schema.Properties) > 0 || len(schema.AllOf) > 0 || len(schema.AnyOf) > 0 || len(schema.OneOf) > 0 {
			return
		}
	}
	t.Fatalf("%s %s has no response/request schema", op.Method, path)
}

func assertProblemResponseConforms(t *testing.T, path string, op *huma.Operation, status int) {
	t.Helper()
	response := op.Responses[strconv.Itoa(status)]
	if response == nil {
		t.Fatalf("%s %s missing problem response %d", op.Method, path, status)
	}
	assertResponseHasSchema(t, path, op, response.Content)
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
		t.Fatal("generated governance public contract is empty")
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
		t.Fatal("generated governance internal contract is empty")
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
