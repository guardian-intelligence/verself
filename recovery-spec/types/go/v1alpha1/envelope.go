package v1alpha1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	GuardianAPIVersion = "guardian.verself.sh/v1alpha1"
	KindCompiledSpec   = "CompiledSpecification"

	ConditionStatusTrue    = "True"
	ConditionStatusFalse   = "False"
	ConditionStatusUnknown = "Unknown"
)

var (
	apiVersionRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/v[0-9]+((alpha|beta)[0-9]+)?$`)
	kindRE       = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	nameRE       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	specHashRE   = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	conditionRE  = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	reasonRE     = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
)

type Metadata struct {
	Name       string `yaml:"name" json:"name"`
	Generation int64  `yaml:"generation,omitempty" json:"generation,omitempty"`
}

type ResourceEnvelope struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Metadata   Metadata  `yaml:"metadata" json:"metadata"`
	Spec       yaml.Node `yaml:"spec" json:"-"`
}

type ReportEnvelope struct {
	APIVersion string       `yaml:"apiVersion" json:"apiVersion"`
	Kind       string       `yaml:"kind" json:"kind"`
	Metadata   Metadata     `yaml:"metadata" json:"metadata"`
	Status     ReportStatus `yaml:"status" json:"status"`
}

type ReportStatus struct {
	ObservedGeneration int64       `yaml:"observedGeneration" json:"observedGeneration"`
	SpecHash           string      `yaml:"specHash" json:"specHash"`
	Conditions         []Condition `yaml:"conditions" json:"conditions"`
}

type Condition struct {
	Type               string `yaml:"type" json:"type"`
	Status             string `yaml:"status" json:"status"`
	Reason             string `yaml:"reason" json:"reason"`
	Message            string `yaml:"message,omitempty" json:"message,omitempty"`
	ObservedGeneration int64  `yaml:"observedGeneration" json:"observedGeneration"`
	LastTransitionTime string `yaml:"lastTransitionTime" json:"lastTransitionTime"`
}

type ResourceIdentity struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Name       string `yaml:"name" json:"name"`
}

type CompiledSpecification struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	SpecHash   string             `yaml:"specHash" json:"specHash"`
	Resources  []CompiledResource `yaml:"resources" json:"resources"`
}

type CompiledResource struct {
	Identity   ResourceIdentity `yaml:"identity" json:"identity"`
	Generation int64            `yaml:"generation,omitempty" json:"generation,omitempty"`
	SpecHash   string           `yaml:"specHash" json:"specHash"`
	Spec       any              `yaml:"spec" json:"spec"`
}

type ProviderRef string
type AuthorityRef string
type DomainRef string
type SecretRef string

func LoadResources(path string) ([]ResourceEnvelope, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read GuardianSpecification %s: %w", path, err)
	}
	resources, err := DecodeResources(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return resources, nil
}

func LoadReports(path string) ([]ReportEnvelope, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read GuardianSpecification reports %s: %w", path, err)
	}
	reports, err := DecodeReports(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return reports, nil
}

func DecodeResources(body []byte) ([]ResourceEnvelope, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	var resources []ResourceEnvelope
	for {
		var raw ResourceEnvelope
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode resource envelope: %w", err)
		}
		if raw.empty() {
			continue
		}
		if err := raw.Validate(); err != nil {
			return nil, err
		}
		resources = append(resources, raw)
	}
	if err := rejectDuplicateIdentities(resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func DecodeReports(body []byte) ([]ReportEnvelope, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	var reports []ReportEnvelope
	for {
		var raw ReportEnvelope
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode report envelope: %w", err)
		}
		if raw.empty() {
			continue
		}
		if err := raw.Validate(); err != nil {
			return nil, err
		}
		reports = append(reports, raw)
	}
	if err := rejectDuplicateReportIdentities(reports); err != nil {
		return nil, err
	}
	return reports, nil
}

func (r ResourceEnvelope) Validate() error {
	if err := ValidateAPIVersion(r.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind(r.Kind); err != nil {
		return err
	}
	if err := r.Metadata.Validate(); err != nil {
		return err
	}
	if r.Spec.Kind == 0 {
		return fmt.Errorf("%s spec is required", r.Identity())
	}
	return nil
}

func (r ResourceEnvelope) Identity() ResourceIdentity {
	return ResourceIdentity{
		APIVersion: strings.TrimSpace(r.APIVersion),
		Kind:       strings.TrimSpace(r.Kind),
		Name:       strings.TrimSpace(r.Metadata.Name),
	}
}

func (r ResourceEnvelope) SpecValue() (any, error) {
	return yamlNodeValue(r.Spec)
}

func (r ResourceEnvelope) SpecHash() (string, error) {
	value, err := r.SpecValue()
	if err != nil {
		return "", err
	}
	payload := struct {
		Identity ResourceIdentity `json:"identity"`
		Spec     any              `json:"spec"`
	}{
		Identity: r.Identity(),
		Spec:     value,
	}
	return hashCanonical(payload)
}

func (r ResourceEnvelope) empty() bool {
	return strings.TrimSpace(r.APIVersion) == "" &&
		strings.TrimSpace(r.Kind) == "" &&
		strings.TrimSpace(r.Metadata.Name) == "" &&
		r.Spec.Kind == 0
}

func (m Metadata) Validate() error {
	if !nameRE.MatchString(strings.TrimSpace(m.Name)) {
		return fmt.Errorf("metadata.name must be a DNS label")
	}
	if m.Generation < 0 {
		return fmt.Errorf("metadata.generation must be non-negative")
	}
	return nil
}

func CompileResources(resources []ResourceEnvelope) (CompiledSpecification, error) {
	if err := rejectDuplicateIdentities(resources); err != nil {
		return CompiledSpecification{}, err
	}
	compiled := make([]CompiledResource, 0, len(resources))
	for _, resource := range resources {
		if err := resource.Validate(); err != nil {
			return CompiledSpecification{}, err
		}
		spec, err := resource.SpecValue()
		if err != nil {
			return CompiledSpecification{}, err
		}
		specHash, err := resource.SpecHash()
		if err != nil {
			return CompiledSpecification{}, err
		}
		compiled = append(compiled, CompiledResource{
			Identity:   resource.Identity(),
			Generation: resource.Metadata.Generation,
			SpecHash:   specHash,
			Spec:       spec,
		})
	}
	sort.Slice(compiled, func(i, j int) bool {
		return compiled[i].Identity.key() < compiled[j].Identity.key()
	})
	wholeHash, err := hashCanonical(compiled)
	if err != nil {
		return CompiledSpecification{}, err
	}
	return CompiledSpecification{
		APIVersion: GuardianAPIVersion,
		Kind:       KindCompiledSpec,
		SpecHash:   wholeHash,
		Resources:  compiled,
	}, nil
}

func (r ReportEnvelope) Validate() error {
	if err := ValidateAPIVersion(r.APIVersion); err != nil {
		return err
	}
	if err := ValidateKind(r.Kind); err != nil {
		return err
	}
	if err := r.Metadata.Validate(); err != nil {
		return err
	}
	if err := r.Status.Validate(); err != nil {
		return err
	}
	return nil
}

func (r ReportEnvelope) Identity() ResourceIdentity {
	return ResourceIdentity{
		APIVersion: strings.TrimSpace(r.APIVersion),
		Kind:       strings.TrimSpace(r.Kind),
		Name:       strings.TrimSpace(r.Metadata.Name),
	}
}

func (r ReportEnvelope) empty() bool {
	return strings.TrimSpace(r.APIVersion) == "" &&
		strings.TrimSpace(r.Kind) == "" &&
		strings.TrimSpace(r.Metadata.Name) == "" &&
		strings.TrimSpace(r.Status.SpecHash) == "" &&
		len(r.Status.Conditions) == 0
}

func (s ReportStatus) Validate() error {
	if s.ObservedGeneration < 0 {
		return fmt.Errorf("status.observedGeneration must be non-negative")
	}
	if !specHashRE.MatchString(strings.TrimSpace(s.SpecHash)) {
		return fmt.Errorf("status.specHash must be sha256:<hex>")
	}
	if len(s.Conditions) == 0 {
		return fmt.Errorf("status.conditions requires at least one condition")
	}
	seen := map[string]struct{}{}
	for i, condition := range s.Conditions {
		if err := condition.Validate(); err != nil {
			return fmt.Errorf("status.conditions[%d]: %w", i, err)
		}
		if _, exists := seen[condition.Type]; exists {
			return fmt.Errorf("status.conditions[%d] duplicates type %q", i, condition.Type)
		}
		seen[condition.Type] = struct{}{}
	}
	return nil
}

func (c Condition) Validate() error {
	if !conditionRE.MatchString(strings.TrimSpace(c.Type)) {
		return fmt.Errorf("type must be PascalCase")
	}
	switch strings.TrimSpace(c.Status) {
	case ConditionStatusTrue, ConditionStatusFalse, ConditionStatusUnknown:
	default:
		return fmt.Errorf("status must be True, False, or Unknown")
	}
	if !reasonRE.MatchString(strings.TrimSpace(c.Reason)) {
		return fmt.Errorf("reason must be PascalCase")
	}
	if c.ObservedGeneration < 0 {
		return fmt.Errorf("observedGeneration must be non-negative")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(c.LastTransitionTime)); err != nil {
		return fmt.Errorf("lastTransitionTime must be RFC3339: %w", err)
	}
	return nil
}

func ValidateAPIVersion(value string) error {
	if !apiVersionRE.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("apiVersion must be <dns-group>/<version>")
	}
	return nil
}

func ValidateKind(value string) error {
	if !kindRE.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("kind must be PascalCase")
	}
	return nil
}

func ValidateLocalName(value, field string) error {
	if !nameRE.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("%s must be a local DNS-label reference", field)
	}
	return nil
}

func ValidateProviderRef(value ProviderRef) error {
	return ValidateLocalName(string(value), "providerRef")
}

func ValidateAuthorityRef(value AuthorityRef) error {
	return ValidateLocalName(string(value), "authorityRef")
}

func ValidateDomainRef(value DomainRef) error {
	return ValidateLocalName(string(value), "domainRef")
}

func ValidateSecretRef(value SecretRef) error {
	return ValidateLocalName(string(value), "secretRef")
}

func rejectDuplicateIdentities(resources []ResourceEnvelope) error {
	seen := map[string]ResourceIdentity{}
	for _, resource := range resources {
		identity := resource.Identity()
		key := identity.key()
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("%s duplicates %s", identity, previous)
		}
		seen[key] = identity
	}
	return nil
}

func rejectDuplicateReportIdentities(reports []ReportEnvelope) error {
	seen := map[string]ResourceIdentity{}
	for _, report := range reports {
		identity := report.Identity()
		key := identity.key()
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("%s duplicates %s", identity, previous)
		}
		seen[key] = identity
	}
	return nil
}

func (i ResourceIdentity) key() string {
	return strings.Join([]string{i.APIVersion, i.Kind, i.Name}, "\x00")
}

func (i ResourceIdentity) String() string {
	return fmt.Sprintf("%s/%s %s", i.APIVersion, i.Kind, i.Name)
}

func hashCanonical(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode canonical JSON: %w", err)
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func yamlNodeValue(node yaml.Node) (any, error) {
	if node.Kind == 0 {
		return nil, nil
	}
	var value any
	if err := node.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode YAML node: %w", err)
	}
	return normalizeYAMLValue(value), nil
}

func normalizeYAMLValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = normalizeYAMLValue(value)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[fmt.Sprint(key)] = normalizeYAMLValue(value)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, normalizeYAMLValue(value))
		}
		return out
	default:
		return typed
	}
}
