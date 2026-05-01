// Package identity is the verself deploy's correlation surface: a
// single (run-key, deploy-id, site, sha, …) tuple is captured at
// process start, projected onto W3C baggage so every span emitted in
// the run carries it, and fanned out as env vars when a child
// process (ansible-playbook, reconciler scripts) needs to inherit
// the same correlation.
//
// Generate produces a fresh snapshot for the verself-deploy process;
// FromEnv re-reads one previously installed by a parent invocation
// so nested verself-deploy children share the parent's run key.
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
// Adding a new dimension means adding a row here and (if it should
// land on every span) confirming that verselfotel.BaggageAttributePrefix
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

// Snapshot is the env-resolved identity. Empty fields are dropped on
// projection; a partially-populated environment yields a partial
// baggage set rather than a hard failure.
type Snapshot struct {
	values map[string]string
}

// FromEnv reads every Field's env var into a Snapshot. Unset vars
// become empty strings; the Baggage() / Env() projections skip them.
func FromEnv() Snapshot {
	values := make(map[string]string, len(Fields))
	for _, f := range Fields {
		if v := os.Getenv(f.Env); v != "" {
			values[f.Env] = v
		}
	}
	return Snapshot{values: values}
}

// Get returns the value for the given env-var name (e.g. "VERSELF_SITE").
// Empty when the field was unset.
func (s Snapshot) Get(envVar string) string {
	return s.values[envVar]
}

// RunKey is a convenience for Get("VERSELF_DEPLOY_RUN_KEY"). Returns
// the empty string when the run key is not set — callers that need a
// hard failure should validate explicitly.
func (s Snapshot) RunKey() string { return s.values["VERSELF_DEPLOY_RUN_KEY"] }

// Site is a convenience for Get("VERSELF_SITE").
func (s Snapshot) Site() string { return s.values["VERSELF_SITE"] }

// Baggage projects the snapshot onto a W3C baggage set. The
// verselfotel SpanProcessor then copies any `verself.` member onto
// every started span, so a single ContextWithBaggage covers the
// whole run.
func (s Snapshot) Baggage() baggage.Baggage {
	if len(s.values) == 0 {
		return baggage.Baggage{}
	}
	members := make([]baggage.Member, 0, len(Fields))
	for _, f := range Fields {
		v := s.values[f.Env]
		if v == "" {
			continue
		}
		m, err := baggage.NewMemberRaw(f.Baggage, v)
		if err != nil {
			// Baggage values are restricted ASCII; identity values fit
			// in practice. An invalid value is a generator bug — drop
			// it rather than fail the deploy.
			continue
		}
		members = append(members, m)
	}
	if len(members) == 0 {
		return baggage.Baggage{}
	}
	bag, err := baggage.New(members...)
	if err != nil {
		return baggage.Baggage{}
	}
	return bag
}

// Env returns "KEY=VALUE" entries suitable for exec.Cmd.Env. Only
// the Fields subset (the closed verself.* projection) is emitted —
// callers needing the full closed set should append the parent's
// os.Environ() too. Order is the declaration order of Fields for
// stable test output.
func (s Snapshot) Env() []string {
	out := make([]string, 0, len(s.values))
	for _, f := range Fields {
		if v := s.values[f.Env]; v != "" {
			out = append(out, f.Env+"="+v)
		}
	}
	return out
}

// AllEnv is Env plus every other key the snapshot carries (TRACEPARENT,
// OTEL_*, derived git metadata). Use when threading identity into a
// child process that must inherit OTel correlation. Order is stable
// (Fields first, then sorted remainder) so test snapshots are
// reproducible.
func (s Snapshot) AllEnv() []string {
	out := make([]string, 0, len(s.values))
	seen := make(map[string]bool, len(Fields))
	for _, f := range Fields {
		if v := s.values[f.Env]; v != "" {
			out = append(out, f.Env+"="+v)
			seen[f.Env] = true
		}
	}
	rest := make([]string, 0, len(s.values))
	for k := range s.values {
		if seen[k] {
			continue
		}
		if v := s.values[k]; v != "" {
			rest = append(rest, k+"="+v)
		}
	}
	// stdlib sort would pull a third package import for a tiny set;
	// insertion sort is enough for the ~10 entries we have here.
	for i := 1; i < len(rest); i++ {
		j := i
		for j > 0 && rest[j-1] > rest[j] {
			rest[j-1], rest[j] = rest[j], rest[j-1]
			j--
		}
	}
	return append(out, rest...)
}

// Inject is the legacy entry point: read env, push onto baggage,
// return the new context. Equivalent to FromEnv().Baggage() applied
// via baggage.ContextWithBaggage. Kept for callers that just want
// the context without the snapshot.
func Inject(ctx context.Context) context.Context {
	bag := FromEnv().Baggage()
	if bag.Len() == 0 {
		return ctx
	}
	return baggage.ContextWithBaggage(ctx, bag)
}
