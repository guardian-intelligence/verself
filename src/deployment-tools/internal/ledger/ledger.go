// Package ledger writes deploy_events rows through the typed chwriter with
// server-side timestamping, Go-side validation, and SQL escaping.
package ledger

import (
	"context"
	"fmt"
	"regexp"

	"github.com/verself/deployment-tools/internal/chwriter"
)

// EventKind enumerates verself.deploy_events.event_kind. Exhaustive
// per the migration; passing an out-of-set value is rejected before
// the insert.
type EventKind string

const (
	EventStarted   EventKind = "started"
	EventSucceeded EventKind = "succeeded"
	EventFailed    EventKind = "failed"
)

// DeployEvent is one row in verself.deploy_events. event_at is
// stamped server-side via now64(9).
type DeployEvent struct {
	RunKey             string
	Site               string
	Sha                string
	Actor              string
	Scope              string
	AffectedComponents []string
	Kind               EventKind
	DurationMs         uint32
	ErrorMessage       string
}

// Writer wraps a chwriter scoped at the verself database.
type Writer struct {
	ch *chwriter.Writer
}

// New constructs a Writer from a chwriter.Writer; the writer must
// have been built against the verself database (chwriter.New(ssh,
// "verself")).
func New(ch *chwriter.Writer) *Writer { return &Writer{ch: ch} }

// RecordDeployEvent appends one row to verself.deploy_events.
// Validates required fields and rejects unknown event kinds.
func (w *Writer) RecordDeployEvent(ctx context.Context, ev DeployEvent) error {
	if w == nil || w.ch == nil {
		return fmt.Errorf("ledger: nil writer")
	}
	if ev.RunKey == "" {
		return fmt.Errorf("ledger: deploy event missing RunKey")
	}
	if !sha40Re.MatchString(ev.Sha) {
		return fmt.Errorf("ledger: deploy event sha %q must be 40-character hex", ev.Sha)
	}
	switch ev.Kind {
	case EventStarted, EventSucceeded, EventFailed:
	default:
		return fmt.Errorf("ledger: unknown deploy event kind %q", ev.Kind)
	}
	row := chwriter.Row{
		"event_at":            chwriter.DateTimeNow(),
		"deploy_run_key":      chwriter.String(ev.RunKey),
		"site":                chwriter.String(ev.Site),
		"sha":                 chwriter.String(ev.Sha),
		"actor":               chwriter.String(orDefault(ev.Actor, "unknown")),
		"scope":               chwriter.String(ev.Scope),
		"affected_components": chwriter.StringArray(ev.AffectedComponents),
		"event_kind":          chwriter.String(string(ev.Kind)),
		"duration_ms":         chwriter.UInt(uint64(ev.DurationMs)),
		"error_message":       chwriter.String(ev.ErrorMessage),
	}
	return w.ch.Insert(ctx, "deploy_events", row)
}

var sha40Re = regexp.MustCompile(`^[0-9a-f]{40}$`)

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
