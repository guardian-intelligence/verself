package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/projects-service/internal/projects"
)

func TestProjectsOpenAPIInternalProjectionContainsPublicOperations(t *testing.T) {
	publicAPI := NewAPI(http.NewServeMux(), Config{Version: "1.0.0", ListenAddr: "127.0.0.1:0", Service: &projects.Service{}})
	internalAPI := NewInternalAPI(http.NewServeMux(), "1.0.0", "https://127.0.0.1:4265", &projects.Service{})

	publicSpec := publicAPI.OpenAPI()
	internalSpec := internalAPI.OpenAPI()

	var checked int
	for path, publicPath := range publicSpec.Paths {
		if !strings.HasPrefix(path, "/api/") {
			continue
		}
		internalPath := internalSpec.Paths[path]
		if internalPath == nil {
			t.Fatalf("internal OpenAPI projection is missing public path %s", path)
		}
		for _, publicOp := range operationsForPath(publicPath) {
			if publicOp == nil {
				continue
			}
			checked++
			internalOp := operationByMethod(internalPath, publicOp.Method)
			if internalOp == nil {
				t.Fatalf("internal OpenAPI projection is missing %s %s", publicOp.Method, path)
			}
			if internalOp.OperationID != publicOp.OperationID {
				t.Fatalf("%s %s operation ID drift: public=%s internal=%s", publicOp.Method, path, publicOp.OperationID, internalOp.OperationID)
			}
			assertOnlySecurity(t, publicOp, path, "bearerAuth")
			assertOnlySecurity(t, internalOp, path, "mutualTLS")
			for _, header := range []string{originOrgIDHeader, originSubjectHeader} {
				if !operationHasRequiredParameter(internalOp, "header", header) {
					t.Fatalf("%s %s internal projection must require %s", publicOp.Method, path, header)
				}
			}
			if _, ok := internalOp.Extensions["x-verself-origin"].(map[string]any); !ok {
				t.Fatalf("%s %s internal projection missing x-verself-origin extension", publicOp.Method, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no public Projects operations")
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

func operationHasRequiredParameter(op *huma.Operation, in, name string) bool {
	for _, param := range op.Parameters {
		if param != nil && param.In == in && param.Name == name && param.Required {
			return true
		}
	}
	return false
}
