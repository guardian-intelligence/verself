// Package predicate is the L1 layer of the attestation module: typed, sealed
// in-toto statements. It owns the closed registries of predicate types and
// build types, and it is the ONLY place hand-rolling a statement is acceptable
// — because it is done once, behind a constructor, against the official in-toto
// protos. See src/attestation/AGENTS.md for the trust contract.
//
// Two invariants live here and nowhere else:
//
//   - A Statement serializes exactly once, in its constructor, and Bytes()
//     returns that sealed buffer forever. There is no Marshal(). protojson is
//     deliberately non-deterministic; re-serializing after signing would
//     produce bytes that no longer verify.
//   - Parse is strict: unknown _type, predicateType, or buildType, a malformed
//     subject digest, zero subjects, or any unknown predicate field is a tagged
//     error, never a silent default.
package predicate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	intoto "github.com/in-toto/attestation/go/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	// StatementType is the in-toto Statement v1 _type URI.
	StatementType = "https://in-toto.io/Statement/v1"
	// PredicateTypeSLSAProvenance is the SLSA provenance v1 predicateType URI.
	// cosign maps this to the "slsaprovenance1" type alias (v0.2 is the bare
	// "slsaprovenance"); the conformance gate asserts the v1 URI.
	PredicateTypeSLSAProvenance = "https://slsa.dev/provenance/v1"
	// PayloadType is the DSSE payloadType for in-toto statements.
	PayloadType = "application/vnd.in-toto+json"
)

// BuildType is a closed enum. Adding a member is a deliberate, reviewable edit
// to this package — that is the point: it makes "what claims our supply chain
// can express" a code-review question, not an emergent property.
type BuildType string

const (
	// BazelOCIBuild is the deployment-channel build type: a rules_oci image
	// produced by Bazel and published by deployment-service.
	BazelOCIBuild BuildType = "https://bazel.build/rules_oci/oci_image"
	// MkskReleaseBuild is the release-channel build type: a make-skill release
	// artifact bundle produced by mksk-release.
	MkskReleaseBuild BuildType = "https://verself.sh/buildtypes/mksk-release/v1"
)

var knownBuildTypes = map[BuildType]struct{}{
	BazelOCIBuild:    {},
	MkskReleaseBuild: {},
}

// Tagged errors. Verifiers branch on these and they land legibly in evidence
// rows.
var (
	ErrUnknownStatementType = errors.New("attestation: unknown statement _type")
	ErrUnknownPredicateType = errors.New("attestation: unknown predicateType")
	ErrUnknownBuildType     = errors.New("attestation: unknown buildType")
	ErrInvalidSubjectDigest = errors.New("attestation: invalid subject digest")
	ErrNoSubjects           = errors.New("attestation: statement has no subjects")
	ErrMalformedStatement   = errors.New("attestation: malformed statement")
	ErrIncompletePredicate  = errors.New("attestation: incomplete predicate")
)

// sha256Hex matches a bare lowercase sha256 hex digest (no algorithm prefix).
var sha256Hex = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Subject identifies attested bytes by content digest. Binding to bytes, not
// names or tags, is the core in-toto design move: a tag can be repointed, a
// sha256 cannot.
type Subject struct {
	Name   string
	SHA256 string // bare lowercase hex, 64 chars
}

func (s Subject) validate() error {
	if !sha256Hex.MatchString(s.SHA256) {
		return fmt.Errorf("%w: subject %q digest %q is not bare lowercase sha256 hex", ErrInvalidSubjectDigest, s.Name, s.SHA256)
	}
	return nil
}

// Predicate is a sealed sum type: the only implementations are defined in this
// package, so callers cannot smuggle an untyped predicate through.
type Predicate interface {
	isPredicate()
	predicateType() string
}

// SLSAProvenance is the SLSA provenance v1 predicate, modeled as typed fields
// rather than map[string]any. The same drift argument as the OCSF builder rule
// applies — here drift means a silently unverifiable claim.
type SLSAProvenance struct {
	BuildType            BuildType
	ExternalParameters   ExternalParameters
	ResolvedDependencies []ResolvedDependency
	BuilderID            string
	InvocationID         string
}

func (SLSAProvenance) isPredicate()           {}
func (SLSAProvenance) predicateType() string  { return PredicateTypeSLSAProvenance }

// ExternalParameters are the typed parameters for a BazelOCIBuild /
// MkskReleaseBuild. Empty fields are simply omitted from the statement.
type ExternalParameters struct {
	BazelTarget     string
	BazelPushTarget string
	OCIRepository   string
	Site            string
}

// ResolvedDependency is a source input to the build, e.g. the git commit.
type ResolvedDependency struct {
	URI       string
	GitCommit string
}

func (p SLSAProvenance) validate() error {
	if _, ok := knownBuildTypes[p.BuildType]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownBuildType, p.BuildType)
	}
	if p.BuilderID == "" {
		return fmt.Errorf("%w: builder id is required", ErrIncompletePredicate)
	}
	if p.InvocationID == "" {
		return fmt.Errorf("%w: invocation id is required", ErrIncompletePredicate)
	}
	return nil
}

// Statement is a sealed in-toto statement. Its bytes are fixed at construction;
// there is no re-marshal API by design.
type Statement struct {
	raw       []byte
	subjects  []Subject
	predicate Predicate
}

// Bytes returns a copy of the sealed statement bytes — exactly what was (or will
// be) signed and verified.
func (s *Statement) Bytes() []byte {
	out := make([]byte, len(s.raw))
	copy(out, s.raw)
	return out
}

// Subjects returns a copy of the statement's subjects.
func (s *Statement) Subjects() []Subject {
	out := make([]Subject, len(s.subjects))
	copy(out, s.subjects)
	return out
}

// Predicate returns the typed predicate.
func (s *Statement) Predicate() Predicate { return s.predicate }

// SubjectMatching reports whether the given bare sha256 hex is among the
// statement's subjects. The spec allows multiple subjects, so verifiers must ask
// "is my digest among them", never "take subject[0]".
func (s *Statement) SubjectMatching(sha256hex string) (Subject, bool) {
	for _, subj := range s.subjects {
		if subj.SHA256 == sha256hex {
			return subj, true
		}
	}
	return Subject{}, false
}

// NewProvenance builds, validates, serializes-once, and seals a SLSA provenance
// statement. This is the producer entry point.
func NewProvenance(subjects []Subject, prov SLSAProvenance) (*Statement, error) {
	if len(subjects) == 0 {
		return nil, ErrNoSubjects
	}
	for _, subj := range subjects {
		if err := subj.validate(); err != nil {
			return nil, err
		}
	}
	if err := prov.validate(); err != nil {
		return nil, err
	}

	predStruct, err := provenanceStruct(prov)
	if err != nil {
		return nil, err
	}
	rds := make([]*intoto.ResourceDescriptor, 0, len(subjects))
	for _, subj := range subjects {
		rds = append(rds, &intoto.ResourceDescriptor{
			Name:   subj.Name,
			Digest: map[string]string{"sha256": subj.SHA256},
		})
	}
	stmt := &intoto.Statement{
		Type:          StatementType,
		Subject:       rds,
		PredicateType: PredicateTypeSLSAProvenance,
		Predicate:     predStruct,
	}
	raw, err := protojson.Marshal(stmt)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal statement: %v", ErrMalformedStatement, err)
	}
	return &Statement{
		raw:       raw,
		subjects:  append([]Subject(nil), subjects...),
		predicate: prov,
	}, nil
}

// Parse strictly decodes verified statement bytes into a typed Statement and
// retains the input bytes as the sealed buffer. It is the consumer entry point,
// called only on bytes that have already passed signature verification.
func Parse(raw []byte) (*Statement, error) {
	var stmt intoto.Statement
	// DiscardUnknown=false rejects unknown top-level statement fields.
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, &stmt); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedStatement, err)
	}
	if stmt.Type != StatementType {
		return nil, fmt.Errorf("%w: %q", ErrUnknownStatementType, stmt.Type)
	}
	if stmt.PredicateType != PredicateTypeSLSAProvenance {
		return nil, fmt.Errorf("%w: %q", ErrUnknownPredicateType, stmt.PredicateType)
	}
	if len(stmt.Subject) == 0 {
		return nil, ErrNoSubjects
	}
	subjects := make([]Subject, 0, len(stmt.Subject))
	for _, rd := range stmt.Subject {
		subj := Subject{Name: rd.GetName(), SHA256: rd.GetDigest()["sha256"]}
		if err := subj.validate(); err != nil {
			return nil, err
		}
		subjects = append(subjects, subj)
	}
	prov, err := parseProvenance(stmt.Predicate)
	if err != nil {
		return nil, err
	}
	if err := prov.validate(); err != nil {
		return nil, err
	}
	return &Statement{raw: append([]byte(nil), raw...), subjects: subjects, predicate: prov}, nil
}

// --- predicate <-> structpb conversion -------------------------------------
//
// The in-toto proto carries the predicate as a structpb.Struct (arbitrary
// JSON). To keep the predicate schema CLOSED we round-trip through a strict DTO
// with DisallowUnknownFields, so an unexpected predicate field is rejected
// rather than silently dropped.

type provenanceDTO struct {
	BuildDefinition struct {
		BuildType          string `json:"buildType"`
		ExternalParameters struct {
			BazelTarget     string `json:"bazelTarget,omitempty"`
			BazelPushTarget string `json:"bazelPushTarget,omitempty"`
			OCIRepository   string `json:"ociRepository,omitempty"`
			Site            string `json:"site,omitempty"`
		} `json:"externalParameters"`
		ResolvedDependencies []struct {
			URI    string            `json:"uri"`
			Digest map[string]string `json:"digest"`
		} `json:"resolvedDependencies,omitempty"`
	} `json:"buildDefinition"`
	RunDetails struct {
		Builder struct {
			ID string `json:"id"`
		} `json:"builder"`
		Metadata struct {
			InvocationID string `json:"invocationId"`
		} `json:"metadata"`
	} `json:"runDetails"`
}

func provenanceStruct(prov SLSAProvenance) (*structpb.Struct, error) {
	var dto provenanceDTO
	dto.BuildDefinition.BuildType = string(prov.BuildType)
	dto.BuildDefinition.ExternalParameters.BazelTarget = prov.ExternalParameters.BazelTarget
	dto.BuildDefinition.ExternalParameters.BazelPushTarget = prov.ExternalParameters.BazelPushTarget
	dto.BuildDefinition.ExternalParameters.OCIRepository = prov.ExternalParameters.OCIRepository
	dto.BuildDefinition.ExternalParameters.Site = prov.ExternalParameters.Site
	for _, rd := range prov.ResolvedDependencies {
		dep := struct {
			URI    string            `json:"uri"`
			Digest map[string]string `json:"digest"`
		}{URI: rd.URI, Digest: map[string]string{"gitCommit": rd.GitCommit}}
		dto.BuildDefinition.ResolvedDependencies = append(dto.BuildDefinition.ResolvedDependencies, dep)
	}
	dto.RunDetails.Builder.ID = prov.BuilderID
	dto.RunDetails.Metadata.InvocationID = prov.InvocationID

	encoded, err := json.Marshal(dto)
	if err != nil {
		return nil, fmt.Errorf("%w: encode predicate: %v", ErrMalformedStatement, err)
	}
	var s structpb.Struct
	if err := protojson.Unmarshal(encoded, &s); err != nil {
		return nil, fmt.Errorf("%w: predicate to struct: %v", ErrMalformedStatement, err)
	}
	return &s, nil
}

func parseProvenance(predStruct *structpb.Struct) (SLSAProvenance, error) {
	if predStruct == nil {
		return SLSAProvenance{}, fmt.Errorf("%w: missing predicate", ErrMalformedStatement)
	}
	encoded, err := protojson.Marshal(predStruct)
	if err != nil {
		return SLSAProvenance{}, fmt.Errorf("%w: marshal predicate: %v", ErrMalformedStatement, err)
	}
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.DisallowUnknownFields()
	var dto provenanceDTO
	if err := dec.Decode(&dto); err != nil {
		return SLSAProvenance{}, fmt.Errorf("%w: unknown or malformed predicate field: %v", ErrMalformedStatement, err)
	}
	prov := SLSAProvenance{
		BuildType: BuildType(dto.BuildDefinition.BuildType),
		ExternalParameters: ExternalParameters{
			BazelTarget:     dto.BuildDefinition.ExternalParameters.BazelTarget,
			BazelPushTarget: dto.BuildDefinition.ExternalParameters.BazelPushTarget,
			OCIRepository:   dto.BuildDefinition.ExternalParameters.OCIRepository,
			Site:            dto.BuildDefinition.ExternalParameters.Site,
		},
		BuilderID:    dto.RunDetails.Builder.ID,
		InvocationID: dto.RunDetails.Metadata.InvocationID,
	}
	for _, dep := range dto.BuildDefinition.ResolvedDependencies {
		prov.ResolvedDependencies = append(prov.ResolvedDependencies, ResolvedDependency{
			URI:       dep.URI,
			GitCommit: dep.Digest["gitCommit"],
		})
	}
	return prov, nil
}
