// Package ledger writes deploy_events and deploy_layer_runs rows
// through the typed chwriter. Replaces scripts/record-deploy-event.sh
// and scripts/record-layer-run.sh — same column shape, server-side
// timestamping, but with Go-side validation and SQL escaping in place
// of bash + python heredocs.
package ledger

import (
	"context"
	"fmt"
	"regexp"

	"github.com/verself/deployment-tooling/internal/chwriter"
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

// LayerEventKind enumerates verself.deploy_layer_runs.event_kind.
type LayerEventKind string

const (
	LayerStarted   LayerEventKind = "started"
	LayerSucceeded LayerEventKind = "succeeded"
	LayerFailed    LayerEventKind = "failed"
	LayerSkipped   LayerEventKind = "skipped"
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

// LayerRun is one row in verself.deploy_layer_runs. event_at is
// stamped server-side. LastAppliedHash may be empty on the layer's
// first-ever run.
type LayerRun struct {
	RunKey           string
	Site             string
	Layer            string
	InputHash        string
	LastAppliedHash  string
	Kind             LayerEventKind
	Skipped          bool
	SkipReason       string
	DurationMs       uint32
	ChangedCount     uint32
	ErrorMessage     string
}

// Writer wraps a chwriter scoped at the verself database.
type Writer struct {
	ch *chwriter.Writer
}

// New constructs a Writer from a chwriter.Writer; the writer must
// have been built against the verself database (chwriter.New(ssh,
// "verself")) — passing a different database silently inserts the
// wrong table.
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

// RecordLayerRun appends one row to verself.deploy_layer_runs.
func (w *Writer) RecordLayerRun(ctx context.Context, lr LayerRun) error {
	if w == nil || w.ch == nil {
		return fmt.Errorf("ledger: nil writer")
	}
	if lr.RunKey == "" {
		return fmt.Errorf("ledger: layer run missing RunKey")
	}
	if !sha64Re.MatchString(lr.InputHash) {
		return fmt.Errorf("ledger: layer run input hash %q must be 64-character hex", lr.InputHash)
	}
	if lr.LastAppliedHash != "" && !sha64Re.MatchString(lr.LastAppliedHash) {
		return fmt.Errorf("ledger: layer run last_applied_hash %q must be empty or 64-character hex", lr.LastAppliedHash)
	}
	switch lr.Kind {
	case LayerStarted, LayerSucceeded, LayerFailed, LayerSkipped:
	default:
		return fmt.Errorf("ledger: unknown layer event kind %q", lr.Kind)
	}
	last := lr.LastAppliedHash
	if last == "" {
		// FixedString(64) requires a populated value when the column
		// list is explicit. Pad with zeros to match the bash script's
		// "no prior evidence" sentinel.
		last = zero64
	}
	skipped := uint64(0)
	if lr.Skipped {
		skipped = 1
	}
	row := chwriter.Row{
		"event_at":          chwriter.DateTimeNow(),
		"deploy_run_key":    chwriter.String(lr.RunKey),
		"site":              chwriter.String(lr.Site),
		"layer":             chwriter.String(lr.Layer),
		"input_hash":        chwriter.String(lr.InputHash),
		"last_applied_hash": chwriter.String(last),
		"event_kind":        chwriter.String(string(lr.Kind)),
		"skipped":           chwriter.UInt(skipped),
		"skip_reason":       chwriter.String(lr.SkipReason),
		"duration_ms":       chwriter.UInt(uint64(lr.DurationMs)),
		"changed_count":     chwriter.UInt(uint64(lr.ChangedCount)),
		"error_message":     chwriter.String(lr.ErrorMessage),
	}
	return w.ch.Insert(ctx, "deploy_layer_runs", row)
}

// LastAppliedHash returns the input_hash of the most recent
// succeeded-or-skipped run for (site, layer). Returns the empty
// string when no row matches (first-ever run). Replaces
// scripts/layer-last-applied.sh.
func (w *Writer) LastAppliedHash(ctx context.Context, site, layer string) (string, error) {
	if w == nil || w.ch == nil {
		return "", fmt.Errorf("ledger: nil writer")
	}
	if site == "" || layer == "" {
		return "", fmt.Errorf("ledger: site and layer are required")
	}
	q := fmt.Sprintf(
		"SELECT argMax(input_hash, event_at) FROM verself.deploy_layer_runs "+
			"WHERE site = %s AND layer = %s AND event_kind IN ('succeeded', 'skipped') "+
			"FORMAT TSVRaw",
		chRenderString(site), chRenderString(layer),
	)
	out, err := w.ch.QueryString(ctx, q)
	if err != nil {
		return "", err
	}
	out = trimWhitespace(out)
	if out == "" || out == zero64 {
		return "", nil
	}
	if !sha64Re.MatchString(out) {
		return "", fmt.Errorf("ledger: layer-last-applied returned non-hash value: %q", out)
	}
	return out, nil
}

var (
	sha40Re = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha64Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const zero64 = "0000000000000000000000000000000000000000000000000000000000000000"

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// chRenderString produces a ClickHouse string literal. Mirrors
// chwriter.String but exported here to avoid pulling chwriter's
// internal API into ledger; only LastAppliedHash uses it because
// SELECTs aren't a chwriter primitive.
func chRenderString(s string) string {
	// Same backslash + apostrophe escaping as chwriter.String.
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '\'' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	out = append(out, '\'')
	return string(out)
}

func trimWhitespace(s string) string {
	// Drop leading/trailing ASCII whitespace including \r and \n.
	start := 0
	for start < len(s) && isWhitespace(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isWhitespace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}
