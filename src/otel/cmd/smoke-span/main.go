package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	verselfotel "github.com/verself/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func main() {
	ctx := context.Background()
	spans, err := smokeSpans()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	serviceName := strings.TrimSpace(os.Getenv("SMOKE_SPAN_SERVICE"))
	if serviceName == "" {
		serviceName = "smoke-runner"
	}

	shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    serviceName,
		ServiceVersion: "1.0.0",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize otel: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "flush otel: %v\n", err)
		}
	}()

	tracer := otel.Tracer(serviceName)
	for _, spec := range spans {
		attrs, err := smokeAttrs(spec.Attributes)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		_, span := tracer.Start(ctx, spec.Name)
		span.SetAttributes(attrs...)
		span.SetStatus(codes.Ok, "")
		span.End()
	}
}

type smokeSpan struct {
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
}

func smokeSpans() ([]smokeSpan, error) {
	raw := strings.TrimSpace(os.Getenv("SMOKE_SPANS_JSON"))
	if raw != "" {
		var spans []smokeSpan
		if err := json.Unmarshal([]byte(raw), &spans); err != nil {
			return nil, fmt.Errorf("decode SMOKE_SPANS_JSON: %w", err)
		}
		if len(spans) == 0 {
			return nil, fmt.Errorf("SMOKE_SPANS_JSON must contain at least one span")
		}
		for i := range spans {
			spans[i].Name = strings.TrimSpace(spans[i].Name)
			if spans[i].Name == "" {
				return nil, fmt.Errorf("SMOKE_SPANS_JSON[%d].name is required", i)
			}
			if spans[i].Attributes == nil {
				spans[i].Attributes = map[string]any{}
			}
		}
		return spans, nil
	}

	spanName := strings.TrimSpace(os.Getenv("SMOKE_SPAN_NAME"))
	if spanName == "" {
		return nil, fmt.Errorf("SMOKE_SPAN_NAME is required")
	}

	attrs, err := smokeAttrPayload()
	if err != nil {
		return nil, err
	}
	return []smokeSpan{{Name: spanName, Attributes: attrs}}, nil
}

func smokeAttrPayload() (map[string]any, error) {
	raw := strings.TrimSpace(os.Getenv("SMOKE_SPAN_ATTRS_JSON"))
	if raw == "" {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("decode SMOKE_SPAN_ATTRS_JSON: %w", err)
	}
	return payload, nil
}

func smokeAttrs(payload map[string]any) ([]attribute.KeyValue, error) {
	if payload == nil {
		return nil, nil
	}
	attrs := make([]attribute.KeyValue, 0, len(payload))
	for key, value := range payload {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		switch typed := value.(type) {
		case bool:
			attrs = append(attrs, attribute.Bool(key, typed))
		case float64:
			attrs = append(attrs, attribute.Float64(key, typed))
		case string:
			attrs = append(attrs, attribute.String(key, typed))
		default:
			encoded, err := json.Marshal(typed)
			if err != nil {
				return nil, fmt.Errorf("encode span attribute %q: %w", key, err)
			}
			attrs = append(attrs, attribute.String(key, string(encoded)))
		}
	}
	return attrs, nil
}
