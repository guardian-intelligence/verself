package ocsf

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuilderBuildsAPIActivity(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 34, 56, 0, time.UTC)
	eventUID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	builder := Builder{Product: Product{
		Name:       stringPtr("Verself Governance Service"),
		Uid:        stringPtr("verself"),
		VendorName: stringPtr("Guardian Intelligence"),
		Version:    stringPtr(SchemaVersion),
	}}

	built, err := builder.Build(context.Background(), APIActivityInput{
		OrgID:    "org_1",
		EventUID: eventUID,
		Time:     now,
		Operation: Operation{
			Service:    "secrets-service",
			ID:         "create-secret",
			EventCode:  "secrets.secret.create",
			Action:     "create",
			Permission: "secrets.secret.create",
		},
		Actor: ActorIdentity{
			Type:          "user",
			UID:           "user_1",
			DisplayName:   "Ada Lovelace",
			Email:         "ada@example.com",
			CredentialUID: "cred_1",
		},
		Resources: []Resource{{
			Type:     "secret",
			UID:      "secret_1",
			FullName: "urn:verself:secret:secret_1",
		}},
		Request: Request{
			UID:        "req_1",
			Method:     "post",
			Route:      "/api/v1/secrets",
			SafeParams: "project_id=project_1",
			Host:       "secrets.api.verself.sh",
			ClientIP:   "192.0.2.10",
			SourceName: "browser",
		},
		Response: Response{Code: 201, Status: "Created"},
		Trace: TraceContext{
			TraceID: "11111111111111111111111111111111",
			SpanID:  "2222222222222222",
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if built.Event.ClassUid != ClassUIDAPIActivity {
		t.Fatalf("class uid = %d, want %d", built.Event.ClassUid, ClassUIDAPIActivity)
	}
	if built.Event.Metadata.Version != SchemaVersion {
		t.Fatalf("version = %q, want %q", built.Event.Metadata.Version, SchemaVersion)
	}
	if built.Event.Api.Service == nil || stringValue(built.Event.Api.Service.Name) != "secrets-service" {
		t.Fatalf("api service = %#v", built.Event.Api.Service)
	}
	if built.Event.Actor.User == nil || stringValue(built.Event.Actor.User.Uid) != "user_1" {
		t.Fatalf("actor user = %#v", built.Event.Actor.User)
	}
	if len(built.Resources) != 1 || built.Resources[0].Role != "Target" {
		t.Fatalf("resources = %#v", built.Resources)
	}
	if built.Event.Unmapped == nil {
		t.Fatal("unmapped trace extension missing")
	}
	var unmapped map[string]any
	if err := json.Unmarshal([]byte(*built.Event.Unmapped), &unmapped); err != nil {
		t.Fatalf("unmapped JSON: %v", err)
	}
	traceValue, ok := unmapped["trace"].(map[string]any)
	if !ok {
		t.Fatalf("trace extension = %#v", unmapped["trace"])
	}
	if traceValue["uid"] != "11111111111111111111111111111111" {
		t.Fatalf("trace uid = %#v", traceValue["uid"])
	}
	if built.SHA256Hex == "" || built.JSON == "" {
		t.Fatalf("missing canonical payload or hash: %#v", built)
	}
}
