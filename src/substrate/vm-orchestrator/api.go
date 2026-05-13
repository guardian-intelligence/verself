package vmorchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// apiClient is a thin HTTP client over a Firecracker Unix socket.
// It covers only the endpoints used by verself's VM runtime.
type apiClient struct {
	client *http.Client
	base   string // "http://localhost" (routed via Unix socket)
}

func newAPIClient(socketPath string) *apiClient {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &apiClient{
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, "unix", socketPath)
				},
			},
		},
		base: "http://localhost",
	}
}

// --- API request/response types ---
// Inline structs for the small set of endpoints we use. Not worth abstracting.

type bootSourceReq struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

type driveReq struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

type machineConfigReq struct {
	VCPUCount  int  `json:"vcpu_count"`
	MemSizeMiB int  `json:"mem_size_mib"`
	SMT        bool `json:"smt"`
}

type networkInterfaceReq struct {
	IfaceID     string `json:"iface_id"`
	HostDevName string `json:"host_dev_name"`
	GuestMAC    string `json:"guest_mac"`
}

type vsockReq struct {
	GuestCID uint32 `json:"guest_cid"`
	UDSPath  string `json:"uds_path"`
}

type actionReq struct {
	ActionType string `json:"action_type"`
}

type metricsReq struct {
	MetricsPath string `json:"metrics_path"`
}

type entropyReq struct{}

// --- API methods ---

func (c *apiClient) putBootSource(ctx context.Context, kernelPath, bootArgs string) error {
	return c.put(ctx, "/boot-source", bootSourceReq{
		KernelImagePath: kernelPath,
		BootArgs:        bootArgs,
	})
}

func (c *apiClient) putDrive(ctx context.Context, driveID, path string, rootDevice, readOnly bool) error {
	return c.put(ctx, "/drives/"+driveID, driveReq{
		DriveID:      driveID,
		PathOnHost:   path,
		IsRootDevice: rootDevice,
		IsReadOnly:   readOnly,
	})
}

func (c *apiClient) putMachineConfig(ctx context.Context, vcpus, memMiB int) error {
	return c.put(ctx, "/machine-config", machineConfigReq{
		VCPUCount:  vcpus,
		MemSizeMiB: memMiB,
		SMT:        false,
	})
}

func (c *apiClient) putNetworkInterface(ctx context.Context, ifaceID, tapName, guestMAC string) error {
	return c.put(ctx, "/network-interfaces/"+ifaceID, networkInterfaceReq{
		IfaceID:     ifaceID,
		HostDevName: tapName,
		GuestMAC:    guestMAC,
	})
}

func (c *apiClient) putVsock(ctx context.Context, guestCID uint32, udsPath string) error {
	return c.put(ctx, "/vsock", vsockReq{
		GuestCID: guestCID,
		UDSPath:  udsPath,
	})
}

func (c *apiClient) putMetrics(ctx context.Context, metricsPath string) error {
	return c.put(ctx, "/metrics", metricsReq{
		MetricsPath: metricsPath,
	})
}

func (c *apiClient) putEntropy(ctx context.Context) error {
	return c.put(ctx, "/entropy", entropyReq{})
}

func (c *apiClient) startInstance(ctx context.Context) error {
	return c.put(ctx, "/actions", actionReq{ActionType: "InstanceStart"})
}

// put sends a PUT request with JSON body.
func (c *apiClient) put(ctx context.Context, path string, body interface{}) error {
	return c.doJSON(ctx, http.MethodPut, path, body)
}

func (c *apiClient) doJSON(ctx context.Context, method, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	return nil
}
