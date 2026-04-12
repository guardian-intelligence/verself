package fmotel

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	headerDeployID        = "X-Forge-Metal-Deploy-Id"
	headerDeployRunKey    = "X-Forge-Metal-Deploy-Run-Key"
	headerTaskTemplateID  = "X-Forge-Metal-Task-Template-Id"
	headerTaskInstanceID  = "X-Forge-Metal-Task-Instance-Id"
	headerProbeID         = "X-Forge-Metal-Probe-Id"
	headerVerificationRun = "X-Forge-Metal-Verification-Run"
	headerCorrelationID   = "X-Forge-Metal-Correlation-Id"
)

const (
	attrDeployID        = "forge_metal.deploy_id"
	attrDeployRunKey    = "forge_metal.deploy_run_key"
	attrTaskTemplateID  = "forge_metal.task_template_id"
	attrTaskInstanceID  = "forge_metal.task_instance_id"
	attrProbeID         = "forge_metal.probe_id"
	attrVerificationRun = "forge_metal.verification_run"
	attrCorrelationID   = "forge_metal.correlation_id"
)

const maxCorrelationValueLen = 128

type correlationContextKey struct{}

// CorrelationMetadata carries low-cardinality observability metadata that
// arrives on inbound requests. It is not used for authz or any other control
// flow.
type CorrelationMetadata struct {
	DeployID        string
	DeployRunKey    string
	TaskTemplateID  string
	TaskInstanceID  string
	ProbeID         string
	VerificationRun string
	CorrelationID   string
}

// CorrelationFromHeaders extracts and sanitizes the forge-metal correlation
// headers from the request.
func CorrelationFromHeaders(h http.Header) CorrelationMetadata {
	return CorrelationMetadata{
		DeployID:        sanitizeDeployID(h.Get(headerDeployID)),
		DeployRunKey:    sanitizeCorrelationValue(h.Get(headerDeployRunKey)),
		TaskTemplateID:  sanitizeCorrelationValue(h.Get(headerTaskTemplateID)),
		TaskInstanceID:  sanitizeCorrelationValue(h.Get(headerTaskInstanceID)),
		ProbeID:         sanitizeCorrelationValue(h.Get(headerProbeID)),
		VerificationRun: sanitizeCorrelationValue(h.Get(headerVerificationRun)),
		CorrelationID:   sanitizeCorrelationValue(h.Get(headerCorrelationID)),
	}
}

// CorrelationFromContext returns any metadata previously attached by
// CorrelationMiddleware.
func CorrelationFromContext(ctx context.Context) (CorrelationMetadata, bool) {
	meta, ok := ctx.Value(correlationContextKey{}).(CorrelationMetadata)
	return meta, ok
}

// CorrelationMiddleware attaches sanitized correlation metadata to the
// request context and active span, if present.
func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := CorrelationFromHeaders(r.Header)
		if meta.isZero() {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), correlationContextKey{}, meta)
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(meta.Attributes()...)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Attributes converts the metadata into low-cardinality span attributes.
func (m CorrelationMetadata) Attributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 7)
	appendAttr := func(key, value string) {
		if value != "" {
			attrs = append(attrs, attribute.String(key, value))
		}
	}

	appendAttr(attrDeployID, m.DeployID)
	appendAttr(attrDeployRunKey, m.DeployRunKey)
	appendAttr(attrTaskTemplateID, m.TaskTemplateID)
	appendAttr(attrTaskInstanceID, m.TaskInstanceID)
	appendAttr(attrProbeID, m.ProbeID)
	appendAttr(attrVerificationRun, m.VerificationRun)
	appendAttr(attrCorrelationID, m.CorrelationID)
	return attrs
}

func (m CorrelationMetadata) isZero() bool {
	return m.DeployID == "" &&
		m.DeployRunKey == "" &&
		m.TaskTemplateID == "" &&
		m.TaskInstanceID == "" &&
		m.ProbeID == "" &&
		m.VerificationRun == "" &&
		m.CorrelationID == ""
}

func sanitizeDeployID(value string) string {
	value = sanitizeCorrelationValue(value)
	if value == "" {
		return ""
	}

	parsed, err := uuid.Parse(value)
	if err != nil {
		return value
	}
	return parsed.String()
}

func sanitizeCorrelationValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxCorrelationValueLen || !utf8.ValidString(value) {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return value
}
