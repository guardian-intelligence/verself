package dto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type schemaLinkTestBody struct {
	Message string `json:"message"`
}

type schemaLinkTestOutput struct {
	Body schemaLinkTestBody
}

func TestDefaultHumaConfigDoesNotMutateResponseBodies(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, DefaultHumaConfig("Test API", "1.0.0"))
	huma.Register(api, huma.Operation{
		OperationID: "schema-link-test",
		Method:      http.MethodGet,
		Path:        "/schema-link-test",
	}, func(context.Context, *struct{}) (*schemaLinkTestOutput, error) {
		return &schemaLinkTestOutput{Body: schemaLinkTestBody{Message: "ok"}}, nil
	})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/schema-link-test", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := body["$schema"]; ok {
		t.Fatalf("response includes framework schema link field: %#v", body)
	}
	if got := body["message"]; got != "ok" {
		t.Fatalf("message = %#v, want ok", got)
	}
}
