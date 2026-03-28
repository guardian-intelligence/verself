package controller

import (
	"context"
	"log/slog"

	"github.com/forge-metal/forge-metal/internal/config"
)

// Controller is the central job scheduler and node registry.
// It accepts job submissions (Forgejo webhooks), tracks worker
// health via heartbeats, and assigns jobs to available nodes.
type Controller struct {
	cfg config.ControllerConfig
}

// New creates a new Controller from config.
func New(cfg config.ControllerConfig) *Controller {
	return &Controller{cfg: cfg}
}

// Run starts the controller: HTTP API + gRPC server + job scheduler.
func (c *Controller) Run(ctx context.Context) error {
	slog.Info("controller starting",
		"http", c.cfg.Listen,
		"grpc", c.cfg.GRPCListen,
	)
	// TODO: implement HTTP webhook receiver, gRPC agent service,
	// node registry, job queue, scheduler loop
	<-ctx.Done()
	return ctx.Err()
}
