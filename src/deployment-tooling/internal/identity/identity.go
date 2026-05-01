// Package identity reads the verself deploy identity from the process
// environment and projects it onto an outgoing OTel context as W3C
// baggage.
//
// The shared verselfotel package registers a SpanProcessor that copies
// every baggage member with key prefix `verself.` onto each started
// span, so callers do not set per-span attributes; injecting once at
// process start is enough.
//
// The env vars consumed here are the ones written by
// scripts/deploy_identity.sh. The script remains the single source of
// truth in Phase 1; Phase 4 ports the script's logic into Go.
package identity

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/baggage"
)

// Field is a single (env var, baggage key) pair. The env var is the
// upstream contract with deploy_identity.sh; the baggage key is the
// downstream contract with the verselfotel SpanProcessor.
type Field struct {
	Env     string
	Baggage string
}

// Fields enumerates the identity members projected onto every span.
// Adding a new dimension means adding a row here and (if it should land
// on every span) confirming that verselfotel.BaggageAttributePrefix
// covers the baggage key.
var Fields = []Field{
	{Env: "VERSELF_DEPLOY_RUN_KEY", Baggage: "verself.deploy_run_key"},
	{Env: "VERSELF_DEPLOY_ID", Baggage: "verself.deploy_id"},
	{Env: "VERSELF_SITE", Baggage: "verself.site"},
	{Env: "VERSELF_AUTHOR", Baggage: "verself.author"},
	{Env: "VERSELF_COMMIT_SHA", Baggage: "verself.commit_sha"},
	{Env: "VERSELF_DEPLOY_SCOPE", Baggage: "verself.deploy_scope"},
	{Env: "VERSELF_DEPLOY_KIND", Baggage: "verself.deploy_kind"},
}

// Inject derives baggage members from the process environment and
// returns a context carrying them. Members with empty env values are
// skipped, so a partially-populated environment (e.g. a developer
// running the binary directly) yields a partial baggage set rather
// than a hard failure.
func Inject(ctx context.Context) context.Context {
	members := make([]baggage.Member, 0, len(Fields))
	for _, f := range Fields {
		v := os.Getenv(f.Env)
		if v == "" {
			continue
		}
		m, err := baggage.NewMemberRaw(f.Baggage, v)
		if err != nil {
			// Baggage values are restricted ASCII; identity values
			// (run_key, deploy_id, site, sha) all fit. An invalid value
			// is a deploy_identity.sh bug — skip it rather than fail
			// the deploy.
			continue
		}
		members = append(members, m)
	}
	if len(members) == 0 {
		return ctx
	}
	bag, err := baggage.New(members...)
	if err != nil {
		return ctx
	}
	return baggage.ContextWithBaggage(ctx, bag)
}
