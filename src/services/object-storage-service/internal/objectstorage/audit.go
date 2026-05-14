package objectstorage

import "context"

type APIActivity struct {
	OrgID         string         `json:"org_id"`
	APIService    string         `json:"api_service"`
	APIOperation  string         `json:"api_operation"`
	APIEventCode  string         `json:"api_event_code"`
	APIAction     string         `json:"api_action"`
	ActorType     string         `json:"actor_type"`
	ActorUID      string         `json:"actor_uid"`
	CredentialUID string         `json:"credential_uid,omitempty"`
	Permission    string         `json:"permission"`
	ResourceType  string         `json:"resource_type"`
	ResourceUID   string         `json:"resource_uid,omitempty"`
	HTTPStatus    uint16         `json:"http_status"`
	Decision      string         `json:"authorization_decision"`
	Status        string         `json:"status"`
	StatusCode    string         `json:"status_code,omitempty"`
	TraceUID      string         `json:"trace_uid,omitempty"`
	Unmapped      map[string]any `json:"unmapped,omitempty"`
}

type APIActivitySink func(ctx context.Context, record APIActivity) error
