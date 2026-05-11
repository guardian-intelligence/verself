package bazeltelemetry

import "time"

const SchemaVersion = 1

type Upload struct {
	SchemaVersion int           `json:"schema_version"`
	Invocation    Invocation    `json:"invocation"`
	Events        []Event       `json:"events,omitempty"`
	ProfileSpans  []ProfileSpan `json:"profile_spans,omitempty"`
	Spawns        []Spawn       `json:"spawns,omitempty"`
	Targets       []Target      `json:"targets,omitempty"`
}

type Invocation struct {
	InvocationID      string    `json:"invocation_id"`
	Command           string    `json:"command"`
	Args              []string  `json:"args,omitempty"`
	TargetPatterns    []string  `json:"target_patterns,omitempty"`
	WorkingDirectory  string    `json:"working_directory,omitempty"`
	ExitCode          int       `json:"exit_code"`
	StartedAt         time.Time `json:"started_at"`
	CompletedAt       time.Time `json:"completed_at"`
	DurationMs        int64     `json:"duration_ms"`
	ProfileBytes      int64     `json:"profile_bytes"`
	BEPBytes          int64     `json:"bep_bytes"`
	ExecutionLogBytes int64     `json:"execution_log_bytes"`
	GitHubWorkflow    string    `json:"github_workflow,omitempty"`
	GitHubJob         string    `json:"github_job,omitempty"`
	GitHubRef         string    `json:"github_ref,omitempty"`
	GitHubSHA         string    `json:"github_sha,omitempty"`
}

type Event struct {
	ObservedAt time.Time `json:"observed_at"`
	EventName  string    `json:"event_name"`
	Result     string    `json:"result"`
	Reason     string    `json:"reason,omitempty"`
	DurationMs int64     `json:"duration_ms"`
}

type ProfileSpan struct {
	SpanKind     string    `json:"span_kind"`
	Category     string    `json:"category"`
	EventName    string    `json:"event_name"`
	Package      string    `json:"package,omitempty"`
	BuildFile    string    `json:"build_file,omitempty"`
	ExternalRepo string    `json:"external_repo,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	DurationMs   int64     `json:"duration_ms"`
}

type Spawn struct {
	TargetLabel string    `json:"target_label"`
	Package     string    `json:"package,omitempty"`
	BuildFile   string    `json:"build_file,omitempty"`
	RuleName    string    `json:"rule_name,omitempty"`
	Mnemonic    string    `json:"mnemonic"`
	Runner      string    `json:"runner"`
	CacheHit    bool      `json:"cache_hit"`
	ExitCode    int32     `json:"exit_code"`
	Status      string    `json:"status,omitempty"`
	OutputCount int       `json:"output_count"`
	OutputFirst string    `json:"output_first,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	DurationMs  int64     `json:"duration_ms"`
}

type Target struct {
	Label            string `json:"label"`
	Package          string `json:"package,omitempty"`
	BuildFile        string `json:"build_file,omitempty"`
	RuleName         string `json:"rule_name,omitempty"`
	Success          bool   `json:"success"`
	OutputGroupCount int    `json:"output_group_count"`
	OutputFileCount  int    `json:"output_file_count"`
}
