package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestObjectStoragePublicOperationsCarryIAMPolicy(t *testing.T) {
	api := NewAPI(http.NewServeMux(), Config{Version: "test"})
	var checked int
	for path, pathItem := range api.OpenAPI().Paths {
		if !strings.HasPrefix(path, "/api/") {
			continue
		}
		for _, op := range operationsForPath(pathItem) {
			if op == nil {
				continue
			}
			checked++
			rawPolicy, ok := op.Extensions["x-verself-iam"].(map[string]any)
			if !ok {
				t.Fatalf("%s %s missing x-verself-iam policy", op.Method, path)
			}
			if rawPolicy["permission"] == "" || rawPolicy["resource"] == "" || rawPolicy["action"] == "" {
				t.Fatalf("%s %s has incomplete IAM policy: %#v", op.Method, path, rawPolicy)
			}
			if len(op.Security) != 1 {
				t.Fatalf("%s %s has unexpected security requirements: %#v", op.Method, path, op.Security)
			}
			if _, ok := op.Security[0]["bearerAuth"]; !ok {
				t.Fatalf("%s %s is not bearer-protected: %#v", op.Method, path, op.Security)
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no object-storage API operations")
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
