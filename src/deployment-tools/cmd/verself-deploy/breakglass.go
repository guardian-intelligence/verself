package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/supplychain"
)

const (
	breakglassAllowProvisional = "supply-chain.provisional-artifacts"
	breakglassAllowRejected    = "supply-chain.rejected-artifacts"
)

type breakglassException struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Site          string   `json:"site"`
	SHA           string   `json:"sha"`
	ExpiresAt     string   `json:"expires_at"`
	Reason        string   `json:"reason"`
	Allows        []string `json:"allows"`
}

type loadedBreakglass struct {
	Exception breakglassException
	ExpiresAt time.Time
	Evidence  string
}

func loadBreakglassException(path, site, sha string, now time.Time) (loadedBreakglass, error) {
	if strings.TrimSpace(path) == "" {
		return loadedBreakglass{}, errors.New("breakglass path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return loadedBreakglass{}, fmt.Errorf("read breakglass exception: %w", err)
	}
	var exception breakglassException
	if err := json.Unmarshal(raw, &exception); err != nil {
		return loadedBreakglass{}, fmt.Errorf("parse breakglass exception: %w", err)
	}
	expiresAt, err := validateBreakglassException(exception, site, sha, now)
	if err != nil {
		return loadedBreakglass{}, err
	}
	return loadedBreakglass{Exception: exception, ExpiresAt: expiresAt, Evidence: string(raw)}, nil
}

func validateBreakglassException(exception breakglassException, site, sha string, now time.Time) (time.Time, error) {
	if exception.SchemaVersion != 1 {
		return time.Time{}, fmt.Errorf("unsupported breakglass schema_version %d", exception.SchemaVersion)
	}
	if strings.TrimSpace(exception.ID) == "" {
		return time.Time{}, errors.New("breakglass id is required")
	}
	if exception.Site != site {
		return time.Time{}, fmt.Errorf("breakglass site mismatch: exception=%s deploy=%s", exception.Site, site)
	}
	if exception.SHA != sha {
		return time.Time{}, fmt.Errorf("breakglass sha mismatch: exception=%s deploy=%s", exception.SHA, sha)
	}
	if len(strings.TrimSpace(exception.Reason)) < 20 {
		return time.Time{}, errors.New("breakglass reason must be at least 20 characters")
	}
	expiresAt, err := time.Parse(time.RFC3339, exception.ExpiresAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse breakglass expires_at: %w", err)
	}
	if !expiresAt.After(now.UTC()) {
		return time.Time{}, fmt.Errorf("breakglass exception expired at %s", expiresAt.Format(time.RFC3339))
	}
	if len(exception.Allows) == 0 {
		return time.Time{}, errors.New("breakglass allows is required")
	}
	for _, allow := range exception.Allows {
		switch allow {
		case breakglassAllowProvisional, breakglassAllowRejected:
		default:
			return time.Time{}, fmt.Errorf("unsupported breakglass allow %q", allow)
		}
	}
	return expiresAt.UTC(), nil
}

func validateBreakglassForSupplyChain(bg loadedBreakglass, eval supplychain.Evaluation) error {
	allows := map[string]bool{}
	for _, allow := range bg.Exception.Allows {
		allows[allow] = true
	}
	for _, result := range eval.Results {
		if result.PolicyResult != supplychain.ResultRejected {
			continue
		}
		if result.PolicyReason == "artifact is tracked but not admitted" {
			if !allows[breakglassAllowProvisional] {
				return fmt.Errorf("breakglass exception does not allow provisional artifact %s", result.Finding.ID)
			}
			continue
		}
		if !allows[breakglassAllowRejected] {
			return fmt.Errorf("breakglass exception does not allow rejected artifact %s: %s", result.Finding.ID, result.PolicyReason)
		}
	}
	return nil
}

func recordSupplyChainBreakglass(ctx context.Context, rt *runtime.Runtime, site, sha string, snap identity.Snapshot, bg loadedBreakglass, eval supplychain.Evaluation) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.breakglass.allow",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.String("verself.deploy_run_key", snap.RunKey()),
			attribute.String("breakglass.exception_id", bg.Exception.ID),
			attribute.Int64("supply_chain.rejected_count", int64(eval.Rejected)),
			attribute.Int64("supply_chain.provisional_count", int64(eval.Provisional)),
		),
	)
	defer span.End()

	traceID, spanID := spanIDs(span.SpanContext())
	row := deploydb.BreakglassEventRow{
		EventAt:           time.Now().UTC(),
		DeployRunKey:      snap.RunKey(),
		Site:              site,
		Sha:               sha,
		Actor:             snap.Get("VERSELF_AUTHOR"),
		ExceptionID:       bg.Exception.ID,
		ExpiresAt:         bg.ExpiresAt,
		Reason:            bg.Exception.Reason,
		AllowedResults:    append([]string{}, bg.Exception.Allows...),
		PolicyRejected:    eval.Rejected,
		PolicyProvisional: eval.Provisional,
		TraceID:           traceID,
		SpanID:            spanID,
		Evidence:          bg.Evidence,
	}
	if err := rt.DeployDB.InsertBreakglassEvents(ctx, []deploydb.BreakglassEventRow{row}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}
