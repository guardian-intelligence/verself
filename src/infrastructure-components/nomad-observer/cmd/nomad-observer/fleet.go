package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/hashicorp/nomad/api"
)

// FleetNodeRow is one snapshot of a schedulable node, keyed by the four
// placement coordinates (region=cell, datacenter=cluster, node_pool=role,
// meta=capabilities). Rows are append-only; current state is argMax over
// observed_at per node_id. Column order is irrelevant — AppendStruct binds by
// the ch tag, not position.
type FleetNodeRow struct {
	Region            string            `ch:"region"`
	Datacenter        string            `ch:"datacenter"`
	NodePool          string            `ch:"node_pool"`
	NodeClass         string            `ch:"node_class"`
	CPUISALevel       string            `ch:"cpu_isa_level"`
	NodeID            string            `ch:"node_id"`
	NodeName          string            `ch:"node_name"`
	SPIFFEID          string            `ch:"spiffe_id"`
	AttestationMethod string            `ch:"attestation_method"`
	Attested          uint8             `ch:"attested"`
	Status            string            `ch:"status"`
	Eligibility       string            `ch:"eligibility"`
	Drain             uint8             `ch:"drain"`
	Meta              map[string]string `ch:"meta"`
	ModifyIndex       uint64            `ch:"modify_index"`
	ObservedAt        time.Time         `ch:"observed_at"`
}

// fleetProjector periodically projects the live Nomad fleet into
// verself.fleet_nodes. The capability meta it records is read from each node's
// own Nomad client meta; the attested SPIRE node identity (spiffe_id /
// attestation_method) is populated once node identity becomes property-bearing.
type fleetProjector struct {
	nomad    *api.Client
	ch       clickhouse.Conn
	region   string
	interval time.Duration
	logger   *slog.Logger
}

func (p *fleetProjector) run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	// Snapshot immediately so the projection is populated without waiting a
	// full interval after start/restart.
	p.snapshotOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.snapshotOnce(ctx)
		}
	}
}

// snapshotOnce lists the fleet and inserts one row per node. Transient Nomad or
// ClickHouse errors are logged and retried on the next tick rather than
// crashing the observer — the projection is best-effort evidence, not a
// liveness dependency.
func (p *fleetProjector) snapshotOnce(ctx context.Context) {
	stubs, _, err := p.nomad.Nodes().List(&api.QueryOptions{})
	if err != nil {
		p.logger.WarnContext(ctx, "fleet.snapshot.list_failed", slog.String("error", err.Error()))
		return
	}
	observedAt := time.Now().UTC()
	batch, err := p.ch.PrepareBatch(ctx, "INSERT INTO verself.fleet_nodes")
	if err != nil {
		p.logger.WarnContext(ctx, "fleet.snapshot.prepare_failed", slog.String("error", err.Error()))
		return
	}
	rows := 0
	for _, stub := range stubs {
		node, _, err := p.nomad.Nodes().Info(stub.ID, &api.QueryOptions{})
		if err != nil {
			p.logger.WarnContext(ctx, "fleet.snapshot.node_info_failed",
				slog.String("nomad.node_id", stub.ID), slog.String("error", err.Error()))
			continue
		}
		meta := node.Meta
		if meta == nil {
			meta = map[string]string{}
		}
		row := FleetNodeRow{
			Region:            p.region,
			Datacenter:        node.Datacenter,
			NodePool:          node.NodePool,
			NodeClass:         node.NodeClass,
			CPUISALevel:       meta["cpu_isa_level"],
			NodeID:            node.ID,
			NodeName:          node.Name,
			Status:            node.Status,
			Eligibility:       node.SchedulingEligibility,
			Drain:             boolToUint8(node.Drain),
			Meta:              meta,
			ModifyIndex:       node.ModifyIndex,
			ObservedAt:        observedAt,
		}
		if err := batch.AppendStruct(&row); err != nil {
			_ = batch.Abort()
			p.logger.WarnContext(ctx, "fleet.snapshot.append_failed",
				slog.String("nomad.node_id", node.ID), slog.String("error", err.Error()))
			return
		}
		rows++
	}
	if err := batch.Send(); err != nil {
		p.logger.WarnContext(ctx, "fleet.snapshot.send_failed", slog.String("error", err.Error()))
		return
	}
	p.logger.InfoContext(ctx, "fleet.snapshot.inserted", slog.Int("rows", rows))
}

func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
