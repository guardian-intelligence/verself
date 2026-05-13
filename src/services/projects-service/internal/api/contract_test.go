package api

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/projects-service/internal/contractapi"
	"github.com/verself/projects-service/internal/internalcontractapi"
	"github.com/verself/projects-service/internal/projects"
)

func TestProjectsPublicProjectionMatchesGeneratedDescriptors(t *testing.T) {
	publicAPI := NewAPI(http.NewServeMux(), Config{Version: "1.0.0", ListenAddr: "127.0.0.1:0", Service: &projects.Service{}})
	publicSpec := publicAPI.OpenAPI()

	assertOnlySecuritySchemes(t, publicSpec.Components.SecuritySchemes, "bearerAuth")

	expected := []contractapi.OperationDescriptor{
		contractapi.CreateProject.Descriptor,
		contractapi.ListProjects.Descriptor,
		contractapi.GetProject.Descriptor,
		contractapi.PatchProject.Descriptor,
		contractapi.ArchiveProject.Descriptor,
		contractapi.RestoreProject.Descriptor,
		contractapi.ListProjectEnvironments.Descriptor,
		contractapi.CreateProjectEnvironment.Descriptor,
		contractapi.PatchProjectEnvironment.Descriptor,
		contractapi.ArchiveProjectEnvironment.Descriptor,
	}
	if operationCount(publicSpec) != len(expected) {
		t.Fatalf("public Projects operation count drift: got %d want %d", operationCount(publicSpec), len(expected))
	}
	for _, desc := range expected {
		op := requireOperation(t, publicSpec, desc.Path, desc.Method)
		if op.OperationID != desc.OperationID {
			t.Fatalf("%s %s operation ID drift: got %s want %s", desc.Method, desc.Path, op.OperationID, desc.OperationID)
		}
		if op.DefaultStatus != desc.DefaultStatus {
			t.Fatalf("%s %s status drift: got %d want %d", desc.Method, desc.Path, op.DefaultStatus, desc.DefaultStatus)
		}
		assertOnlySecurity(t, op, desc.Path, "bearerAuth")
		if _, ok := op.Extensions["x-verself-contract"].(map[string]any); !ok {
			t.Fatalf("%s %s missing x-verself-contract extension", desc.Method, desc.Path)
		}
		if _, ok := op.Extensions["x-verself-iam"].(map[string]any); !ok {
			t.Fatalf("%s %s missing x-verself-iam extension", desc.Method, desc.Path)
		}
		if desc.Idempotency.Header != "" && !operationHasRequiredParameter(op, "header", desc.Idempotency.Header) {
			t.Fatalf("%s %s missing required idempotency header", desc.Method, desc.Path)
		}
	}
}

func TestProjectsInternalProjectionMatchesGeneratedDescriptors(t *testing.T) {
	internalAPI := NewInternalAPI(http.NewServeMux(), "1.0.0", "https://127.0.0.1:4265", &projects.Service{}, "inst_test")
	internalSpec := internalAPI.OpenAPI()

	assertOnlySecuritySchemes(t, internalSpec.Components.SecuritySchemes, "mutualTLS")

	expected := []internalcontractapi.OperationDescriptor{
		internalcontractapi.ResolveProject.Descriptor,
		internalcontractapi.ResolveProjectEnvironment.Descriptor,
		internalcontractapi.ListProjectEventsService.Descriptor,
	}
	if operationCount(internalSpec) != len(expected) {
		t.Fatalf("internal Projects operation count drift: got %d want %d", operationCount(internalSpec), len(expected))
	}
	for _, desc := range expected {
		op := requireOperation(t, internalSpec, desc.Path, desc.Method)
		if op.OperationID != desc.OperationID {
			t.Fatalf("%s %s operation ID drift: got %s want %s", desc.Method, desc.Path, op.OperationID, desc.OperationID)
		}
		if op.DefaultStatus != desc.DefaultStatus {
			t.Fatalf("%s %s status drift: got %d want %d", desc.Method, desc.Path, op.DefaultStatus, desc.DefaultStatus)
		}
		assertOnlySecurity(t, op, desc.Path, "mutualTLS")
		if _, ok := op.Extensions["x-verself-contract"].(map[string]any); !ok {
			t.Fatalf("%s %s missing x-verself-contract extension", desc.Method, desc.Path)
		}
		if _, ok := op.Extensions["x-verself-iam"].(map[string]any); !ok {
			t.Fatalf("%s %s missing x-verself-iam extension", desc.Method, desc.Path)
		}
	}
}

func TestProjectsOperationCatalogHasCompletePolicy(t *testing.T) {
	seen := map[string]struct{}{}
	for _, op := range projectOperations() {
		if op.id() == "" {
			t.Fatal("project operation with empty operation ID")
		}
		if _, exists := seen[op.id()]; exists {
			t.Fatalf("duplicate Projects operation ID %q", op.id())
		}
		seen[op.id()] = struct{}{}
	}
	if len(seen) != 13 {
		t.Fatalf("unexpected Projects operation count: got %d", len(seen))
	}
}

func operationCount(spec *huma.OpenAPI) int {
	var count int
	for _, pathItem := range spec.Paths {
		for _, op := range operationsForPath(pathItem) {
			if op != nil {
				count++
			}
		}
	}
	return count
}

func requireOperation(t *testing.T, spec *huma.OpenAPI, path, method string) *huma.Operation {
	t.Helper()
	pathItem := spec.Paths[path]
	if pathItem == nil {
		t.Fatalf("OpenAPI projection is missing path %s", path)
	}
	op := operationByMethod(pathItem, method)
	if op == nil {
		t.Fatalf("OpenAPI projection is missing %s %s", method, path)
	}
	return op
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

func operationByMethod(pathItem *huma.PathItem, method string) *huma.Operation {
	for _, op := range operationsForPath(pathItem) {
		if op != nil && op.Method == method {
			return op
		}
	}
	return nil
}

func assertOnlySecurity(t *testing.T, op *huma.Operation, path, scheme string) {
	t.Helper()
	if len(op.Security) != 1 {
		t.Fatalf("%s %s must declare exactly one OpenAPI security alternative: %#v", op.Method, path, op.Security)
	}
	if _, ok := op.Security[0][scheme]; !ok || len(op.Security[0]) != 1 {
		t.Fatalf("%s %s must require only %s: %#v", op.Method, path, scheme, op.Security)
	}
}

func assertOnlySecuritySchemes(t *testing.T, schemes map[string]*huma.SecurityScheme, expected string) {
	t.Helper()
	if len(schemes) != 1 {
		t.Fatalf("OpenAPI projection must declare exactly one security scheme: %#v", schemes)
	}
	if _, ok := schemes[expected]; !ok {
		t.Fatalf("OpenAPI projection must declare only %s: %#v", expected, schemes)
	}
}

func operationHasRequiredParameter(op *huma.Operation, in, name string) bool {
	for _, param := range op.Parameters {
		if param != nil && param.In == in && param.Name == name && param.Required {
			return true
		}
	}
	return false
}
