package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/nomad/api"
	ocidigest "github.com/opencontainers/go-digest"
)

const (
	podmanMeasureSocketEnv = "NOMAD_OBSERVER_PODMAN_MEASURE_SOCKET"
	podmanMeasureGroupEnv  = "NOMAD_OBSERVER_PODMAN_MEASURE_GROUP"
	podmanBinary           = "/usr/bin/podman"
	podmanCommandTimeout   = 2 * time.Second

	labelNomadAllocID   = "com.hashicorp.nomad.alloc_id"
	labelNomadNamespace = "com.hashicorp.nomad.namespace"
	labelNomadJobID     = "com.hashicorp.nomad.job_id"
	labelNomadTaskGroup = "com.hashicorp.nomad.task_group_name"
	labelNomadTaskName  = "com.hashicorp.nomad.task_name"
)

type podmanMeasurerConfig struct {
	socketPath string
	group      string
}

type podmanMeasurementRequest struct {
	AllocID   string `json:"alloc_id"`
	Namespace string `json:"namespace"`
	JobID     string `json:"job_id"`
	TaskGroup string `json:"task_group"`
	TaskName  string `json:"task_name"`
	ImageRef  string `json:"image_ref"`
}

type podmanMeasurementResponse struct {
	MeasuredDigest string `json:"measured_digest"`
	Source         string `json:"source"`
	Reason         string `json:"reason"`
}

type podmanContainerInspect struct {
	ID          string                 `json:"Id"`
	Name        string                 `json:"Name"`
	Image       string                 `json:"Image"`
	ImageDigest string                 `json:"ImageDigest"`
	ImageName   string                 `json:"ImageName"`
	Config      *podmanContainerConfig `json:"Config"`
}

type podmanContainerConfig struct {
	Image  string            `json:"Image"`
	Labels map[string]string `json:"Labels"`
}

type podmanContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
}

func podmanMeasurerConfigFromEnv() (podmanMeasurerConfig, error) {
	cfg := podmanMeasurerConfig{
		socketPath: envString(podmanMeasureSocketEnv, ""),
		group:      envString(podmanMeasureGroupEnv, "nomad_observer"),
	}
	if cfg.socketPath == "" {
		return podmanMeasurerConfig{}, fmt.Errorf("%s must not be empty", podmanMeasureSocketEnv)
	}
	if err := validatePodmanMeasureSocketPath(cfg.socketPath); err != nil {
		return podmanMeasurerConfig{}, err
	}
	if cfg.group == "" {
		return podmanMeasurerConfig{}, fmt.Errorf("%s must not be empty", podmanMeasureGroupEnv)
	}
	return cfg, nil
}

func validatePodmanMeasureSocketPath(socketPath string) error {
	cleanPath := filepath.Clean(socketPath)
	if !filepath.IsAbs(cleanPath) {
		return fmt.Errorf("%s must be absolute", podmanMeasureSocketEnv)
	}
	const allowedDir = "/run/verself-nomad-observer"
	if filepath.Dir(cleanPath) != allowedDir {
		return fmt.Errorf("%s must be under %s", podmanMeasureSocketEnv, allowedDir)
	}
	return nil
}

func runPodmanMeasurer(ctx context.Context, cfg podmanMeasurerConfig) error {
	listener, err := podmanMeasurerListener(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(cfg.socketPath)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/measure", func(w http.ResponseWriter, req *http.Request) {
		servePodmanMeasurement(w, req)
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = server.Shutdown(shutdownCtx)
		cancel()
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return ctx.Err()
	}
	return err
}

func podmanMeasurerListener(cfg podmanMeasurerConfig) (net.Listener, error) {
	groupID, err := groupID(cfg.group)
	if err != nil {
		return nil, err
	}
	socketDir := filepath.Dir(cfg.socketPath)
	if err := os.MkdirAll(socketDir, 0o770); err != nil {
		return nil, fmt.Errorf("create podman measurement socket dir: %w", err)
	}
	if err := os.Chown(socketDir, 0, groupID); err != nil {
		return nil, fmt.Errorf("chown podman measurement socket dir: %w", err)
	}
	if err := os.Chmod(socketDir, 0o770); err != nil {
		return nil, fmt.Errorf("chmod podman measurement socket dir: %w", err)
	}
	if err := os.Remove(cfg.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale podman measurement socket: %w", err)
	}
	listener, err := net.Listen("unix", cfg.socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on podman measurement socket: %w", err)
	}
	if err := os.Chown(cfg.socketPath, 0, groupID); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chown podman measurement socket: %w", err)
	}
	if err := os.Chmod(cfg.socketPath, 0o660); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chmod podman measurement socket: %w", err)
	}
	return listener, nil
}

func groupID(name string) (int, error) {
	group, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", name, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse group %s gid %q: %w", name, group.Gid, err)
	}
	return gid, nil
}

func servePodmanMeasurement(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		_ = req.Body.Close()
	}()
	var measurementReq podmanMeasurementRequest
	decoder := json.NewDecoder(io.LimitReader(req.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&measurementReq); err != nil {
		http.Error(w, fmt.Sprintf("decode measurement request: %s", err), http.StatusBadRequest)
		return
	}
	if err := measurementReq.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	measurement := measurePodmanContainer(req.Context(), podmanBinary, measurementReq)
	response := podmanMeasurementResponse{
		MeasuredDigest: measurement.digest,
		Source:         measurement.source,
		Reason:         measurement.reason,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		return
	}
}

func (r podmanMeasurementRequest) validate() error {
	if strings.TrimSpace(r.AllocID) == "" {
		return errors.New("alloc_id must not be empty")
	}
	if strings.TrimSpace(r.Namespace) == "" {
		return errors.New("namespace must not be empty")
	}
	if strings.TrimSpace(r.JobID) == "" {
		return errors.New("job_id must not be empty")
	}
	if strings.TrimSpace(r.TaskGroup) == "" {
		return errors.New("task_group must not be empty")
	}
	if strings.TrimSpace(r.TaskName) == "" {
		return errors.New("task_name must not be empty")
	}
	if strings.TrimSpace(r.ImageRef) == "" {
		return errors.New("image_ref must not be empty")
	}
	return nil
}

func (o *observer) measureWorkloadOCIRuntime(ctx context.Context, alloc *api.Allocation, task allocationOCITask) workloadOCIRuntimeMeasurement {
	socketPath := strings.TrimSpace(o.cfg.podmanMeasureSocket)
	if socketPath == "" {
		return workloadOCIRuntimeMeasurement{
			source: "podman_measurement_socket",
			reason: "podman measurement socket is not configured",
		}
	}
	req := podmanMeasurementRequest{
		AllocID:   alloc.ID,
		Namespace: normalizeNamespace(alloc.Namespace, o.cfg.namespace),
		JobID:     alloc.JobID,
		TaskGroup: alloc.TaskGroup,
		TaskName:  task.name,
		ImageRef:  task.imageRef,
	}
	measurement, err := requestPodmanMeasurement(ctx, socketPath, req)
	if err != nil {
		return workloadOCIRuntimeMeasurement{
			source: "podman_measurement_socket",
			reason: err.Error(),
		}
	}
	return measurement
}

func requestPodmanMeasurement(ctx context.Context, socketPath string, measurementReq podmanMeasurementRequest) (workloadOCIRuntimeMeasurement, error) {
	body, err := json.Marshal(measurementReq)
	if err != nil {
		return workloadOCIRuntimeMeasurement{}, err
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://podman-measurer/measure", bytes.NewReader(body))
	if err != nil {
		return workloadOCIRuntimeMeasurement{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return workloadOCIRuntimeMeasurement{}, fmt.Errorf("request podman measurement: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return workloadOCIRuntimeMeasurement{}, fmt.Errorf("podman measurement failed: %s: %s", resp.Status, strings.TrimSpace(string(errorBody)))
	}
	var measurementResp podmanMeasurementResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&measurementResp); err != nil {
		return workloadOCIRuntimeMeasurement{}, fmt.Errorf("decode podman measurement: %w", err)
	}
	return workloadOCIRuntimeMeasurement{
		digest: measurementResp.MeasuredDigest,
		source: measurementResp.Source,
		reason: measurementResp.Reason,
	}, nil
}

func measurePodmanContainer(ctx context.Context, podmanPath string, measurementReq podmanMeasurementRequest) workloadOCIRuntimeMeasurement {
	inspect, err := inspectPodmanContainer(ctx, podmanPath, podmanContainerName(measurementReq.AllocID, measurementReq.TaskName))
	if err != nil {
		inspect, err = inspectPodmanContainerByLabels(ctx, podmanPath, measurementReq)
		if err != nil {
			return workloadOCIRuntimeMeasurement{
				source: "podman_container_inspect",
				reason: err.Error(),
			}
		}
	}
	source, reason := podmanMeasurementIdentitySource(measurementReq, inspect)
	if reason != "" {
		return workloadOCIRuntimeMeasurement{
			source: source,
			reason: reason,
		}
	}
	digest, err := podmanMeasuredDigest(inspect)
	if err != nil {
		return workloadOCIRuntimeMeasurement{
			source: source,
			reason: err.Error(),
		}
	}
	return workloadOCIRuntimeMeasurement{
		digest: digest,
		source: source,
	}
}

func inspectPodmanContainer(ctx context.Context, podmanPath, container string) (podmanContainerInspect, error) {
	var inspected []podmanContainerInspect
	if err := podmanJSON(ctx, podmanPath, []string{"container", "inspect", "--format=json", container}, &inspected); err != nil {
		return podmanContainerInspect{}, err
	}
	if len(inspected) != 1 {
		return podmanContainerInspect{}, fmt.Errorf("expected one podman container for %s, got %d", container, len(inspected))
	}
	return inspected[0], nil
}

func inspectPodmanContainerByLabels(ctx context.Context, podmanPath string, measurementReq podmanMeasurementRequest) (podmanContainerInspect, error) {
	var summaries []podmanContainerSummary
	if err := podmanJSON(ctx, podmanPath, []string{"ps", "--all", "--format", "json"}, &summaries); err != nil {
		return podmanContainerInspect{}, err
	}
	matches := make([]podmanContainerSummary, 0, 1)
	for _, summary := range summaries {
		if summary.Labels[labelNomadAllocID] == measurementReq.AllocID && summary.Labels[labelNomadTaskName] == measurementReq.TaskName {
			matches = append(matches, summary)
		}
	}
	if len(matches) == 0 {
		return podmanContainerInspect{}, fmt.Errorf("podman container %s not found by name or nomad labels", podmanContainerName(measurementReq.AllocID, measurementReq.TaskName))
	}
	if len(matches) > 1 {
		return podmanContainerInspect{}, fmt.Errorf("podman labels matched %d containers for alloc %s task %s", len(matches), measurementReq.AllocID, measurementReq.TaskName)
	}
	containerID := strings.TrimSpace(matches[0].ID)
	if containerID == "" {
		return podmanContainerInspect{}, fmt.Errorf("podman label match for alloc %s task %s had no container id", measurementReq.AllocID, measurementReq.TaskName)
	}
	return inspectPodmanContainer(ctx, podmanPath, containerID)
}

func podmanJSON(ctx context.Context, podmanPath string, args []string, output any) error {
	commandCtx, cancel := context.WithTimeout(ctx, podmanCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, podmanPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if commandCtx.Err() != nil {
		return fmt.Errorf("%s %s timed out", podmanPath, strings.Join(args, " "))
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", podmanPath, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode %s %s: %w", podmanPath, strings.Join(args, " "), err)
	}
	return nil
}

func podmanMeasurementIdentitySource(measurementReq podmanMeasurementRequest, inspect podmanContainerInspect) (string, string) {
	labels := map[string]string{}
	if inspect.Config != nil && inspect.Config.Labels != nil {
		labels = inspect.Config.Labels
	}
	if labels[labelNomadAllocID] != "" || labels[labelNomadTaskName] != "" {
		if labels[labelNomadAllocID] != measurementReq.AllocID {
			return "podman_container_inspect_labels", fmt.Sprintf("nomad alloc label %q does not match allocation %q", labels[labelNomadAllocID], measurementReq.AllocID)
		}
		if labels[labelNomadTaskName] != measurementReq.TaskName {
			return "podman_container_inspect_labels", fmt.Sprintf("nomad task label %q does not match task %q", labels[labelNomadTaskName], measurementReq.TaskName)
		}
		if got := labels[labelNomadNamespace]; got != "" && got != measurementReq.Namespace {
			return "podman_container_inspect_labels", fmt.Sprintf("nomad namespace label %q does not match namespace %q", got, measurementReq.Namespace)
		}
		if got := labels[labelNomadJobID]; got != "" && got != measurementReq.JobID {
			return "podman_container_inspect_labels", fmt.Sprintf("nomad job label %q does not match job %q", got, measurementReq.JobID)
		}
		if got := labels[labelNomadTaskGroup]; got != "" && got != measurementReq.TaskGroup {
			return "podman_container_inspect_labels", fmt.Sprintf("nomad task group label %q does not match task group %q", got, measurementReq.TaskGroup)
		}
		return "podman_container_inspect_labels", ""
	}
	if inspect.Name == podmanContainerName(measurementReq.AllocID, measurementReq.TaskName) {
		return "podman_container_inspect_name", ""
	}
	return "podman_container_inspect_name", fmt.Sprintf("podman container %q does not match expected nomad container %q", inspect.Name, podmanContainerName(measurementReq.AllocID, measurementReq.TaskName))
}

func podmanMeasuredDigest(inspect podmanContainerInspect) (string, error) {
	for _, candidate := range []string{inspect.ImageDigest, inspect.ImageName, inspectImageName(inspect)} {
		digest, err := digestFromImageReference(candidate)
		if err == nil {
			return digest, nil
		}
	}
	return "", fmt.Errorf("podman container %s has no measured image digest", firstNonEmpty(inspect.ID, inspect.Name))
}

func inspectImageName(inspect podmanContainerInspect) string {
	if inspect.Config == nil {
		return ""
	}
	return inspect.Config.Image
}

func digestFromImageReference(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("image reference is empty")
	}
	if parsed, err := ocidigest.Parse(value); err == nil {
		return parsed.String(), nil
	}
	_, after, ok := strings.Cut(value, "@")
	if !ok {
		return "", fmt.Errorf("image reference %q is not pinned by digest", value)
	}
	parsed, err := ocidigest.Parse(after)
	if err != nil {
		return "", fmt.Errorf("image reference digest is invalid: %w", err)
	}
	return parsed.String(), nil
}

func podmanContainerName(allocID, taskName string) string {
	return taskName + "-" + allocID
}
