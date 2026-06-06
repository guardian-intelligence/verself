package specdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
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
	Nomad        NomadRun  `json:"nomad,omitempty" yaml:"nomad,omitempty" toml:"nomad,omitempty" toon:"nomad,omitempty"`
}

type SubstrateSpec struct {
	Access LifecycleHook `json:"access,omitempty" yaml:"access,omitempty" toml:"access,omitempty" toon:"access,omitempty"`
	Upload Upload        `json:"upload,omitempty" yaml:"upload,omitempty" toml:"upload,omitempty" toon:"upload,omitempty"`
	Kernel Kernel        `json:"kernel,omitempty" yaml:"kernel,omitempty" toml:"kernel,omitempty" toon:"kernel,omitempty"`
}

type PublicOriginSpec struct {
	URL string `json:"url,omitempty" yaml:"url,omitempty" toml:"url,omitempty" toon:"url,omitempty"`
}

type LifecycleHook struct {
	Argv []string `json:"argv,omitempty" yaml:"argv,omitempty" toml:"argv,omitempty" toon:"argv,omitempty"`
}

type NomadRun struct {
	Run LifecycleHook `json:"run,omitempty" yaml:"run,omitempty" toml:"run,omitempty" toon:"run,omitempty"`
}

type Kernel struct {
	OpenBaoPrepare LifecycleHook `json:"openbaoPrepare,omitempty" yaml:"openbaoPrepare,omitempty" toml:"openbaoPrepare,omitempty" toon:"openbaoPrepare,omitempty"`
	Nomad          LifecycleHook `json:"nomad,omitempty" yaml:"nomad,omitempty" toml:"nomad,omitempty" toon:"nomad,omitempty"`
	Verify         LifecycleHook `json:"verify,omitempty" yaml:"verify,omitempty" toml:"verify,omitempty" toon:"verify,omitempty"`
}

type Upload struct {
	Run     LifecycleHook `json:"run,omitempty" yaml:"run,omitempty" toml:"run,omitempty" toon:"run,omitempty"`
	Extract LifecycleHook `json:"extract,omitempty" yaml:"extract,omitempty" toml:"extract,omitempty" toon:"extract,omitempty"`
	Verify  LifecycleHook `json:"verify,omitempty" yaml:"verify,omitempty" toml:"verify,omitempty" toon:"verify,omitempty"`
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
		return validateHook("spec.nomad.run", spec.Nomad.Run)
	case APISubstrate + "/" + KindSubstrate:
		spec, err := DecodeResourceSpec[SubstrateSpec](resource.Spec)
		if err != nil {
			return err
		}
		if err := validateHook("spec.access", spec.Access); err != nil {
			return err
		}
		if err := validateHook("spec.upload.run", spec.Upload.Run); err != nil {
			return err
		}
		if err := validateHook("spec.upload.extract", spec.Upload.Extract); err != nil {
			return err
		}
		if err := validateHook("spec.upload.verify", spec.Upload.Verify); err != nil {
			return err
		}
		if err := validateHook("spec.kernel.openbaoPrepare", spec.Kernel.OpenBaoPrepare); err != nil {
			return err
		}
		if err := validateHook("spec.kernel.nomad", spec.Kernel.Nomad); err != nil {
			return err
		}
		return validateHook("spec.kernel.verify", spec.Kernel.Verify)
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

func validateHook(name string, hook LifecycleHook) error {
	if len(hook.Argv) == 0 {
		return fmt.Errorf("%s.argv is required", name)
	}
	for i, arg := range hook.Argv {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("%s.argv[%d] must not be empty", name, i)
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
