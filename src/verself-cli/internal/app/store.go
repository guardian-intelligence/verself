package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct {
	paths XDGPaths
}

func newStore(getenv EnvLookup) (*Store, error) {
	paths, err := resolveXDG(getenv)
	if err != nil {
		return nil, err
	}
	return &Store{paths: paths}, nil
}

func (s *Store) LoadConfig() (Config, error) {
	var cfg Config
	if err := readJSONFile(s.paths.configPath(), &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{Version: 1}, nil
		}
		return Config{}, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return cfg, nil
}

func (s *Store) SaveConfig(cfg Config) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return writeJSONFile(s.paths.configPath(), cfg, 0o600)
}

func (s *Store) SaveProfile(p ProfileRecord) error {
	if p.Version == 0 {
		p.Version = 1
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	return writeJSONFile(s.paths.profilePath(p.Name), p, 0o600)
}

func (s *Store) LoadProfile(name string) (ProfileRecord, error) {
	if name == "" {
		cfg, err := s.LoadConfig()
		if err != nil {
			return ProfileRecord{}, err
		}
		name = cfg.ActiveProfile
	}
	if name == "" {
		return ProfileRecord{}, errors.New("profile is required; run `verself auth login`")
	}
	var p ProfileRecord
	if err := readJSONFile(s.paths.profilePath(name), &p); err != nil {
		return ProfileRecord{}, err
	}
	if p.Version == 0 {
		p.Version = 1
	}
	return p, nil
}

func (s *Store) ListProfiles() ([]string, error) {
	dir := filepath.Join(s.paths.DataDir, "profiles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(entry.Name(), ".json"))
	}
	return out, nil
}

func (s *Store) DeleteProfile(name string) error {
	if name == "" {
		cfg, err := s.LoadConfig()
		if err != nil {
			return err
		}
		name = cfg.ActiveProfile
	}
	if name == "" {
		return nil
	}
	profile, err := s.LoadProfile(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && profile.TokenRef != "" {
		if deleteErr := s.DeleteCredential(profile.TokenRef); deleteErr != nil {
			return deleteErr
		}
	}
	if err := os.Remove(s.paths.profilePath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.ActiveProfile == name {
		cfg.ActiveProfile = ""
		return s.SaveConfig(cfg)
	}
	return nil
}

func (s *Store) SaveCompany(c CompanyRecord) error {
	if c.Version == 0 {
		c.Version = currentCompanyVersion
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	return writeJSONFile(s.paths.companyPath(c.Name), c, 0o600)
}

func (s *Store) LoadCompany(name string) (CompanyRecord, error) {
	if name == "" {
		cfg, err := s.LoadConfig()
		if err != nil {
			return CompanyRecord{}, err
		}
		name = cfg.ActiveCompany
	}
	if name == "" {
		return CompanyRecord{}, errors.New("company is required; pass a name or run `verself company use <name>`")
	}
	var c CompanyRecord
	if err := readJSONFile(s.paths.companyPath(name), &c); err != nil {
		return CompanyRecord{}, err
	}
	return c, nil
}

func (s *Store) ListCompanies() ([]string, error) {
	dir := filepath.Join(s.paths.DataDir, "companies")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(entry.Name(), ".json"))
	}
	return out, nil
}

func (s *Store) SaveCredential(value string) (string, error) {
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.paths.credentialDir(), id+".secret")
	if err := atomicWriteFile(path, []byte(value), 0o600); err != nil {
		return "", err
	}
	return "verself-cred://" + id, nil
}

func (s *Store) ReadCredential(ref string) (string, error) {
	id, ok := strings.CutPrefix(ref, "verself-cred://")
	if !ok || id == "" || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("credential ref invalid: %s", ref)
	}
	data, err := os.ReadFile(filepath.Join(s.paths.credentialDir(), id+".secret"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Store) DeleteCredential(ref string) error {
	id, ok := strings.CutPrefix(ref, "verself-cred://")
	if !ok || id == "" || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("credential ref invalid: %s", ref)
	}
	if err := os.Remove(filepath.Join(s.paths.credentialDir(), id+".secret")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) LoadEnv(scope EnvScope) (EnvStore, error) {
	var env EnvStore
	if err := readJSONFile(s.paths.envPath(scope), &env); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EnvStore{Version: 1, Scope: scope, Values: map[string]EnvSecret{}}, nil
		}
		return EnvStore{}, err
	}
	if env.Values == nil {
		env.Values = map[string]EnvSecret{}
	}
	return env, nil
}

func (s *Store) SaveEnv(env EnvStore) error {
	if env.Version == 0 {
		env.Version = 1
	}
	if env.Values == nil {
		env.Values = map[string]EnvSecret{}
	}
	return writeJSONFile(s.paths.envPath(env.Scope), env, 0o600)
}

func (s *Store) PutEnvSecret(scope EnvScope, key, value string) (string, error) {
	env, err := s.LoadEnv(scope)
	if err != nil {
		return "", err
	}
	ref, err := s.SaveCredential(value)
	if err != nil {
		return "", err
	}
	env.Values[key] = EnvSecret{
		Key:         key,
		ValueRef:    ref,
		Sensitivity: "secret",
		UpdatedAt:   time.Now().UTC(),
	}
	if err := s.SaveEnv(env); err != nil {
		return "", err
	}
	return ref, nil
}

func readJSONFile(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(path, data, mode)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
