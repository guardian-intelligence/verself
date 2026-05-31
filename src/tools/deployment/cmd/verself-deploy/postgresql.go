package main

import (
	"context"
	"fmt"

	"github.com/verself/deployment-tools/internal/postgresruntime"
	"github.com/verself/deployment-tools/internal/runtime"
)

func applyPostgresBase(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) error {
	runtimePath, err := artifactRemotePath(plan, "postgresql-runtime")
	if err != nil {
		return err
	}
	result, err := postgresruntime.ApplyBase(ctx, postgresApplyOptions(rt, plan, runtimePath), plan.Postgres)
	if err != nil {
		return err
	}
	fmt.Printf("verself-deploy: postgresql reconciled base roles=%d databases=%d peer_mappings=%d\n", result.Roles, result.Databases, result.PeerMappings)
	return nil
}

func applyPostgresReplicationRoles(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) error {
	runtimePath, err := artifactRemotePath(plan, "postgresql-runtime")
	if err != nil {
		return err
	}
	result, err := postgresruntime.ApplyReplicationRoles(ctx, postgresApplyOptions(rt, plan, runtimePath), plan.Postgres)
	if err != nil {
		return err
	}
	if result.ReplicationRoles > 0 {
		fmt.Printf("verself-deploy: postgresql reconciled replication_roles=%d\n", result.ReplicationRoles)
	}
	return nil
}

func applyPostgresPublications(ctx context.Context, rt *runtime.Runtime, plan *deployPlan, strict bool) error {
	runtimePath, err := artifactRemotePath(plan, "postgresql-runtime")
	if err != nil {
		return err
	}
	result, err := postgresruntime.ApplyPublications(ctx, postgresApplyOptions(rt, plan, runtimePath), plan.Postgres, strict)
	if err != nil {
		return err
	}
	if result.Publications > 0 && strict {
		fmt.Printf("verself-deploy: postgresql reconciled publications=%d\n", result.Publications)
	}
	return nil
}

func postgresApplyOptions(rt *runtime.Runtime, plan *deployPlan, runtimePath string) postgresruntime.ApplyOptions {
	return postgresruntime.ApplyOptions{
		Site:        plan.Site,
		SSH:         rt.SSH,
		Tracer:      rt.Tracer,
		RuntimePath: runtimePath,
	}
}

func artifactRemotePath(plan *deployPlan, output string) (string, error) {
	for _, artifact := range plan.Artifacts {
		if artifact.Output == output {
			return artifact.Key, nil
		}
	}
	return "", fmt.Errorf("artifact %q is not in deploy plan", output)
}
