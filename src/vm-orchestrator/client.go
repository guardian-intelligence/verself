package vmorchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	vmrpc "github.com/verself/vm-orchestrator/proto/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn   *grpc.ClientConn
	client vmrpc.VMServiceClient
}

func NewClient(ctx context.Context, socketPath string) (*Client, error) {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	conn, err := grpc.DialContext(ctx, "vm-orchestrator",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, fmt.Errorf("create vm-orchestrator client: %w", err)
	}
	return &Client{conn: conn, client: vmrpc.NewVMServiceClient(conn)}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) AcquireLease(ctx context.Context, key string, spec LeaseSpec) (LeaseRecord, error) {
	resp, err := c.client.AcquireLease(ctx, &vmrpc.AcquireLeaseRequest{IdempotencyKey: key, Spec: leaseSpecToProto(spec)})
	if err != nil {
		return LeaseRecord{}, fmt.Errorf("acquire lease: %w", err)
	}
	return LeaseRecord{
		LeaseID:    resp.GetLeaseId(),
		State:      leaseStateFromProto(resp.GetState()),
		AcquiredAt: timeFromUnixNs(resp.GetAcquiredAtUnixNs()),
		ExpiresAt:  timeFromUnixNs(resp.GetExpiresAtUnixNs()),
		VMIP:       resp.GetVmIp(),
		Resources:  vmResourcesFromProto(resp.GetResources()),
	}, nil
}

func (c *Client) RenewLease(ctx context.Context, leaseID, key string, extendSeconds uint64, allowlist []string) (time.Time, error) {
	resp, err := c.client.RenewLease(ctx, &vmrpc.RenewLeaseRequest{LeaseId: leaseID, IdempotencyKey: key, ExtendSeconds: extendSeconds, CheckpointSaveAllowlist: allowlist})
	if err != nil {
		return time.Time{}, fmt.Errorf("renew lease %s: %w", leaseID, err)
	}
	return timeFromUnixNs(resp.GetExpiresAtUnixNs()), nil
}

func (c *Client) ReleaseLease(ctx context.Context, leaseID, key string) error {
	_, err := c.client.ReleaseLease(ctx, &vmrpc.ReleaseLeaseRequest{LeaseId: leaseID, IdempotencyKey: key})
	if err != nil {
		return fmt.Errorf("release lease %s: %w", leaseID, err)
	}
	return nil
}

func (c *Client) StartExec(ctx context.Context, leaseID, key string, spec ExecSpec) (ExecRecord, error) {
	resp, err := c.client.StartExec(ctx, &vmrpc.StartExecRequest{LeaseId: leaseID, IdempotencyKey: key, Spec: execSpecToProto(spec)})
	if err != nil {
		return ExecRecord{}, fmt.Errorf("start exec %s: %w", leaseID, err)
	}
	return ExecRecord{
		LeaseID:   resp.GetLeaseId(),
		ExecID:    resp.GetExecId(),
		State:     execStateFromProto(resp.GetState()),
		StartedAt: timeFromUnixNs(resp.GetStartedAtUnixNs()),
	}, nil
}

func (c *Client) WaitExec(ctx context.Context, leaseID, execID string, includeOutput bool) (ExecRecord, error) {
	resp, err := c.client.WaitExec(ctx, &vmrpc.WaitExecRequest{LeaseId: leaseID, ExecId: execID, IncludeOutput: includeOutput})
	if err != nil {
		return ExecRecord{}, fmt.Errorf("wait exec %s/%s: %w", leaseID, execID, err)
	}
	return execRecordFromProto(resp.GetExec()), nil
}

func (c *Client) GetExec(ctx context.Context, leaseID, execID string, includeOutput bool) (ExecRecord, error) {
	resp, err := c.client.GetExec(ctx, &vmrpc.GetExecRequest{LeaseId: leaseID, ExecId: execID, IncludeOutput: includeOutput})
	if err != nil {
		return ExecRecord{}, fmt.Errorf("get exec %s/%s: %w", leaseID, execID, err)
	}
	return execRecordFromProto(resp.GetExec()), nil
}

func (c *Client) CancelExec(ctx context.Context, leaseID, execID, key, reason string) (bool, error) {
	resp, err := c.client.CancelExec(ctx, &vmrpc.CancelExecRequest{LeaseId: leaseID, ExecId: execID, IdempotencyKey: key, Reason: reason})
	if err != nil {
		return false, fmt.Errorf("cancel exec %s/%s: %w", leaseID, execID, err)
	}
	return resp.GetAccepted(), nil
}

func (c *Client) CommitFilesystemMount(ctx context.Context, leaseID, key, mountName, targetSourceRef string) (FilesystemCommitRecord, error) {
	resp, err := c.client.CommitFilesystemMount(ctx, &vmrpc.CommitFilesystemMountRequest{
		LeaseId:         leaseID,
		IdempotencyKey:  key,
		MountName:       mountName,
		TargetSourceRef: targetSourceRef,
	})
	if err != nil {
		return FilesystemCommitRecord{}, fmt.Errorf("commit filesystem mount %s/%s: %w", leaseID, mountName, err)
	}
	return FilesystemCommitRecord{
		LeaseID:         resp.GetLeaseId(),
		MountName:       resp.GetMountName(),
		TargetSourceRef: resp.GetTargetSourceRef(),
		Snapshot:        resp.GetSnapshot(),
		CommittedAt:     timeFromUnixNs(resp.GetCommittedAtUnixNs()),
	}, nil
}

func (c *Client) StreamLeaseEvents(ctx context.Context, leaseID string, fromSeq uint64, follow bool, handler func(LeaseEvent) error) error {
	stream, err := c.client.StreamLeaseEvents(ctx, &vmrpc.StreamLeaseEventsRequest{LeaseId: leaseID, FromSeq: fromSeq, Follow: follow})
	if err != nil {
		return fmt.Errorf("stream lease events %s: %w", leaseID, err)
	}
	for {
		event, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("recv lease event %s: %w", leaseID, err)
		}
		if handler != nil {
			if err := handler(leaseEventFromProto(event)); err != nil {
				return err
			}
		}
	}
}

func (c *Client) GetCapacity(ctx context.Context) (Capacity, error) {
	resp, err := c.client.GetCapacity(ctx, &vmrpc.GetCapacityRequest{})
	if err != nil {
		return Capacity{}, fmt.Errorf("get capacity: %w", err)
	}
	out := Capacity{GuestPoolCIDR: resp.GetGuestPoolCidr()}
	if pool := resp.GetPool(); pool != nil {
		out.TotalSlots = pool.GetTotalSlots()
		out.LeasesHeld = pool.GetLeasesHeld()
		out.LeasesAvailable = pool.GetLeasesAvailable()
		out.MaxVCPUsPerLease = pool.GetMaxVcpusPerLease()
		out.MaxMemoryMiBPerLease = pool.GetMaxMemoryMibPerLease()
		out.MaxRootDiskGiBPerLease = pool.GetMaxRootDiskGibPerLease()
		out.RootfsProvisionedBytes = pool.GetRootfsProvisionedBytes()
	}
	return out, nil
}

func leaseSpecToProto(spec LeaseSpec) *vmrpc.LeaseSpec {
	mode := vmrpc.NetworkAttachMode_NETWORK_ATTACH_MODE_NAT
	if spec.NetworkMode == "none" {
		mode = vmrpc.NetworkAttachMode_NETWORK_ATTACH_MODE_NONE
	}
	return &vmrpc.LeaseSpec{
		Resources:               vmResourcesToProto(spec.Resources),
		FromCheckpointRef:       spec.FromCheckpointRef,
		TtlSeconds:              spec.TTLSeconds,
		TrustClass:              spec.TrustClass,
		CheckpointSaveAllowlist: append([]string(nil), spec.CheckpointSaveAllowlist...),
		Network:                 &vmrpc.NetworkAttach{Mode: mode},
		FilesystemMounts:        filesystemMountsToProto(spec.FilesystemMounts),
	}
}

func execSpecToProto(spec ExecSpec) *vmrpc.ExecSpec {
	return &vmrpc.ExecSpec{
		Argv:           append([]string(nil), spec.Argv...),
		WorkingDir:     spec.WorkingDir,
		Env:            cloneStringMap(spec.Env),
		Stdin:          vmrpc.StdioMode_STDIO_MODE_NONE,
		Stdout:         vmrpc.StdioMode_STDIO_MODE_BUFFERED,
		Stderr:         vmrpc.StdioMode_STDIO_MODE_BUFFERED,
		MaxWallSeconds: spec.MaxWallSeconds,
	}
}

func execRecordFromProto(record *vmrpc.ExecRecord) ExecRecord {
	if record == nil {
		return ExecRecord{}
	}
	return ExecRecord{
		LeaseID:                record.GetLeaseId(),
		ExecID:                 record.GetExecId(),
		State:                  execStateFromProto(record.GetState()),
		ExitCode:               int(record.GetExitCode()),
		TerminalReason:         record.GetTerminalReason(),
		QueuedAt:               timeFromUnixNs(record.GetQueuedAtUnixNs()),
		StartedAt:              timeFromUnixNs(record.GetStartedAtUnixNs()),
		FirstByteAt:            timeFromUnixNs(record.GetFirstByteAtUnixNs()),
		ExitedAt:               timeFromUnixNs(record.GetExitedAtUnixNs()),
		StdoutBytes:            record.GetStdoutBytes(),
		StderrBytes:            record.GetStderrBytes(),
		DroppedLogBytes:        record.GetDroppedLogBytes(),
		Output:                 record.GetOutput(),
		Metrics:                vmMetricsFromProto(record.GetMetrics()),
		ZFSWritten:             record.GetZfsWritten(),
		RootfsProvisionedBytes: record.GetRootfsProvisionedBytes(),
	}
}

func leaseEventFromProto(event *vmrpc.LeaseEvent) LeaseEvent {
	if event == nil {
		return LeaseEvent{}
	}
	return LeaseEvent{
		LeaseID:   event.GetLeaseId(),
		Seq:       event.GetEventSeq(),
		Type:      leaseEventTypeFromProto(event.GetEventType()),
		ExecID:    event.GetExecId(),
		Attrs:     cloneStringMap(event.GetAttrs()),
		CreatedAt: timeFromUnixNs(event.GetCreatedAtUnixNs()),
	}
}
