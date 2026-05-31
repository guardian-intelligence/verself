package main

import (
	"context"
	"fmt"

	"github.com/verself/deployment-tools/internal/postgresruntime"
	"github.com/verself/deployment-tools/internal/runtime"
)

func applyPostgresBase(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) error {
	result, err := postgresruntime.ApplyBase(ctx, postgresApplyOptions(rt, plan), plan.Postgres)
	if err != nil {
		return err
	}
	fmt.Printf("verself-deploy: postgresql reconciled base roles=%d databases=%d peer_mappings=%d\n", result.Roles, result.Databases, result.PeerMappings)
	return nil
}

func applyPostgresReplicationRoles(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) error {
	result, err := postgresruntime.ApplyReplicationRoles(ctx, postgresApplyOptions(rt, plan), plan.Postgres)
	if err != nil {
		return err
	}
	if result.ReplicationRoles > 0 {
		fmt.Printf("verself-deploy: postgresql reconciled replication_roles=%d\n", result.ReplicationRoles)
	}
	return nil
}

func applyPostgresPublications(ctx context.Context, rt *runtime.Runtime, plan *deployPlan, strict bool) error {
	result, err := postgresruntime.ApplyPublications(ctx, postgresApplyOptions(rt, plan), plan.Postgres, strict)
	if err != nil {
		return err
	}
	if result.Publications > 0 && strict {
		fmt.Printf("verself-deploy: postgresql reconciled publications=%d\n", result.Publications)
	}
	return nil
}

func postgresApplyOptions(rt *runtime.Runtime, plan *deployPlan) postgresruntime.ApplyOptions {
	return postgresruntime.ApplyOptions{
		Site:   plan.Site,
		SSH:    rt.SSH,
		Tracer: rt.Tracer,
		Token:  openBaoReconcileToken(),
	}
}
