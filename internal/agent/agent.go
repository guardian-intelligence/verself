package agent

import (
	"context"
	"log/slog"

	"github.com/forge-metal/forge-metal/internal/config"
)

// Agent is the worker daemon running on each bare-metal node.
// It heartbeats to the controller, receives job assignments,
// and executes CI workloads in gVisor sandboxes.
type Agent struct {
	cfg    config.AgentConfig
	nodeID string
}

// New creates a new Agent from config.
func New(cfg config.AgentConfig, nodeID string) *Agent {
	return &Agent{cfg: cfg, nodeID: nodeID}
}

// Run starts the agent loop: heartbeat + job execution.
func (a *Agent) Run(ctx context.Context) error {
	slog.Info("agent starting", "node_id", a.nodeID, "controller", a.cfg.ControllerAddr)
	// TODO: implement heartbeat loop, job receiver, sandbox executor
	<-ctx.Done()
	return ctx.Err()
}
