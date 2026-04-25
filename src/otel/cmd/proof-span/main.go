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
	spanName := strings.TrimSpace(os.Getenv("PROOF_SPAN_NAME"))
	if spanName == "" {
		fmt.Fprintln(os.Stderr, "PROOF_SPAN_NAME is required")
		os.Exit(2)
	}
	serviceName := strings.TrimSpace(os.Getenv("PROOF_SPAN_SERVICE"))
	if serviceName == "" {
		serviceName = "proof-runner"
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

	attrs, err := proofAttrs()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_, span := otel.Tracer(serviceName).Start(ctx, spanName)
	span.SetAttributes(attrs...)
	span.SetStatus(codes.Ok, "")
	span.End()
}

func proofAttrs() ([]attribute.KeyValue, error) {
	raw := strings.TrimSpace(os.Getenv("PROOF_SPAN_ATTRS_JSON"))
	if raw == "" {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("decode PROOF_SPAN_ATTRS_JSON: %w", err)
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
