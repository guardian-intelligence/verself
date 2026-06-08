// Command zot-config renders the ephemeral zot registry config.json and bcrypt
// htpasswd line into /alloc from the Guardian resource graph plus the projected
// publisher password. It is baked into the zot OCI image and invoked by the
// nomad.hcl "config" prestart task. It owns no install/supervise/proc
// responsibilities: under the podman cutover Nomad owns process lifecycle, the
// image loads from a docker-archive, durable storage is a host bind-mount, and
// identity is a baked uid/gid.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	apiVersion      = "zot.guardianintelligence.org/v1alpha1"
	kind            = "ZotRegistry"
	defaultResource = "zot"
)

type options struct {
	resourceGraph string
	resourceName  string
	passwordFile  string
	configPath    string
	htpasswdPath  string
}

type document struct {
	Resources []resource `json:"resources"`
}

type resource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   metadata        `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type metadata struct {
	Name string `json:"name"`
}

type spec struct {
	ConfigPath    string `json:"configPath"`
	HtpasswdPath  string `json:"htpasswdPath"`
	StorageDir    string `json:"storageDir"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Realm         string `json:"realm"`
	LogLevel      string `json:"logLevel"`
	PublisherUser string `json:"publisherUser"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "zot-config: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	cfg, err := loadSpec(opts)
	if err != nil {
		return err
	}
	// The CRD carries configPath/htpasswdPath as the source of truth; the flags
	// default to them but a mismatch is a wiring error, so fail loud.
	configPath := firstNonEmpty(opts.configPath, cfg.ConfigPath)
	htpasswdPath := firstNonEmpty(opts.htpasswdPath, cfg.HtpasswdPath)
	if !filepath.IsAbs(configPath) || !filepath.IsAbs(htpasswdPath) {
		return errors.New("configPath and htpasswdPath must be absolute")
	}
	password, err := readPassword(opts.passwordFile)
	if err != nil {
		return err
	}
	if err := writeHtpasswd(htpasswdPath, cfg.PublisherUser, password); err != nil {
		return err
	}
	return writeConfig(configPath, htpasswdPath, cfg)
}

func parseOptions(args []string) (options, error) {
	opts := options{resourceName: defaultResource}
	fs := flag.NewFlagSet("zot-config", flag.ContinueOnError)
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "ZotRegistry resource name.")
	fs.StringVar(&opts.passwordFile, "password-file", "", "Path to the plaintext publisher password.")
	fs.StringVar(&opts.configPath, "config-path", "", "Output config.json path; defaults to spec.configPath.")
	fs.StringVar(&opts.htpasswdPath, "htpasswd-path", "", "Output htpasswd path; defaults to spec.htpasswdPath.")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.resourceGraph = strings.TrimSpace(opts.resourceGraph)
	opts.resourceName = strings.TrimSpace(opts.resourceName)
	opts.passwordFile = strings.TrimSpace(opts.passwordFile)
	if opts.resourceGraph == "" {
		return options{}, errors.New("--resource-graph is required")
	}
	if opts.resourceName == "" {
		return options{}, errors.New("--resource-name is required")
	}
	if opts.passwordFile == "" {
		return options{}, errors.New("--password-file is required")
	}
	return opts, nil
}

func loadSpec(opts options) (spec, error) {
	body, err := os.ReadFile(opts.resourceGraph)
	if err != nil {
		return spec{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc document
	if err := json.Unmarshal(body, &doc); err != nil {
		return spec{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []resource
	for _, res := range doc.Resources {
		if res.APIVersion == apiVersion && res.Kind == kind && res.Metadata.Name == opts.resourceName {
			matches = append(matches, res)
		}
	}
	if len(matches) != 1 {
		return spec{}, fmt.Errorf("expected exactly one %s resource named %q, found %d", kind, opts.resourceName, len(matches))
	}
	var cfg spec
	if err := json.Unmarshal(matches[0].Spec, &cfg); err != nil {
		return spec{}, fmt.Errorf("decode ZotRegistry spec: %w", err)
	}
	if err := validateSpec(cfg); err != nil {
		return spec{}, err
	}
	return cfg, nil
}

func validateSpec(cfg spec) error {
	for name, value := range map[string]string{
		"storageDir":    cfg.StorageDir,
		"host":          cfg.Host,
		"realm":         cfg.Realm,
		"logLevel":      cfg.LogLevel,
		"publisherUser": cfg.PublisherUser,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("ZotRegistry.spec.%s is required", name)
		}
	}
	if !filepath.IsAbs(cfg.StorageDir) {
		return errors.New("ZotRegistry.spec.storageDir must be an absolute path")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return errors.New("ZotRegistry.spec.port must be between 1 and 65535")
	}
	if strings.ContainsAny(cfg.PublisherUser, ":\n\r") {
		return errors.New("ZotRegistry.spec.publisherUser cannot contain ':', CR, or LF")
	}
	return nil
}

func readPassword(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read publisher password: %w", err)
	}
	password := strings.TrimSpace(string(raw))
	if password == "" {
		return "", errors.New("publisher password rendered empty")
	}
	return password, nil
}

// writeHtpasswd derives the bcrypt htpasswd line from the same plaintext the
// publisher presents, so the two stay in lockstep without persisting the hash
// on durable state.
func writeHtpasswd(path, user, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash publisher password: %w", err)
	}
	return writeAtomic(path, []byte(user+":"+string(hash)+"\n"), 0o640)
}

func writeConfig(configPath, htpasswdPath string, cfg spec) error {
	body, err := json.MarshalIndent(zotConfig{
		DistSpecVersion: "1.1.1",
		Storage: zotStorageConfig{
			RootDirectory: cfg.StorageDir,
			Commit:        true,
			Dedupe:        true,
			GC:            true,
			GCDelay:       "1h",
			GCInterval:    "24h",
		},
		HTTP: zotHTTPConfig{
			Address: cfg.Host,
			Port:    cfg.Port,
			Realm:   cfg.Realm,
			Auth: zotAuthConfig{
				Htpasswd:  zotHtpasswdConfig{Path: htpasswdPath},
				FailDelay: 1,
			},
			AccessControl: zotAccessControl{
				Repositories: map[string]zotRepositoryPolicy{
					"verself/**": {
						AnonymousPolicy: []string{"read"},
						Policies: []zotUserPolicy{{
							Users:   []string{cfg.PublisherUser},
							Actions: []string{"read", "create", "update", "delete"},
						}},
					},
					"**": {
						AnonymousPolicy: []string{"read"},
					},
				},
			},
		},
		Log: zotLogConfig{Level: cfg.LogLevel},
		Extensions: zotExtensions{
			Scrub: zotScrubConfig{Enable: true, Interval: "24h"},
		},
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode zot config: %w", err)
	}
	body = append(body, '\n')
	return writeAtomic(configPath, body, 0o640)
}

type zotConfig struct {
	DistSpecVersion string           `json:"distSpecVersion"`
	Storage         zotStorageConfig `json:"storage"`
	HTTP            zotHTTPConfig    `json:"http"`
	Log             zotLogConfig     `json:"log"`
	Extensions      zotExtensions    `json:"extensions"`
}

type zotStorageConfig struct {
	RootDirectory string `json:"rootDirectory"`
	Commit        bool   `json:"commit"`
	Dedupe        bool   `json:"dedupe"`
	GC            bool   `json:"gc"`
	GCDelay       string `json:"gcDelay"`
	GCInterval    string `json:"gcInterval"`
}

type zotHTTPConfig struct {
	Address       string           `json:"address"`
	Port          int              `json:"port"`
	Realm         string           `json:"realm"`
	Auth          zotAuthConfig    `json:"auth"`
	AccessControl zotAccessControl `json:"accessControl"`
}

type zotAuthConfig struct {
	Htpasswd  zotHtpasswdConfig `json:"htpasswd"`
	FailDelay int               `json:"failDelay"`
}

type zotHtpasswdConfig struct {
	Path string `json:"path"`
}

type zotAccessControl struct {
	Repositories map[string]zotRepositoryPolicy `json:"repositories"`
}

type zotRepositoryPolicy struct {
	AnonymousPolicy []string        `json:"anonymousPolicy"`
	Policies        []zotUserPolicy `json:"policies,omitempty"`
}

type zotUserPolicy struct {
	Users   []string `json:"users"`
	Actions []string `json:"actions"`
}

type zotLogConfig struct {
	Level string `json:"level"`
}

type zotExtensions struct {
	Scrub zotScrubConfig `json:"scrub"`
}

type zotScrubConfig struct {
	Enable   bool   `json:"enable"`
	Interval string `json:"interval"`
}

func writeAtomic(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("promote %s: %w", path, err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
