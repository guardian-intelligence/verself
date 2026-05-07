package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/verself/deployment-tools/internal/ansible"
	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/runtime"
)

func applyHostFirewall(ctx context.Context, rt *runtime.Runtime, db *deploydb.Client, repoRoot, site string) error {
	registryPath, err := buildHostNftablesRegistry(ctx, repoRoot)
	if err != nil {
		return fmt.Errorf("build host nftables registry: %w", err)
	}
	ansibleDir := filepath.Join(repoRoot, "src", "host", "ansible")
	inventory := filepath.Join(repoRoot, "src", "host", "sites", site, "inventory.ini")
	res, err := ansible.Run(ctx, db, ansible.Options{
		Playbook:   "playbooks/nftables.yml",
		Inventory:  inventory,
		AnsibleDir: ansibleDir,
		ExtraArgs: []string{
			"-e", "verself_site=" + site,
			"-e", "component_substrate_registry_file=" + registryPath,
			"-e", "verself_repo_root=" + repoRoot,
		},
		Site:         site,
		Phase:        "host_firewall",
		RunKey:       rt.Identity.RunKey(),
		OTLPEndpoint: rt.OTLPEndpoint(),
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("host firewall playbook exited %d", res.ExitCode)
	}
	return nil
}
