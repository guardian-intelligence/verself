package specdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Document struct {
	Kind         string       `json:"kind" yaml:"kind" toml:"kind" toon:"kind"`
	Name         string       `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	StaticConfig StaticConfig `json:"staticConfig" yaml:"staticConfig" toml:"staticConfig" toon:"staticConfig"`
	Board        Board        `json:"board" yaml:"board" toml:"board" toon:"board"`
	Nomad        Nomad        `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
}

type StaticConfig struct {
	BaseURL        string `json:"baseURL" yaml:"baseURL" toml:"baseURL" toon:"baseURL"`
	CredentialsRef string `json:"credentialsRef" yaml:"credentialsRef" toml:"credentialsRef" toon:"credentialsRef"`
}

type Board struct {
	Access LifecycleHook `json:"access" yaml:"access" toml:"access" toon:"access"`
	Upload Upload        `json:"upload" yaml:"upload" toml:"upload" toon:"upload"`
}

type Upload struct {
	Run    LifecycleHook `json:"run" yaml:"run" toml:"run" toon:"run"`
	Verify LifecycleHook `json:"verify" yaml:"verify" toml:"verify" toon:"verify"`
}

type LifecycleHook struct {
	Argv []string `json:"argv" yaml:"argv" toml:"argv" toon:"argv"`
}

type Nomad struct {
	Address   string     `json:"address" yaml:"address" toml:"address" toon:"address"`
	Namespace string     `json:"namespace" yaml:"namespace" toml:"namespace" toon:"namespace"`
	Jobs      []NomadJob `json:"jobs" yaml:"jobs" toml:"jobs" toon:"jobs"`
}

type NomadJob struct {
	Path        string   `json:"path" yaml:"path" toml:"path" toon:"path"`
	RequiredFor []string `json:"requiredFor,omitempty" yaml:"requiredFor,omitempty" toml:"requiredFor,omitempty" toon:"requiredFor,omitempty"`
}

func Validate(doc Document) error {
	if doc.Kind != "FlyProcedure" {
		return fmt.Errorf("kind must be FlyProcedure, got %q", doc.Kind)
	}
	if strings.TrimSpace(doc.StaticConfig.BaseURL) == "" {
		return errors.New("staticConfig.baseURL is required")
	}
	parsedBaseURL, err := url.Parse(doc.StaticConfig.BaseURL)
	if err != nil {
		return fmt.Errorf("staticConfig.baseURL is invalid: %w", err)
	}
	if parsedBaseURL.Scheme != "https" || parsedBaseURL.Host == "" {
		return errors.New("staticConfig.baseURL must be an https URL with a host")
	}
	if strings.TrimSpace(doc.StaticConfig.CredentialsRef) == "" {
		return errors.New("staticConfig.credentialsRef is required")
	}
	if err := validateHook("board.access", doc.Board.Access); err != nil {
		return err
	}
	if err := validateHook("board.upload.run", doc.Board.Upload.Run); err != nil {
		return err
	}
	if err := validateHook("board.upload.verify", doc.Board.Upload.Verify); err != nil {
		return err
	}
	if strings.TrimSpace(doc.Nomad.Address) == "" {
		return errors.New("nomad.address is required")
	}
	if strings.TrimSpace(doc.Nomad.Namespace) == "" {
		return errors.New("nomad.namespace is required")
	}
	if len(doc.Nomad.Jobs) == 0 {
		return errors.New("nomad.jobs is required")
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
