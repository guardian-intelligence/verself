package routecatalog

import "testing"

func TestParseProfileLikeOperationPolicy(t *testing.T) {
	catalog := MustParse([]byte(`{
		"schema_version":"verself.route-catalog.v1",
		"projection":"source",
		"service":{"shape_id":"verself.profile.v1#Profile","name":"profile-service","version":"2026-05-13","public_audience":"verself-api","internal_audience":"profile-service"},
		"operations":[{
			"shape_id":"verself.profile.v1#PatchProfileIdentity",
			"operation_id":"patch-profile-identity",
			"method":"PATCH",
			"path":"/api/v1/profile/identity",
			"default_status":200,
			"input_shape":"verself.profile.v1#PatchProfileIdentityInput",
			"output_shape":"verself.profile.v1#ProfileOutput",
			"readonly":false,
			"paginated":false,
			"identity":{"mode":"bearer","audience":"verself-api","principals":["browser","cli"]},
			"authorization":{"permission":"profile:self:identity:write","organization_source":"request_subject","organization_member":""},
			"audit":{"event":"profile.subject.identity.write","resource":"profile_identity","action":"write","ocsf_class_uid":0,"ocsf_class_name":""},
			"rate_limit":{"bucket":"profile_mutation"},
			"request_body":{"max_bytes":65536},
			"idempotency":{"policy":"idempotency_key_header","header":"Idempotency-Key","member":"idempotencyKey"},
			"sdk":{"module":"profile.identity","method":"update","paginated":false,"retryable":false},
			"errors":[{"shape_id":"verself.common.v1#ValidationFailedError","type":"urn:verself:problem:request:validation_failed","code":"request.validation_failed","status":400}]
		}]
	}`))

	operation := catalog.MustOperation("patch-profile-identity")
	policy := operation.OperationPolicy()
	if got := policy.OrgScope.String(); got != "token_subject" {
		t.Fatalf("OrgScope = %q, want token_subject", got)
	}
	if got := operation.ProblemStatuses(); len(got) != 1 || got[0] != 400 {
		t.Fatalf("ProblemStatuses = %#v, want [400]", got)
	}
}
