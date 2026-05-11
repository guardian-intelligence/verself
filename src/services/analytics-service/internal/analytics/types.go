package analytics

import (
	"encoding/base64"
	"strings"
	"time"
)

const (
	SourceKindGitHubActionsOIDC = "github_actions_oidc"
	SignalKindLog               = "log"
	DatasetBuild                = "build"

	RequestKindOTLPLogs = "otlp_logs"

	OutcomeAllowed = "allowed"
	OutcomeDenied  = "denied"
	OutcomeError   = "error"

	PermissionDatasetRead    = "analytics:dataset:read"
	PermissionDatasetReadRaw = "analytics:dataset:read_raw"
)

type DatasetContext struct {
	OrgID       string
	ProjectID   string
	DatasetID   string
	Environment string
}

type Source struct {
	Kind            string
	Subject         string
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
	EventDate           time.Time          `ch:"event_date"`
	ObservedAt          time.Time          `ch:"observed_at"`
	Timestamp           time.Time          `ch:"timestamp"`
	EventID             string             `ch:"event_id"`
	OrgID               string             `ch:"org_id"`
	ProjectID           string             `ch:"project_id"`
	DatasetID           string             `ch:"dataset_id"`
	Environment         string             `ch:"environment"`
	SignalKind          string             `ch:"signal_kind"`
	EventName           string             `ch:"event_name"`
	SourceKind          string             `ch:"source_kind"`
	SourceSubject       string             `ch:"source_subject"`
	ServiceName         string             `ch:"service_name"`
	ServiceVersion      string             `ch:"service_version"`
	SeverityText        string             `ch:"severity_text"`
	Status              string             `ch:"status"`
	TraceID             string             `ch:"trace_id"`
	SpanID              string             `ch:"span_id"`
	ParentSpanID        string             `ch:"parent_span_id"`
	DurationMs          uint64             `ch:"duration_ms"`
	Body                string             `ch:"body"`
	BodyRedactionStatus string             `ch:"body_redaction_status"`
	StringAttributes    map[string]string  `ch:"string_attributes"`
	IntAttributes       map[string]int64   `ch:"int_attributes"`
	FloatAttributes     map[string]float64 `ch:"float_attributes"`
	BoolAttributes      map[string]uint8   `ch:"bool_attributes"`
}

type IngestEventRow struct {
	EventDate       time.Time `ch:"event_date"`
	RecordedAt      time.Time `ch:"recorded_at"`
	OrgID           string    `ch:"org_id"`
	ProjectID       string    `ch:"project_id"`
	DatasetID       string    `ch:"dataset_id"`
	Environment     string    `ch:"environment"`
	SourceKind      string    `ch:"source_kind"`
	SourceSubject   string    `ch:"source_subject"`
	RequestKind     string    `ch:"request_kind"`
	Outcome         string    `ch:"outcome"`
	AcceptedRecords uint32    `ch:"accepted_records"`
	RejectedRecords uint32    `ch:"rejected_records"`
	ErrorCode       string    `ch:"error_code"`
	TraceID         string    `ch:"trace_id"`
}

type AccessEventRow struct {
	EventDate           time.Time `ch:"event_date"`
	RecordedAt          time.Time `ch:"recorded_at"`
	OrgID               string    `ch:"org_id"`
	ProjectID           string    `ch:"project_id"`
	DatasetID           string    `ch:"dataset_id"`
	Environment         string    `ch:"environment"`
	SubjectType         string    `ch:"subject_type"`
	SubjectID           string    `ch:"subject_id"`
	OperationPermission string    `ch:"operation_permission"`
	ResourcePermission  string    `ch:"resource_permission"`
	Outcome             string    `ch:"outcome"`
	ResultCount         uint32    `ch:"result_count"`
	ErrorCode           string    `ch:"error_code"`
	TraceID             string    `ch:"trace_id"`
}

type QueryRequest struct {
	OrgID       string
	ProjectID   string
	DatasetID   string
	Environment string
	Limit       int
	From        time.Time
	To          time.Time
	EventName   string
	TraceID     string
	Raw         bool
}

type QueryEvent struct {
	EventID             string             `json:"event_id"`
	ObservedAt          string             `json:"observed_at"`
	Timestamp           string             `json:"timestamp"`
	OrgID               string             `json:"org_id"`
	ProjectID           string             `json:"project_id"`
	DatasetID           string             `json:"dataset_id"`
	Environment         string             `json:"environment"`
	SignalKind          string             `json:"signal_kind"`
	EventName           string             `json:"event_name"`
	SourceKind          string             `json:"source_kind"`
	ServiceName         string             `json:"service_name"`
	SeverityText        string             `json:"severity_text,omitempty"`
	Status              string             `json:"status,omitempty"`
	TraceID             string             `json:"trace_id,omitempty"`
	SpanID              string             `json:"span_id,omitempty"`
	DurationMs          uint64             `json:"duration_ms,omitempty"`
	Body                string             `json:"body,omitempty"`
	BodyRedactionStatus string             `json:"body_redaction_status,omitempty"`
	StringAttributes    map[string]string  `json:"string_attributes,omitempty"`
	IntAttributes       map[string]int64   `json:"int_attributes,omitempty"`
	FloatAttributes     map[string]float64 `json:"float_attributes,omitempty"`
	BoolAttributes      map[string]uint8   `json:"bool_attributes,omitempty"`
}

type QueryEventsPage struct {
	Events []QueryEvent `json:"events"`
	Limit  int          `json:"limit"`
}

func DatasetResourceID(projectID, datasetID string) string {
	return "project_" + opaqueID(projectID) + "_dataset_" + opaqueID(datasetID)
}

func (d DatasetContext) Normalized() DatasetContext {
	return DatasetContext{
		OrgID:       strings.TrimSpace(d.OrgID),
		ProjectID:   strings.TrimSpace(d.ProjectID),
		DatasetID:   strings.TrimSpace(d.DatasetID),
		Environment: strings.TrimSpace(d.Environment),
	}
}

func opaqueID(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(value)))
}
