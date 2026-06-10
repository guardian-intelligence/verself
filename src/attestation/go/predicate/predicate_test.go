package predicate

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validProvenance() (subjects []Subject, prov SLSAProvenance) {
	subjects = []Subject{{
		Name:   "ghcr.io/verself/analytics-service@sha256:" + strings.Repeat("a", 64),
		SHA256: strings.Repeat("a", 64),
	}}
	prov = SLSAProvenance{
		BuildType: BazelOCIBuild,
		ExternalParameters: ExternalParameters{
			BazelTarget:     "//src/services/analytics-service:image",
			BazelPushTarget: "//src/services/analytics-service:push",
			OCIRepository:   "verself/analytics-service",
			Site:            "gamma",
		},
		ResolvedDependencies: []ResolvedDependency{{
			URI:       "git+https://github.com/guardian-intelligence/verself-sh@" + strings.Repeat("b", 40),
			GitCommit: strings.Repeat("b", 40),
		}},
		BuilderID:    "spiffe://verself.gamma/svc/deployment-service",
		InvocationID: "deploy-run-0001",
	}
	return
}

func TestNewProvenanceRoundTrip(t *testing.T) {
	subjects, prov := validProvenance()
	stmt, err := NewProvenance(subjects, prov)
	if err != nil {
		t.Fatalf("NewProvenance: %v", err)
	}

	// Parse must recover an equal typed statement from the sealed bytes.
	parsed, err := Parse(stmt.Bytes())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !bytes.Equal(parsed.Bytes(), stmt.Bytes()) {
		t.Fatalf("Parse retained different bytes than the sealed statement")
	}
	got, ok := parsed.Predicate().(SLSAProvenance)
	if !ok {
		t.Fatalf("predicate type = %T, want SLSAProvenance", parsed.Predicate())
	}
	if got.BuildType != prov.BuildType || got.BuilderID != prov.BuilderID || got.InvocationID != prov.InvocationID {
		t.Fatalf("predicate round-trip mismatch: %+v", got)
	}
	if got.ExternalParameters != prov.ExternalParameters {
		t.Fatalf("externalParameters mismatch: %+v", got.ExternalParameters)
	}
	if len(got.ResolvedDependencies) != 1 || got.ResolvedDependencies[0] != prov.ResolvedDependencies[0] {
		t.Fatalf("resolvedDependencies mismatch: %+v", got.ResolvedDependencies)
	}
	if _, ok := parsed.SubjectMatching(subjects[0].SHA256); !ok {
		t.Fatalf("SubjectMatching failed for the attested digest")
	}
	if _, ok := parsed.SubjectMatching(strings.Repeat("c", 64)); ok {
		t.Fatalf("SubjectMatching matched a digest not in the statement")
	}
}

func TestBytesAreSealedCopies(t *testing.T) {
	subjects, prov := validProvenance()
	stmt, err := NewProvenance(subjects, prov)
	if err != nil {
		t.Fatalf("NewProvenance: %v", err)
	}
	first := stmt.Bytes()
	first[0] = 'X' // mutate the returned copy
	if bytes.Equal(stmt.Bytes(), first) {
		t.Fatalf("Bytes() returned a mutable reference to sealed state")
	}
}

func TestNewProvenanceRejectsBadInput(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(subjects *[]Subject, prov *SLSAProvenance)
		wantErr  error
	}{
		{"no subjects", func(s *[]Subject, _ *SLSAProvenance) { *s = nil }, ErrNoSubjects},
		{"uppercase digest", func(s *[]Subject, _ *SLSAProvenance) { (*s)[0].SHA256 = strings.Repeat("A", 64) }, ErrInvalidSubjectDigest},
		{"short digest", func(s *[]Subject, _ *SLSAProvenance) { (*s)[0].SHA256 = "abc" }, ErrInvalidSubjectDigest},
		{"prefixed digest", func(s *[]Subject, _ *SLSAProvenance) { (*s)[0].SHA256 = "sha256:" + strings.Repeat("a", 64) }, ErrInvalidSubjectDigest},
		{"unknown build type", func(_ *[]Subject, p *SLSAProvenance) { p.BuildType = "https://evil.example/build" }, ErrUnknownBuildType},
		{"missing builder", func(_ *[]Subject, p *SLSAProvenance) { p.BuilderID = "" }, ErrIncompletePredicate},
		{"missing invocation", func(_ *[]Subject, p *SLSAProvenance) { p.InvocationID = "" }, ErrIncompletePredicate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subjects, prov := validProvenance()
			tc.mutate(&subjects, &prov)
			if _, err := NewProvenance(subjects, prov); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseRejectsBadStatements(t *testing.T) {
	subjects, prov := validProvenance()
	good, err := NewProvenance(subjects, prov)
	if err != nil {
		t.Fatalf("NewProvenance: %v", err)
	}
	base := map[string]any{}
	if err := json.Unmarshal(good.Bytes(), &base); err != nil {
		t.Fatalf("unmarshal good statement: %v", err)
	}

	rewrite := func(mutate func(m map[string]any)) []byte {
		clone := map[string]any{}
		raw, _ := json.Marshal(base)
		_ = json.Unmarshal(raw, &clone)
		mutate(clone)
		out, _ := json.Marshal(clone)
		return out
	}

	cases := []struct {
		name    string
		body    []byte
		wantErr error
	}{
		{
			name:    "unknown statement type",
			body:    rewrite(func(m map[string]any) { m["_type"] = "https://in-toto.io/Statement/v0.1" }),
			wantErr: ErrUnknownStatementType,
		},
		{
			name:    "unknown predicate type",
			body:    rewrite(func(m map[string]any) { m["predicateType"] = "https://slsa.dev/provenance/v0.2" }),
			wantErr: ErrUnknownPredicateType,
		},
		{
			name: "unknown build type",
			body: rewrite(func(m map[string]any) {
				pred := m["predicate"].(map[string]any)
				pred["buildDefinition"].(map[string]any)["buildType"] = "https://evil.example/build"
			}),
			wantErr: ErrUnknownBuildType,
		},
		{
			name: "unknown predicate field",
			body: rewrite(func(m map[string]any) {
				m["predicate"].(map[string]any)["smuggled"] = "value"
			}),
			wantErr: ErrMalformedStatement,
		},
		{
			name: "bad subject digest",
			body: rewrite(func(m map[string]any) {
				subj := m["subject"].([]any)[0].(map[string]any)
				subj["digest"].(map[string]any)["sha256"] = strings.Repeat("Z", 64)
			}),
			wantErr: ErrInvalidSubjectDigest,
		},
		{
			name:    "garbage",
			body:    []byte("{not json"),
			wantErr: ErrMalformedStatement,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.body); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
