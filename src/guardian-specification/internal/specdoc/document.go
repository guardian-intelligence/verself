package specdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

const (
	APIGuardian   = "guardian.guardianintelligence.org/v1alpha1"
	APISubstrate  = "substrate.guardianintelligence.org/v1alpha1"
	APINetworking = "networking.guardianintelligence.org/v1alpha1"

	KindFlyProcedure = "FlyProcedure"
	KindSubstrate    = "Substrate"
	KindPublicOrigin = "PublicOrigin"
)

type Document struct {
	Entrypoint ObjectRef  `json:"entrypoint" yaml:"entrypoint" toml:"entrypoint" toon:"entrypoint"`
	Resources  []Resource `json:"resources" yaml:"resources" toml:"resources" toon:"resources"`
}

type ObjectRef struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion" toml:"apiVersion" toon:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind" toml:"kind" toon:"kind"`
	Name       string `json:"name" yaml:"name" toml:"name" toon:"name"`
}

type ObjectMeta struct {
	Name string `json:"name" yaml:"name" toml:"name" toon:"name"`
}

type Resource struct {
	APIVersion string       `json:"apiVersion" yaml:"apiVersion" toml:"apiVersion" toon:"apiVersion"`
	Kind       string       `json:"kind" yaml:"kind" toml:"kind" toon:"kind"`
	Metadata   ObjectMeta   `json:"metadata" yaml:"metadata" toml:"metadata" toon:"metadata"`
	Spec       ResourceSpec `json:"spec" yaml:"spec" toml:"spec" toon:"spec"`
}

type ResourceSpec map[string]any

type FlyProcedureSpec struct {
	SubstrateRef ObjectRef `json:"substrateRef,omitempty" yaml:"substrateRef,omitempty" toml:"substrateRef,omitempty" toon:"substrateRef,omitempty"`
	Preflight    Preflight `json:"preflight,omitempty" yaml:"preflight,omitempty" toml:"preflight,omitempty" toon:"preflight,omitempty"`
	Nomad        NomadFly  `json:"nomad,omitempty" yaml:"nomad,omitempty" toml:"nomad,omitempty" toon:"nomad,omitempty"`
}

type SubstrateSpec struct {
	Remote Remote `json:"remote,omitempty" yaml:"remote,omitempty" toml:"remote,omitempty" toon:"remote,omitempty"`
}

type PublicOriginSpec struct {
	URL string `json:"url,omitempty" yaml:"url,omitempty" toml:"url,omitempty" toon:"url,omitempty"`
}

type Preflight struct {
	Ansible AnsiblePreflight `json:"ansible,omitempty" yaml:"ansible,omitempty" toml:"ansible,omitempty" toon:"ansible,omitempty"`
}

type AnsiblePreflight struct {
	Playbook string `json:"playbook,omitempty" yaml:"playbook,omitempty" toml:"playbook,omitempty" toon:"playbook,omitempty"`
}

type NomadFly struct {
	Address   string     `json:"address,omitempty" yaml:"address,omitempty" toml:"address,omitempty" toon:"address,omitempty"`
	Namespace string     `json:"namespace,omitempty" yaml:"namespace,omitempty" toml:"namespace,omitempty" toon:"namespace,omitempty"`
	Jobs      []NomadJob `json:"jobs,omitempty" yaml:"jobs,omitempty" toml:"jobs,omitempty" toon:"jobs,omitempty"`
}

type NomadJob struct {
	Name          string   `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	Path          string   `json:"path,omitempty" yaml:"path,omitempty" toml:"path,omitempty" toon:"path,omitempty"`
	ArtifactPaths []string `json:"artifactPaths,omitempty" yaml:"artifactPaths,omitempty" toml:"artifactPaths,omitempty" toon:"artifactPaths,omitempty"`
}

type Remote struct {
	RepoRoot string   `json:"repoRoot,omitempty" yaml:"repoRoot,omitempty" toml:"repoRoot,omitempty" toon:"repoRoot,omitempty"`
	SSH      []string `json:"ssh,omitempty" yaml:"ssh,omitempty" toml:"ssh,omitempty" toon:"ssh,omitempty"`
}

type CompiledDocument struct {
	Entrypoint    ObjectRef
	Fly           Resource
	FlySpec       FlyProcedureSpec
	Substrate     Resource
	SubstrateSpec SubstrateSpec
	Resources     map[ResourceKey]Resource
}

type ResourceKey struct {
	APIVersion string
	Kind       string
	Name       string
}

func Validate(doc Document) error {
	_, err := Compile(doc)
	return err
}

func Compile(doc Document) (CompiledDocument, error) {
	if len(doc.Resources) == 0 {
		return CompiledDocument{}, errors.New("resources is required")
	}
	if err := validateRef("entrypoint", doc.Entrypoint); err != nil {
		return CompiledDocument{}, err
	}
	if doc.Entrypoint.APIVersion != APIGuardian || doc.Entrypoint.Kind != KindFlyProcedure {
		return CompiledDocument{}, fmt.Errorf("entrypoint must reference %s/%s", APIGuardian, KindFlyProcedure)
	}
	resources := map[ResourceKey]Resource{}
	for i, resource := range doc.Resources {
		if err := validateResourceEnvelope(i, resource); err != nil {
			return CompiledDocument{}, err
		}
		key := resource.Key()
		if _, exists := resources[key]; exists {
			return CompiledDocument{}, fmt.Errorf("resources[%d] duplicates %s", i, key.String())
		}
		if err := validateResourceSpec(resource); err != nil {
			return CompiledDocument{}, fmt.Errorf("%s: %w", key.String(), err)
		}
		resources[key] = resource
	}
	fly, err := resolve(resources, doc.Entrypoint)
	if err != nil {
		return CompiledDocument{}, err
	}
	flySpec, err := DecodeResourceSpec[FlyProcedureSpec](fly.Spec)
	if err != nil {
		return CompiledDocument{}, fmt.Errorf("%s spec: %w", fly.Key().String(), err)
	}
	substrate, err := resolve(resources, flySpec.SubstrateRef)
	if err != nil {
		return CompiledDocument{}, fmt.Errorf("%s spec.substrateRef: %w", fly.Key().String(), err)
	}
	substrateSpec, err := DecodeResourceSpec[SubstrateSpec](substrate.Spec)
	if err != nil {
		return CompiledDocument{}, fmt.Errorf("%s spec: %w", substrate.Key().String(), err)
	}
	return CompiledDocument{
		Entrypoint:    doc.Entrypoint,
		Fly:           fly,
		FlySpec:       flySpec,
		Substrate:     substrate,
		SubstrateSpec: substrateSpec,
		Resources:     resources,
	}, nil
}

func (r Resource) Key() ResourceKey {
	return ResourceKey{APIVersion: r.APIVersion, Kind: r.Kind, Name: r.Metadata.Name}
}

func (k ResourceKey) String() string {
	return k.APIVersion + "/" + k.Kind + "/" + k.Name
}

func (r ObjectRef) Key() ResourceKey {
	return ResourceKey(r)
}

func CanonicalJSON(doc Document) ([]byte, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("canonical JSON: %w", err)
	}
	return data, nil
}

func validateResourceEnvelope(index int, resource Resource) error {
	if strings.TrimSpace(resource.APIVersion) == "" {
		return fmt.Errorf("resources[%d].apiVersion is required", index)
	}
	if strings.TrimSpace(resource.Kind) == "" {
		return fmt.Errorf("resources[%d].kind is required", index)
	}
	if strings.TrimSpace(resource.Metadata.Name) == "" {
		return fmt.Errorf("resources[%d].metadata.name is required", index)
	}
	if strings.Contains(resource.Metadata.Name, "/") {
		return fmt.Errorf("resources[%d].metadata.name must not contain '/'", index)
	}
	return nil
}

func validateResourceSpec(resource Resource) error {
	if err := rejectLegacyTokens(resource); err != nil {
		return err
	}
	switch resource.APIVersion + "/" + resource.Kind {
	case APIGuardian + "/" + KindFlyProcedure:
		spec, err := DecodeResourceSpec[FlyProcedureSpec](resource.Spec)
		if err != nil {
			return err
		}
		if err := requireKindRef("spec.substrateRef", spec.SubstrateRef, APISubstrate, KindSubstrate); err != nil {
			return err
		}
		if err := validatePreflight("spec.preflight", spec.Preflight); err != nil {
			return err
		}
		return validateNomadFly("spec.nomad", spec.Nomad)
	case APISubstrate + "/" + KindSubstrate:
		spec, err := DecodeResourceSpec[SubstrateSpec](resource.Spec)
		if err != nil {
			return err
		}
		return validateRemote("spec.remote", spec.Remote)
	case APINetworking + "/" + KindPublicOrigin:
		spec, err := DecodeResourceSpec[PublicOriginSpec](resource.Spec)
		if err != nil {
			return err
		}
		return validatePublicOriginURL(spec.URL)
	default:
		return nil
	}
}

func validatePreflight(name string, preflight Preflight) error {
	return validateWorkspacePath(name+".ansible.playbook", preflight.Ansible.Playbook)
}

func validateWorkspacePath(name string, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s is required", name)
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("%s must be workspace-relative", name)
	}
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("%s contains a NUL byte", name)
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must not escape the workspace", name)
	}
	return nil
}

func ResourceSpecFrom(value any) (ResourceSpec, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode resource spec: %w", err)
	}
	var spec ResourceSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("decode resource spec: %w", err)
	}
	return spec, nil
}

func MustResourceSpec(value any) ResourceSpec {
	spec, err := ResourceSpecFrom(value)
	if err != nil {
		panic(err)
	}
	return spec
}

func DecodeResourceSpec[T any](spec ResourceSpec) (T, error) {
	var out T
	data, err := json.Marshal(spec)
	if err != nil {
		return out, fmt.Errorf("encode resource spec: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return out, errors.New("multiple resource specs are not supported")
		}
		return out, err
	}
	return out, nil
}

func DecodeOpenResourceSpec[T any](spec ResourceSpec) (T, error) {
	var out T
	data, err := json.Marshal(spec)
	if err != nil {
		return out, fmt.Errorf("encode resource spec: %w", err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

func validateRef(name string, ref ObjectRef) error {
	if strings.TrimSpace(ref.APIVersion) == "" {
		return fmt.Errorf("%s.apiVersion is required", name)
	}
	if strings.TrimSpace(ref.Kind) == "" {
		return fmt.Errorf("%s.kind is required", name)
	}
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("%s.name is required", name)
	}
	return nil
}

func requireKindRef(name string, ref ObjectRef, apiVersion string, kind string) error {
	if err := validateRef(name, ref); err != nil {
		return err
	}
	if ref.APIVersion != apiVersion || ref.Kind != kind {
		return fmt.Errorf("%s must reference %s/%s", name, apiVersion, kind)
	}
	return nil
}

func validateNomadFly(name string, nomad NomadFly) error {
	if err := validateHTTPURL(name+".address", nomad.Address); err != nil {
		return err
	}
	if strings.TrimSpace(nomad.Namespace) == "" {
		return fmt.Errorf("%s.namespace is required", name)
	}
	if len(nomad.Jobs) == 0 {
		return fmt.Errorf("%s.jobs is required", name)
	}
	seen := map[string]struct{}{}
	for i, job := range nomad.Jobs {
		if strings.TrimSpace(job.Name) == "" {
			return fmt.Errorf("%s.jobs[%d].name is required", name, i)
		}
		if _, exists := seen[job.Name]; exists {
			return fmt.Errorf("%s.jobs[%d].name duplicates %q", name, i, job.Name)
		}
		seen[job.Name] = struct{}{}
		if err := validateWorkspacePath(fmt.Sprintf("%s.jobs[%d].path", name, i), job.Path); err != nil {
			return err
		}
		for j, path := range job.ArtifactPaths {
			if err := validateWorkspacePath(fmt.Sprintf("%s.jobs[%d].artifactPaths[%d]", name, i, j), path); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateHTTPURL(name string, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must be http or https", name)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s host is required", name)
	}
	return nil
}

func validateRemote(name string, remote Remote) error {
	if remote.RepoRoot == "" && len(remote.SSH) == 0 {
		return nil
	}
	if !strings.HasPrefix(remote.RepoRoot, "/") {
		return fmt.Errorf("%s.repoRoot must be an absolute path", name)
	}
	if len(remote.SSH) == 0 {
		return fmt.Errorf("%s.ssh is required", name)
	}
	for i, arg := range remote.SSH {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("%s.ssh[%d] must not be empty", name, i)
		}
	}
	return nil
}

func validatePublicOriginURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("spec.url is invalid: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return errors.New("spec.url must be an https origin without path, query, or fragment")
	}
	if _, port, err := net.SplitHostPort(parsed.Host); err == nil && port == "" {
		return errors.New("spec.url host port is empty")
	}
	host := parsed.Hostname()
	if host == "" || net.ParseIP(host) != nil {
		return errors.New("spec.url host must be a DNS name")
	}
	return validateDNSName("spec.url host", host)
}

func validateDNSName(field string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.ContainsAny(value, "/:@") {
		return fmt.Errorf("%s=%q must be a DNS name", field, value)
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return fmt.Errorf("%s=%q must contain at least two DNS labels", field, value)
	}
	for _, label := range labels {
		if label == "" {
			return fmt.Errorf("%s=%q has an empty DNS label", field, value)
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("%s=%q has a DNS label starting or ending with '-'", field, value)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("%s=%q contains unsupported DNS character %q", field, value, r)
			}
		}
	}
	return nil
}

func resolve(resources map[ResourceKey]Resource, ref ObjectRef) (Resource, error) {
	resource, ok := resources[ref.Key()]
	if !ok {
		return Resource{}, fmt.Errorf("missing resource %s", RefString(ref))
	}
	return resource, nil
}

func RefString(ref ObjectRef) string {
	return ref.APIVersion + "/" + ref.Kind + "/" + ref.Name
}

func rejectLegacyTokens(resource Resource) error {
	data, err := json.Marshal(resource.Spec)
	if err != nil {
		return fmt.Errorf("inspect legacy tokens: %w", err)
	}
	if strings.Contains(string(data), "__"+"VERSELF_") {
		return errors.New("legacy string-token placeholders are not allowed")
	}
	return nil
}
