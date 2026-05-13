package analytics

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	maxRecordsPerRequest    = 500
	maxAttributesPerRecord  = 80
	maxAttributeKeyBytes    = 128
	maxStringAttributeBytes = 2048
	nanosPerSecond          = 1_000_000_000
)

var (
	eventNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
	secretPatterns   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`),
	}
)

func DecodeLogsRequest(contentType string, body []byte) (*logspb.ExportLogsServiceRequest, error) {
	req := &logspb.ExportLogsServiceRequest{}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "application/json", "":
		unmarshal := protojson.UnmarshalOptions{DiscardUnknown: true}
		if err := unmarshal.Unmarshal(body, req); err != nil {
			return nil, fmt.Errorf("decode otlp json logs: %w", err)
		}
	case "application/x-protobuf":
		if err := proto.Unmarshal(body, req); err != nil {
			return nil, fmt.Errorf("decode otlp protobuf logs: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported content type %q", contentType)
	}
	return req, nil
}

func EncodeLogsResponse(contentType string) ([]byte, string, error) {
	resp := &logspb.ExportLogsServiceResponse{}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType == "application/x-protobuf" {
		data, err := proto.Marshal(resp)
		return data, "application/x-protobuf", err
	}
	marshal := protojson.MarshalOptions{UseProtoNames: false}
	data, err := marshal.Marshal(resp)
	return data, "application/json", err
}

func RowsFromLogs(dataset DatasetContext, source Source, req *logspb.ExportLogsServiceRequest, allowedPrefixes []string, now time.Time) ([]EventRow, error) {
	dataset = dataset.Normalized()
	if err := validateDatasetContext(dataset); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("otlp logs request is nil")
	}
	var rows []EventRow
	for _, resourceLogs := range req.ResourceLogs {
		resourceAttrs, err := scalarAttributes(resourceLogs.GetResource().GetAttributes())
		if err != nil {
			return nil, fmt.Errorf("resource attributes: %w", err)
		}
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			for _, record := range scopeLogs.LogRecords {
				if len(rows) >= maxRecordsPerRequest {
					return nil, fmt.Errorf("too many log records: max %d", maxRecordsPerRequest)
				}
				row, err := rowFromLogRecord(dataset, source, resourceLogs.GetResource(), resourceAttrs, record.GetTraceId(), record.GetSpanId(), record.GetTimeUnixNano(), record.GetObservedTimeUnixNano(), record.GetBody(), record.GetSeverityText(), record.GetAttributes(), allowedPrefixes, now)
				if err != nil {
					return nil, err
				}
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func rowFromLogRecord(
	dataset DatasetContext,
	source Source,
	_ *resourcepb.Resource,
	resourceAttrs scalarAttributeMaps,
	traceID []byte,
	spanID []byte,
	timeUnixNano uint64,
	observedTimeUnixNano uint64,
	body *commonpb.AnyValue,
	severityText string,
	attributes []*commonpb.KeyValue,
	allowedPrefixes []string,
	now time.Time,
) (EventRow, error) {
	attrs, err := scalarAttributes(attributes)
	if err != nil {
		return EventRow{}, fmt.Errorf("log attributes: %w", err)
	}
	eventName := firstNonEmpty(attrs.Strings["event.name"], stringValue(body))
	if err := validateEventName(eventName, allowedPrefixes); err != nil {
		return EventRow{}, err
	}
	timestamp := timestampFromNanos(timeUnixNano)
	observedAt := timestampFromNanos(observedTimeUnixNano)
	if observedAt.IsZero() {
		observedAt = timestamp
	}
	if observedAt.IsZero() {
		observedAt = now.UTC()
	}
	if timestamp.IsZero() {
		timestamp = observedAt
	}
	eventID := firstNonEmpty(attrs.Strings["event.id"], randomHex(16))
	durationMs := uint64FromAttributes(attrs, "duration_ms")
	bodyText, redactionStatus := redactedBody(stringValue(body))
	stringAttrs := safeStringAttributes(attrs.Strings)
	annotateSourceAttributes(stringAttrs, source)
	return EventRow{
		EventDate:           observedAt.Truncate(24 * time.Hour),
		ObservedAt:          observedAt,
		Timestamp:           timestamp,
		EventID:             eventID,
		OrgID:               dataset.OrgID,
		ProjectID:           dataset.ProjectID,
		DatasetID:           dataset.DatasetID,
		Environment:         dataset.Environment,
		SignalKind:          SignalKindLog,
		EventName:           eventName,
		SourceKind:          source.Kind,
		SourceSubject:       source.Subject,
		ServiceName:         resourceAttrs.Strings["service.name"],
		ServiceVersion:      resourceAttrs.Strings["service.version"],
		SeverityText:        strings.TrimSpace(severityText),
		Status:              firstNonEmpty(attrs.Strings["status"], attrs.Strings["status.code"]),
		TraceID:             hex.EncodeToString(traceID),
		SpanID:              hex.EncodeToString(spanID),
		ParentSpanID:        attrs.Strings["parent_span_id"],
		DurationMs:          durationMs,
		Body:                bodyText,
		BodyRedactionStatus: redactionStatus,
		StringAttributes:    stringAttrs,
		IntAttributes:       attrs.Ints,
		FloatAttributes:     attrs.Floats,
		BoolAttributes:      attrs.Bools,
	}, nil
}

type scalarAttributeMaps struct {
	Strings map[string]string
	Ints    map[string]int64
	Floats  map[string]float64
	Bools   map[string]uint8
}

func scalarAttributes(attrs []*commonpb.KeyValue) (scalarAttributeMaps, error) {
	if len(attrs) > maxAttributesPerRecord {
		return scalarAttributeMaps{}, fmt.Errorf("too many attributes: max %d", maxAttributesPerRecord)
	}
	out := scalarAttributeMaps{
		Strings: map[string]string{},
		Ints:    map[string]int64{},
		Floats:  map[string]float64{},
		Bools:   map[string]uint8{},
	}
	for _, attr := range attrs {
		key := strings.TrimSpace(attr.GetKey())
		if key == "" {
			return scalarAttributeMaps{}, fmt.Errorf("empty attribute key")
		}
		if len(key) > maxAttributeKeyBytes {
			return scalarAttributeMaps{}, fmt.Errorf("attribute key %q exceeds %d bytes", key, maxAttributeKeyBytes)
		}
		if strings.HasPrefix(key, "analytics.") || strings.HasPrefix(key, "verself.") {
			continue
		}
		if err := putScalar(&out, key, attr.GetValue()); err != nil {
			return scalarAttributeMaps{}, err
		}
	}
	return out, nil
}

func putScalar(out *scalarAttributeMaps, key string, value *commonpb.AnyValue) error {
	switch typed := value.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		if len(typed.StringValue) > maxStringAttributeBytes {
			return fmt.Errorf("string attribute %q exceeds %d bytes", key, maxStringAttributeBytes)
		}
		out.Strings[key] = typed.StringValue
	case *commonpb.AnyValue_IntValue:
		out.Ints[key] = typed.IntValue
	case *commonpb.AnyValue_DoubleValue:
		out.Floats[key] = typed.DoubleValue
	case *commonpb.AnyValue_BoolValue:
		if typed.BoolValue {
			out.Bools[key] = 1
		} else {
			out.Bools[key] = 0
		}
	default:
		return fmt.Errorf("attribute %q uses unsupported non-scalar OTLP value", key)
	}
	return nil
}

func validateDatasetContext(dataset DatasetContext) error {
	if dataset.OrgID == "" {
		return fmt.Errorf("dataset org_id is required")
	}
	if dataset.ProjectID == "" {
		return fmt.Errorf("dataset project_id is required")
	}
	if dataset.DatasetID == "" {
		return fmt.Errorf("dataset_id is required")
	}
	if dataset.Environment == "" {
		return fmt.Errorf("dataset environment is required")
	}
	return nil
}

func validateEventName(eventName string, allowedPrefixes []string) error {
	eventName = strings.TrimSpace(eventName)
	if !eventNamePattern.MatchString(eventName) {
		return fmt.Errorf("invalid event name %q", eventName)
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(eventName, prefix) {
			return nil
		}
	}
	return fmt.Errorf("event name %q is not accepted by this dataset policy", eventName)
}

func uint64FromAttributes(attrs scalarAttributeMaps, key string) uint64 {
	if value, ok := attrs.Ints[key]; ok && value > 0 {
		return uint64(value)
	}
	if value, ok := attrs.Floats[key]; ok && value > 0 {
		return uint64(value)
	}
	return 0
}

func timestampFromNanos(value uint64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	seconds := value / nanosPerSecond
	if seconds > uint64(math.MaxInt64) {
		return time.Time{}
	}
	nanos := value % nanosPerSecond
	return time.Unix(int64(seconds), int64(nanos)).UTC() // #nosec G115 -- seconds and nanos are range-checked above.
}

func stringValue(value *commonpb.AnyValue) string {
	if value == nil {
		return ""
	}
	if typed, ok := value.GetValue().(*commonpb.AnyValue_StringValue); ok {
		return typed.StringValue
	}
	return ""
}

func safeStringAttributes(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for key, value := range attrs {
		out[key] = redactSecrets(value)
	}
	return out
}

func annotateSourceAttributes(attrs map[string]string, source Source) {
	if strings.TrimSpace(source.Repository) != "" {
		attrs["github.repository"] = source.Repository
	}
	if strings.TrimSpace(source.RepositoryOwner) != "" {
		attrs["github.repository_owner"] = source.RepositoryOwner
	}
	if strings.TrimSpace(source.Ref) != "" {
		attrs["git.ref"] = source.Ref
	}
	if strings.TrimSpace(source.SHA) != "" {
		attrs["git.sha"] = source.SHA
	}
	if strings.TrimSpace(source.RunID) != "" {
		attrs["github.run_id"] = source.RunID
	}
	if source.RunAttempt > 0 {
		attrs["github.run_attempt"] = fmt.Sprintf("%d", source.RunAttempt)
	}
	if strings.TrimSpace(source.Workflow) != "" {
		attrs["github.workflow"] = source.Workflow
	}
	if strings.TrimSpace(source.Job) != "" {
		attrs["github.job"] = source.Job
	}
}

func redactedBody(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "empty"
	}
	redacted := redactSecrets(value)
	if redacted != value {
		return redacted, "redacted"
	}
	return redacted, "not_redacted"
}

func redactSecrets(value string) string {
	out := value
	for _, pattern := range secretPatterns {
		out = pattern.ReplaceAllString(out, "[REDACTED]")
	}
	return out
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
