package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestVerselfProblemMatchesModeledShape(t *testing.T) {
	err := newProblem(context.Background(), http.StatusUnprocessableEntity, "", "validation failed", errors.New("field is invalid"))
	body, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal problem: %v", marshalErr)
	}
	var fields map[string]any
	if unmarshalErr := json.Unmarshal(body, &fields); unmarshalErr != nil {
		t.Fatalf("unmarshal problem: %v", unmarshalErr)
	}
	for _, field := range []string{"type", "title", "status", "detail", "code"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("problem missing field %q: %s", field, body)
		}
	}
	if _, ok := fields["errors"]; ok {
		t.Fatalf("problem exposed unmodeled errors field: %s", body)
	}
	if err.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", err.Status)
	}
	if err.Code != "request.validation_failed" {
		t.Fatalf("code = %q, want request.validation_failed", err.Code)
	}
}
