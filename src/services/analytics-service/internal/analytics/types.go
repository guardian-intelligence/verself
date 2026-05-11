package analytics

import "time"

const (
	SourceKindGitHubActionsOIDC = "github_actions_oidc"
	DatasetBuild                = "build"
)

type Source struct {
	Kind            string
	Subject         string
	TenantID        string
	Repository      string
	RepositoryOwner string
	Ref             string
	SHA             string
	RunID           string
	RunAttempt      uint32
	Workflow        string
	Job             string
}

type EventRow struct {
	EventDate          time.Time          `ch:"event_date"`
	ObservedAt         time.Time          `ch:"observed_at"`
	EventID            string             `ch:"event_id"`
	Dataset            string             `ch:"dataset"`
	EventName          string             `ch:"event_name"`
	SourceKind         string             `ch:"source_kind"`
	SourceSubject      string             `ch:"source_subject"`
	TenantID           string             `ch:"tenant_id"`
	Repository         string             `ch:"repository"`
	RepositoryOwner    string             `ch:"repository_owner"`
	GitRef             string             `ch:"git_ref"`
	GitSHA             string             `ch:"git_sha"`
	ProviderRunID      string             `ch:"provider_run_id"`
	ProviderRunAttempt uint32             `ch:"provider_run_attempt"`
	ProviderWorkflow   string             `ch:"provider_workflow"`
	ProviderJob        string             `ch:"provider_job"`
	ServiceName        string             `ch:"service_name"`
	ServiceVersion     string             `ch:"service_version"`
	TraceID            string             `ch:"trace_id"`
	SpanID             string             `ch:"span_id"`
	BuildTool          string             `ch:"build_tool"`
	BuildPackage       string             `ch:"build_package"`
	BuildCommand       string             `ch:"build_command"`
	BuildTarget        string             `ch:"build_target"`
	ConfigPath         string             `ch:"config_path"`
	CacheSource        string             `ch:"cache_source"`
	CacheResult        string             `ch:"cache_result"`
	CacheReason        string             `ch:"cache_reason"`
	Status             string             `ch:"status"`
	DurationMs         uint64             `ch:"duration_ms"`
	StringAttributes   map[string]string  `ch:"string_attributes"`
	IntAttributes      map[string]int64   `ch:"int_attributes"`
	FloatAttributes    map[string]float64 `ch:"float_attributes"`
	BoolAttributes     map[string]uint8   `ch:"bool_attributes"`
}
