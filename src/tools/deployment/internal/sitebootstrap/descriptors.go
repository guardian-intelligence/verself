package sitebootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"
)

type bootstrapNomadDescriptor struct {
	Path        string
	Label       string   `json:"label"`
	JobID       string   `json:"job_id"`
	JobSpecPath string   `json:"job_spec_path"`
	Sites       []string `json:"sites"`
}

type runtimeAccessCatalog struct {
	RoleReads      map[string]map[string]struct{}
	RoleWrites     map[string]map[string]struct{}
	SecretReaders  map[string]map[string]struct{}
	SecretWriters  map[string]map[string]struct{}
	PrincipalRoles map[string]map[string]struct{}
	Referenced     map[string]struct{}
}

var runtimeSecretRefRE = regexp.MustCompile(`kv-runtime/data/secret/org/([A-Za-z0-9._-]+)`)

func partitionBootstrapNomadDescriptors(site string, paths []string, firstJobIDs []string) ([]string, []string, []bootstrapNomadDescriptor, error) {
	firstSet := map[string]bool{}
	for _, jobID := range firstJobIDs {
		firstSet[strings.TrimSpace(jobID)] = true
	}
	first := []string{}
	remaining := []string{}
	active := []bootstrapNomadDescriptor{}
	found := map[string]bool{}
	for _, descriptorPath := range paths {
		descriptor, ok, err := readBootstrapNomadDescriptor(site, descriptorPath)
		if err != nil {
			return nil, nil, nil, err
		}
		if !ok {
			continue
		}
		active = append(active, descriptor)
		if firstSet[descriptor.JobID] {
			first = append(first, descriptor.Path)
			found[descriptor.JobID] = true
			continue
		}
		remaining = append(remaining, descriptor.Path)
	}
	for _, jobID := range firstJobIDs {
		if !found[strings.TrimSpace(jobID)] {
			return nil, nil, nil, fmt.Errorf("bootstrap Nomad descriptor for job_id %q was not found", jobID)
		}
	}
	return first, remaining, active, nil
}

func readBootstrapNomadDescriptor(site, descriptorPath string) (bootstrapNomadDescriptor, bool, error) {
	body, err := os.ReadFile(descriptorPath)
	if err != nil {
		return bootstrapNomadDescriptor{}, false, fmt.Errorf("read %s: %w", descriptorPath, err)
	}
	var descriptor bootstrapNomadDescriptor
	if err := json.Unmarshal(body, &descriptor); err != nil {
		return bootstrapNomadDescriptor{}, false, fmt.Errorf("decode %s: %w", descriptorPath, err)
	}
	if descriptor.Label == "" || descriptor.JobID == "" || descriptor.JobSpecPath == "" {
		return bootstrapNomadDescriptor{}, false, fmt.Errorf("%s: descriptor requires label, job_id, and job_spec_path", descriptorPath)
	}
	descriptor.Path = descriptorPath
	if len(descriptor.Sites) == 0 {
		return descriptor, true, nil
	}
	for _, candidate := range descriptor.Sites {
		if candidate == site {
			return descriptor, true, nil
		}
	}
	return descriptor, false, nil
}

func inspectNomadRuntimeAccess(ctx context.Context, nomadAddr, repoRoot string, descriptors []bootstrapNomadDescriptor, secrets runtimeSecretCatalog) (runtimeAccessCatalog, error) {
	cfg := api.DefaultConfig()
	cfg.Address = nomadAddr
	client, err := api.NewClient(cfg)
	if err != nil {
		return runtimeAccessCatalog{}, fmt.Errorf("nomad client: %w", err)
	}
	access := runtimeAccessCatalog{
		RoleReads:      map[string]map[string]struct{}{},
		RoleWrites:     map[string]map[string]struct{}{},
		SecretReaders:  map[string]map[string]struct{}{},
		SecretWriters:  map[string]map[string]struct{}{},
		PrincipalRoles: map[string]map[string]struct{}{},
		Referenced:     map[string]struct{}{},
	}
	for _, descriptor := range descriptors {
		specPath := resolveRepoPath(repoRoot, descriptor.JobSpecPath)
		body, err := os.ReadFile(specPath)
		if err != nil {
			return runtimeAccessCatalog{}, fmt.Errorf("read %s: %w", specPath, err)
		}
		job, err := client.Jobs().ParseHCLOpts(&api.JobsParseRequest{
			JobHCL:       string(body),
			Canonicalize: false,
		})
		if err != nil {
			return runtimeAccessCatalog{}, fmt.Errorf("parse %s: %w", specPath, err)
		}
		jobID := descriptor.JobID
		if job != nil && job.ID != nil && strings.TrimSpace(*job.ID) != "" {
			jobID = strings.TrimSpace(*job.ID)
		}
		for _, group := range job.TaskGroups {
			for _, task := range group.Tasks {
				if task == nil || task.Vault == nil || strings.TrimSpace(task.Vault.Role) == "" {
					continue
				}
				role := strings.TrimSpace(task.Vault.Role)
				access.addPrincipalRole(jobID, role)
				access.addPrincipalRole(task.Name, role)
				for _, tmpl := range task.Templates {
					if tmpl == nil || tmpl.EmbeddedTmpl == nil {
						continue
					}
					for _, secret := range runtimeSecretRefs(*tmpl.EmbeddedTmpl) {
						access.addRoleRead(role, secret)
					}
				}
			}
		}
	}
	for name := range access.Referenced {
		if _, ok := secrets.ByName[name]; !ok {
			return runtimeAccessCatalog{}, fmt.Errorf("Nomad templates reference undeclared OpenBao runtime secret %s", name)
		}
	}
	for _, declaration := range secrets.Declarations {
		name := declaration.Name
		for _, principal := range declaration.readerPrincipals() {
			for role := range access.PrincipalRoles[strings.TrimSpace(principal)] {
				access.addRoleRead(role, name)
			}
		}
		for _, principal := range declaration.writerPrincipals() {
			for role := range access.PrincipalRoles[strings.TrimSpace(principal)] {
				access.addRoleWrite(role, name)
			}
		}
	}
	return access, nil
}

func (c *runtimeAccessCatalog) addPrincipalRole(principal, role string) {
	principal = strings.TrimSpace(principal)
	role = strings.TrimSpace(role)
	if principal == "" || role == "" {
		return
	}
	addSetValue(c.PrincipalRoles, principal, role)
}

func (c *runtimeAccessCatalog) addRoleRead(role, secret string) {
	role = strings.TrimSpace(role)
	secret = strings.TrimSpace(secret)
	if role == "" || secret == "" {
		return
	}
	addSetValue(c.RoleReads, role, secret)
	addSetValue(c.SecretReaders, secret, role)
	c.Referenced[secret] = struct{}{}
}

func (c *runtimeAccessCatalog) addRoleWrite(role, secret string) {
	role = strings.TrimSpace(role)
	secret = strings.TrimSpace(secret)
	if role == "" || secret == "" {
		return
	}
	addSetValue(c.RoleWrites, role, secret)
	addSetValue(c.SecretWriters, secret, role)
}

func runtimeSecretRefs(template string) []string {
	matches := runtimeSecretRefRE.FindAllStringSubmatch(template, -1)
	out := []string{}
	seen := map[string]bool{}
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func resolveRepoPath(repoRoot, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(repoRoot, filepath.FromSlash(rel))
}

func addSetValue(values map[string]map[string]struct{}, key, value string) {
	if values[key] == nil {
		values[key] = map[string]struct{}{}
	}
	values[key][value] = struct{}{}
}

func sortedSetValues(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
