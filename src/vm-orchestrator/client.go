package vmorchestrator

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultPollInterval   = 200 * time.Millisecond
	defaultMaxMessageSize = 32 << 20
)

type Client struct {
	conn   *grpc.ClientConn
	client vmrpc.VMServiceClient
}

func NewClient(ctx context.Context, socketPath string) (*Client, error) {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	conn, err := grpc.DialContext(
		ctx,
		"vm-orchestrator",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(defaultMaxMessageSize),
			grpc.MaxCallSendMsgSize(defaultMaxMessageSize),
		),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial vm-orchestrator socket %s: %w", socketPath, err)
	}

	return &Client{
		conn:   conn,
		client: vmrpc.NewVMServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) EnsureRun(ctx context.Context, spec HostRunSpec) (string, bool, error) {
	ctx, span := tracer.Start(ctx, "vm-orchestrator.EnsureRun")
	defer span.End()
	span.SetAttributes(attribute.String("run.id", spec.RunID))

	resp, err := c.client.EnsureRun(ctx, &vmrpc.EnsureRunRequest{Spec: hostRunSpecToProto(spec)})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", false, fmt.Errorf("ensure run: %w", err)
	}
	span.SetAttributes(
		attribute.String("run.id", resp.GetRunId()),
		attribute.Bool("run.created", resp.GetCreated()),
		attribute.String("run.state", resp.GetState().String()),
	)
	return resp.GetRunId(), resp.GetCreated(), nil
}

func (c *Client) Run(ctx context.Context, spec HostRunSpec) (RunResult, error) {
	runID, _, err := c.EnsureRun(ctx, spec)
	if err != nil {
		return RunResult{}, err
	}

	snapshot, err := c.WaitRun(ctx, runID, true)
	if err != nil {
		cancelErr := err
		if ctx.Err() != nil {
			_, _ = c.CancelRun(context.Background(), runID, "context_canceled")
			cancelErr = ctx.Err()
		}
		return RunResult{}, cancelErr
	}
	if snapshot.Result == nil {
		return RunResult{}, fmt.Errorf("run %s completed without result", snapshot.RunID)
	}
	if snapshot.TerminalReason != "" && snapshot.State != RunStateSucceeded {
		return *snapshot.Result, fmt.Errorf("run %s failed: %s", snapshot.RunID, snapshot.TerminalReason)
	}
	return *snapshot.Result, nil
}

func (c *Client) WaitRun(ctx context.Context, runID string, includeOutput bool) (HostRunSnapshot, error) {
	ctx, span := tracer.Start(ctx, "vm-orchestrator.WaitRun",
		trace.WithAttributes(
			attribute.String("run.id", runID),
			attribute.Bool("run.include_output", includeOutput),
		))
	defer span.End()

	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()

	for {
		snapshot, err := c.GetRun(ctx, runID, includeOutput)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return HostRunSnapshot{}, err
		}
		if snapshot.Terminal {
			span.SetAttributes(
				attribute.String("run.state", runStateName(snapshot.State)),
				attribute.Bool("run.terminal", snapshot.Terminal),
				attribute.String("run.terminal_reason", snapshot.TerminalReason),
			)
			return snapshot, nil
		}

		select {
		case <-ctx.Done():
			return HostRunSnapshot{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) GetRun(ctx context.Context, runID string, includeOutput bool) (HostRunSnapshot, error) {
	resp, err := c.client.GetRun(ctx, &vmrpc.GetRunRequest{
		RunId:         runID,
		IncludeOutput: includeOutput,
	})
	if err != nil {
		return HostRunSnapshot{}, fmt.Errorf("get run %s: %w", runID, err)
	}
	return hostRunSnapshotFromProto(resp), nil
}

func (c *Client) StreamRunEvents(ctx context.Context, runID string, fromSeq uint64, follow bool, handler func(HostRunEvent) error) error {
	stream, err := c.client.StreamRunEvents(ctx, &vmrpc.StreamRunEventsRequest{
		RunId:   runID,
		FromSeq: fromSeq,
		Follow:  follow,
	})
	if err != nil {
		return fmt.Errorf("stream run events %s: %w", runID, err)
	}
	for {
		event, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				return nil
			}
			return fmt.Errorf("recv run event %s: %w", runID, recvErr)
		}
		if handler == nil {
			continue
		}
		if err := handler(hostRunEventFromProto(event)); err != nil {
			return err
		}
	}
}

func (c *Client) CancelRun(ctx context.Context, runID, reason string) (bool, error) {
	resp, err := c.client.CancelRun(ctx, &vmrpc.CancelRunRequest{RunId: runID, Reason: reason})
	if err != nil {
		return false, fmt.Errorf("cancel run %s: %w", runID, err)
	}
	return resp.GetAccepted(), nil
}

func (c *Client) GetCapacity(ctx context.Context) (Capacity, error) {
	resp, err := c.client.GetCapacity(ctx, &vmrpc.GetCapacityRequest{})
	if err != nil {
		return Capacity{}, fmt.Errorf("get capacity: %w", err)
	}
	return Capacity{
		GuestPoolCIDR:          resp.GetGuestPoolCidr(),
		TotalSlots:             resp.GetTotalSlots(),
		ActiveRuns:             resp.GetActiveRuns(),
		AvailableSlots:         resp.GetAvailableSlots(),
		VCPUsPerVM:             resp.GetVcpusPerVm(),
		MemoryMiBPerVM:         resp.GetMemoryMibPerVm(),
		RootfsProvisionedBytes: resp.GetRootfsProvisionedBytes(),
	}, nil
}
