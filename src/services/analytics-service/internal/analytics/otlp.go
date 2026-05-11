package analytics

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
)

var eventNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

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

func RowsFromLogs(source Source, req *logspb.ExportLogsServiceRequest, allowedPrefixes []string, now time.Time) ([]EventRow, error) {
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
				row, err := rowFromLogRecord(source, resourceLogs.GetResource(), resourceAttrs, record.GetTraceId(), record.GetSpanId(), record.GetTimeUnixNano(), record.GetObservedTimeUnixNano(), record.GetBody(), record.GetAttributes(), allowedPrefixes, now)
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
	source Source,
	_ *resourcepb.Resource,
	resourceAttrs scalarAttributeMaps,
	traceID []byte,
	spanID []byte,
	timeUnixNano uint64,
	observedTimeUnixNano uint64,
	body *commonpb.AnyValue,
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
	observedAt := timestampFromNanos(timeUnixNano)
	if observedAt.IsZero() {
		observedAt = timestampFromNanos(observedTimeUnixNano)
	}
	if observedAt.IsZero() {
		observedAt = now.UTC()
	}
	eventID := firstNonEmpty(attrs.Strings["event.id"], randomHex(16))
	durationMs := uint64FromAttributes(attrs, "duration_ms")
	configPath := firstNonEmpty(attrs.Strings["build.config_path"], attrs.Strings["typescript.tsconfig_path"], attrs.Strings["vite.config_path"])
	cacheSource := firstNonEmpty(attrs.Strings["cache.source"], attrs.Strings["viteplus.cache.source"], attrs.Strings["typescript.tsbuildinfo.source"])
	cacheResult := firstNonEmpty(attrs.Strings["cache.result"], attrs.Strings["viteplus.cache.result"], attrs.Strings["typescript.tsbuildinfo.result"])
	return EventRow{
		EventDate:          observedAt.Truncate(24 * time.Hour),
		ObservedAt:         observedAt,
		EventID:            eventID,
		Dataset:            firstNonEmpty(attrs.Strings["analytics.dataset"], DatasetBuild),
		EventName:          eventName,
		SourceKind:         source.Kind,
		SourceSubject:      source.Subject,
		TenantID:           source.TenantID,
		Repository:         source.Repository,
		RepositoryOwner:    source.RepositoryOwner,
		GitRef:             source.Ref,
		GitSHA:             source.SHA,
		ProviderRunID:      source.RunID,
		ProviderRunAttempt: source.RunAttempt,
		ProviderWorkflow:   source.Workflow,
		ProviderJob:        firstNonEmpty(attrs.Strings["github.job"], source.Job),
		ServiceName:        resourceAttrs.Strings["service.name"],
		ServiceVersion:     resourceAttrs.Strings["service.version"],
		TraceID:            hex.EncodeToString(traceID),
		SpanID:             hex.EncodeToString(spanID),
		BuildTool:          attrs.Strings["build.tool"],
		BuildPackage:       attrs.Strings["build.package"],
		BuildCommand:       attrs.Strings["build.command"],
		BuildTarget:        firstNonEmpty(attrs.Strings["build.target"], attrs.Strings["bazel.target_label"]),
		ConfigPath:         configPath,
		CacheSource:        cacheSource,
		CacheResult:        cacheResult,
		CacheReason:        firstNonEmpty(attrs.Strings["cache.reason"], attrs.Strings["viteplus.cache.reason"]),
		Status:             attrs.Strings["status"],
		DurationMs:         durationMs,
		StringAttributes:   attrs.Strings,
		IntAttributes:      attrs.Ints,
		FloatAttributes:    attrs.Floats,
		BoolAttributes:     attrs.Bools,
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
	seconds := int64(value / 1_000_000_000)
	nanos := int64(value % 1_000_000_000)
	return time.Unix(seconds, nanos).UTC()
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
