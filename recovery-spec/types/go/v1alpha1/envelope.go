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
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	apiVersionRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/v[0-9]+((alpha|beta)[0-9]+)?$`)
	kindRE       = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	nameRE       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type ResourceEnvelope struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Metadata   Metadata  `yaml:"metadata" json:"metadata"`
	Spec       yaml.Node `yaml:"spec" json:"-"`
}

type ResourceIdentity struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Name       string `yaml:"name" json:"name"`
}

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
	return hashCanonical(struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Spec       any    `json:"spec"`
	}{
		APIVersion: r.Identity().APIVersion,
		Kind:       r.Identity().Kind,
		Name:       r.Identity().Name,
		Spec:       value,
	})
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
