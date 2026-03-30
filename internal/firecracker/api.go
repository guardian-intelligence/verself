package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

// apiClient is a thin HTTP client over a Firecracker Unix socket.
// Covers only the endpoints needed for the tracer bullet.
type apiClient struct {
	client *http.Client
	base   string // "http://localhost" (routed via Unix socket)
}

func newAPIClient(socketPath string) *apiClient {
	return &apiClient{
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		},
		base: "http://localhost",
	}
}

// --- API request/response types ---
// Inline structs for the 6 endpoints we use. Not worth abstracting.

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

type actionReq struct {
	ActionType string `json:"action_type"`
}

type loggerReq struct {
	LogPath    string `json:"log_path"`
	Level      string `json:"level"`
	ShowLevel  bool   `json:"show_level"`
	ShowOrigin bool   `json:"show_log_origin"`
}

type metricsReq struct {
	MetricsPath string `json:"metrics_path"`
}

// --- API methods ---

func (c *apiClient) putBootSource(ctx context.Context, kernelPath, bootArgs string) error {
	return c.put(ctx, "/boot-source", bootSourceReq{
		KernelImagePath: kernelPath,
		BootArgs:        bootArgs,
	})
}

func (c *apiClient) putDrive(ctx context.Context, driveID, path string, rootDevice bool) error {
	return c.put(ctx, "/drives/"+driveID, driveReq{
		DriveID:      driveID,
		PathOnHost:   path,
		IsRootDevice: rootDevice,
		IsReadOnly:   false,
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

func (c *apiClient) putLogger(ctx context.Context, logPath string) error {
	return c.put(ctx, "/logger", loggerReq{
		LogPath:    logPath,
		Level:      "Warning",
		ShowLevel:  true,
		ShowOrigin: true,
	})
}

func (c *apiClient) putMetrics(ctx context.Context, metricsPath string) error {
	return c.put(ctx, "/metrics", metricsReq{
		MetricsPath: metricsPath,
	})
}

func (c *apiClient) startInstance(ctx context.Context) error {
	return c.put(ctx, "/actions", actionReq{ActionType: "InstanceStart"})
}

func (c *apiClient) flushMetrics(ctx context.Context) error {
	return c.put(ctx, "/actions", actionReq{ActionType: "FlushMetrics"})
}

// put sends a PUT request with JSON body.
func (c *apiClient) put(ctx context.Context, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s: HTTP %d: %s", path, resp.StatusCode, string(respBody))
	}

	return nil
}
