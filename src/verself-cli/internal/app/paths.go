package app

import (
	"errors"
	"os"
	"path/filepath"
)

type EnvLookup func(string) string

type XDGPaths struct {
	ConfigDir string
	DataDir   string
	StateDir  string
	CacheDir  string
}

func resolveXDG(getenv EnvLookup) (XDGPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return XDGPaths{}, err
	}
	if home == "" {
		return XDGPaths{}, errors.New("resolve xdg: home directory is empty")
	}
	base := func(name, fallback string) string {
		if v := getenv(name); v != "" {
			return filepath.Join(v, "verself")
		}
		return filepath.Join(home, fallback, "verself")
	}
	cache := getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	return XDGPaths{
		ConfigDir: base("XDG_CONFIG_HOME", ".config"),
		DataDir:   base("XDG_DATA_HOME", filepath.Join(".local", "share")),
		StateDir:  base("XDG_STATE_HOME", filepath.Join(".local", "state")),
		CacheDir:  filepath.Join(cache, "verself"),
	}, nil
}

func (p XDGPaths) configPath() string {
	return filepath.Join(p.ConfigDir, "config.json")
}

func (p XDGPaths) companyPath(name string) string {
	return filepath.Join(p.DataDir, "companies", name+".json")
}

func (p XDGPaths) profilePath(name string) string {
	return filepath.Join(p.DataDir, "profiles", name+".json")
}

func (p XDGPaths) accountDir(profile string) string {
	return filepath.Join(p.DataDir, "accounts", profile)
}

func (p XDGPaths) accountPath(profile, handle string) string {
	return filepath.Join(p.accountDir(profile), handle+".json")
}

func (p XDGPaths) credentialDir() string {
	return filepath.Join(p.StateDir, "credentials")
}

func (p XDGPaths) envPath(scope EnvScope) string {
	return filepath.Join(p.DataDir, "env", scope.Org, scope.Project, scope.Environment+".json")
}

func (p XDGPaths) bootstrapRunPath(runID string) string {
	return filepath.Join(p.StateDir, "bootstrap", runID+".json")
}
