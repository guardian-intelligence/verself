package vmorchestrator

import (
	"context"
	"fmt"
	"net"
	"time"

	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
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

func (c *Client) Run(ctx context.Context, job JobConfig) (JobResult, error) {
	return c.RunWithConfig(ctx, Config{}, job)
}

func (c *Client) RunWithConfig(ctx context.Context, cfg Config, job JobConfig) (JobResult, error) {
	status, err := c.runAndWait(ctx, &vmrpc.CreateJobRequest{
		Spec: &vmrpc.CreateJobRequest_DirectJob{
			DirectJob: &vmrpc.DirectJobSpec{
				RuntimeConfig: configToProto(cfg),
				Job:           jobConfigToProto(job),
			},
		},
	})
	if err != nil {
		return JobResult{}, err
	}
	if status.Result == nil {
		return JobResult{}, fmt.Errorf("job %s completed without result", status.JobID)
	}
	return *status.Result, nil
}

func (c *Client) StartDirectJob(ctx context.Context, job JobConfig) (string, error) {
	return c.StartDirectJobWithConfig(ctx, Config{}, job)
}

func (c *Client) StartDirectJobWithConfig(ctx context.Context, cfg Config, job JobConfig) (string, error) {
	resp, err := c.client.CreateJob(ctx, &vmrpc.CreateJobRequest{
		Spec: &vmrpc.CreateJobRequest_DirectJob{
			DirectJob: &vmrpc.DirectJobSpec{
				RuntimeConfig: configToProto(cfg),
				Job:           jobConfigToProto(job),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create direct job: %w", err)
	}
	return resp.GetJobId(), nil
}

func (c *Client) StartRepoExec(ctx context.Context, req RepoExecRequest) (string, error) {
	resp, err := c.client.CreateJob(ctx, &vmrpc.CreateJobRequest{
		Spec: &vmrpc.CreateJobRequest_RepoExec{
			RepoExec: &vmrpc.RepoExecSpec{
				RuntimeConfig:   configToProto(req.Config),
				Repo:            req.Repo,
				RepoUrl:         req.RepoURL,
				Ref:             req.Ref,
				JobTemplate:     jobConfigToProto(req.JobTemplate),
				LockfileRelPath: req.LockfileRelPath,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create repo exec job: %w", err)
	}
	return resp.GetJobId(), nil
}

func (c *Client) ExecRepo(ctx context.Context, req RepoExecRequest) (JobStatus, error) {
	return c.runAndWait(ctx, &vmrpc.CreateJobRequest{
		Spec: &vmrpc.CreateJobRequest_RepoExec{
			RepoExec: &vmrpc.RepoExecSpec{
				RuntimeConfig:   configToProto(req.Config),
				Repo:            req.Repo,
				RepoUrl:         req.RepoURL,
				Ref:             req.Ref,
				JobTemplate:     jobConfigToProto(req.JobTemplate),
				LockfileRelPath: req.LockfileRelPath,
			},
		},
	})
}

func (c *Client) WaitJob(ctx context.Context, jobID string, includeOutput bool) (JobStatus, error) {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()

	for {
		status, err := c.GetJobStatus(ctx, jobID, includeOutput)
		if err != nil {
			return JobStatus{}, err
		}
		if status.Terminal {
			return status, nil
		}

		select {
		case <-ctx.Done():
			return JobStatus{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) GetJobStatus(ctx context.Context, jobID string, includeOutput bool) (JobStatus, error) {
	resp, err := c.client.GetJobStatus(ctx, &vmrpc.GetJobStatusRequest{
		JobId:         jobID,
		IncludeOutput: includeOutput,
	})
	if err != nil {
		return JobStatus{}, fmt.Errorf("get job status %s: %w", jobID, err)
	}
	return jobStatusFromProto(resp), nil
}

func (c *Client) CancelJob(ctx context.Context, jobID string) (bool, error) {
	resp, err := c.client.CancelJob(ctx, &vmrpc.CancelJobRequest{JobId: jobID})
	if err != nil {
		return false, fmt.Errorf("cancel job %s: %w", jobID, err)
	}
	return resp.GetCanceled(), nil
}

func (c *Client) WarmGolden(ctx context.Context, req WarmGoldenRequest) (WarmGoldenResult, error) {
	resp, err := c.client.WarmGolden(ctx, &vmrpc.WarmGoldenRequest{
		RuntimeConfig:   configToProto(req.Config),
		Repo:            req.Repo,
		RepoUrl:         req.RepoURL,
		DefaultBranch:   req.DefaultBranch,
		Job:             jobConfigToProto(req.Job),
		LockfileRelPath: req.LockfileRelPath,
	})
	if err != nil {
		return WarmGoldenResult{}, fmt.Errorf("warm golden for %s: %w", req.Repo, err)
	}
	return warmGoldenResultFromProto(resp), nil
}

func (c *Client) GetFleetSnapshot(ctx context.Context) ([]FleetVM, error) {
	resp, err := c.client.GetFleetSnapshot(ctx, &vmrpc.GetFleetSnapshotRequest{})
	if err != nil {
		return nil, fmt.Errorf("get fleet snapshot: %w", err)
	}
	out := make([]FleetVM, 0, len(resp.GetVms()))
	for _, vm := range resp.GetVms() {
		out = append(out, fleetVMFromProto(vm))
	}
	return out, nil
}

func (c *Client) GetCapacity(ctx context.Context) (Capacity, error) {
	resp, err := c.client.GetCapacity(ctx, &vmrpc.GetCapacityRequest{})
	if err != nil {
		return Capacity{}, fmt.Errorf("get capacity: %w", err)
	}
	return Capacity{
		GuestPoolCIDR:  resp.GetGuestPoolCidr(),
		TotalSlots:     resp.GetTotalSlots(),
		ActiveJobs:     resp.GetActiveJobs(),
		AvailableSlots: resp.GetAvailableSlots(),
		VCPUsPerVM:     resp.GetVcpusPerVm(),
		MemoryMiBPerVM: resp.GetMemoryMibPerVm(),
	}, nil
}

func (c *Client) runAndWait(ctx context.Context, req *vmrpc.CreateJobRequest) (JobStatus, error) {
	resp, err := c.client.CreateJob(ctx, req)
	if err != nil {
		return JobStatus{}, fmt.Errorf("create job: %w", err)
	}
	status, err := c.WaitJob(ctx, resp.GetJobId(), true)
	if err != nil {
		cancelErr := err
		if ctx.Err() != nil {
			_, _ = c.CancelJob(context.Background(), resp.GetJobId())
			cancelErr = ctx.Err()
		}
		return JobStatus{}, cancelErr
	}
	if status.ErrorMessage != "" && status.Result == nil {
		return status, fmt.Errorf("job %s failed: %s", status.JobID, status.ErrorMessage)
	}
	return status, nil
}
