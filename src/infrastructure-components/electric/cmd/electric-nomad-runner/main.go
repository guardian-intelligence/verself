package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const shutdownGrace = 25 * time.Second

type config struct {
	ctrBin              string
	containerdAddress   string
	image               string
	envFile             string
	pgUser              string
	pgPasswordFile      string
	pgDatabase          string
	electricSecretFile  string
	storageDir          string
	instanceID          string
	replicationStreamID string
	dbPoolSize          string
	portEnv             string
	runcBinary          string
	containerID         string
}

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "electric-nomad-runner: %v\n", err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "electric-nomad-runner: %v\n", err)
		os.Exit(1)
	}
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("electric-nomad-runner", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	cfg := config{}
	fs.StringVar(&cfg.ctrBin, "ctr", "", "absolute path to ctr")
	fs.StringVar(&cfg.containerdAddress, "containerd-address", "", "containerd socket address")
	fs.StringVar(&cfg.image, "image", "", "container image reference")
	fs.StringVar(&cfg.envFile, "env-file", "", "runtime env file containing Electric secrets")
	fs.StringVar(&cfg.pgUser, "pg-user", "", "PostgreSQL replication role")
	fs.StringVar(&cfg.pgPasswordFile, "pg-password-file", "", "path to the PostgreSQL password credential")
	fs.StringVar(&cfg.pgDatabase, "pg-database", "", "PostgreSQL database Electric should replicate")
	fs.StringVar(&cfg.electricSecretFile, "electric-secret-file", "", "path to the Electric API secret credential")
	fs.StringVar(&cfg.storageDir, "storage-dir", "", "host storage directory mounted at /data")
	fs.StringVar(&cfg.instanceID, "instance-id", "", "stable Electric instance id")
	fs.StringVar(&cfg.replicationStreamID, "replication-stream-id", "", "stable Electric replication stream id")
	fs.StringVar(&cfg.dbPoolSize, "db-pool-size", "", "Electric database pool size")
	fs.StringVar(&cfg.portEnv, "port-env", "NOMAD_PORT_http", "environment variable containing the allocated HTTP port")
	fs.StringVar(&cfg.runcBinary, "runc-binary", "", "runc-compatible runtime binary")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if !filepath.IsAbs(cfg.storageDir) {
		return config{}, errors.New("--storage-dir must be absolute")
	}

	for name, value := range map[string]string{
		"ctr":                   cfg.ctrBin,
		"containerd-address":    cfg.containerdAddress,
		"image":                 cfg.image,
		"env-file":              cfg.envFile,
		"pg-user":               cfg.pgUser,
		"pg-password-file":      cfg.pgPasswordFile,
		"pg-database":           cfg.pgDatabase,
		"electric-secret-file":  cfg.electricSecretFile,
		"storage-dir":           cfg.storageDir,
		"instance-id":           cfg.instanceID,
		"replication-stream-id": cfg.replicationStreamID,
		"db-pool-size":          cfg.dbPoolSize,
		"port-env":              cfg.portEnv,
		"runc-binary":           cfg.runcBinary,
	} {
		if strings.TrimSpace(value) == "" {
			return config{}, fmt.Errorf("--%s is required", name)
		}
	}
	allocID := strings.TrimSpace(os.Getenv("NOMAD_ALLOC_ID"))
	if allocID == "" {
		return config{}, errors.New("NOMAD_ALLOC_ID is required")
	}
	if err := absolutizeExecutable(&cfg.ctrBin, "ctr"); err != nil {
		return config{}, err
	}
	if err := absolutizeExecutable(&cfg.runcBinary, "runc-binary"); err != nil {
		return config{}, err
	}
	cfg.containerID = cfg.instanceID + "-" + allocID
	return cfg, nil
}

func absolutizeExecutable(path *string, flagName string) error {
	if filepath.IsAbs(*path) {
		return nil
	}
	absPath, err := filepath.Abs(*path)
	if err != nil {
		return fmt.Errorf("resolve --%s: %w", flagName, err)
	}
	*path = absPath
	return nil
}

func run(cfg config) error {
	port := strings.TrimSpace(os.Getenv(cfg.portEnv))
	if port == "" {
		return fmt.Errorf("%s is required", cfg.portEnv)
	}
	if err := os.MkdirAll(cfg.storageDir, 0o750); err != nil {
		return fmt.Errorf("create storage dir %s: %w", cfg.storageDir, err)
	}
	if err := writeEnvFile(cfg); err != nil {
		return err
	}

	_ = cleanup(cfg)

	cmd := exec.Command(cfg.ctrBin, ctrArgs(cfg, ctrRunArgs(cfg, port)...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ctr run: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case err := <-done:
		cleanupErr := cleanup(cfg)
		if err != nil {
			return err
		}
		return cleanupErr
	case sig := <-signals:
		if err := terminate(cfg, cmd, done); err != nil {
			return fmt.Errorf("%s: %w", sig, err)
		}
		return nil
	}
}

func writeEnvFile(cfg config) error {
	pgPassword, err := readCredential(cfg.pgPasswordFile)
	if err != nil {
		return fmt.Errorf("read PostgreSQL password: %w", err)
	}
	electricSecret, err := readCredential(cfg.electricSecretFile)
	if err != nil {
		return fmt.Errorf("read Electric secret: %w", err)
	}
	databaseURL := url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(cfg.pgUser, pgPassword),
		Host:   "127.0.0.1:5432",
		Path:   "/" + cfg.pgDatabase,
	}
	if err := os.MkdirAll(filepath.Dir(cfg.envFile), 0o700); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}
	body := "DATABASE_URL=" + databaseURL.String() + "\nELECTRIC_SECRET=" + electricSecret + "\n"
	if err := os.WriteFile(cfg.envFile, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write Electric env file: %w", err)
	}
	return nil
}

func readCredential(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return value, nil
}

func ctrRunArgs(cfg config, port string) []string {
	return []string{
		"run",
		"--rm",
		"--net-host",
		"--runtime", "io.containerd.runc.v2",
		"--runc-binary", cfg.runcBinary,
		"--env-file", cfg.envFile,
		"--env", "ELECTRIC_PORT=" + port,
		"--env", "ELECTRIC_STORAGE_DIR=/data",
		"--env", "ELECTRIC_DB_POOL_SIZE=" + cfg.dbPoolSize,
		"--env", "ELECTRIC_INSTANCE_ID=" + cfg.instanceID,
		"--env", "ELECTRIC_REPLICATION_STREAM_ID=" + cfg.replicationStreamID,
		"--env", "ELECTRIC_MANUAL_TABLE_PUBLISHING=true",
		"--env", "RELEASE_NAME=" + cfg.instanceID + "@127.0.0.1",
		"--env", "RELEASE_DISTRIBUTION=none",
		"--mount", "type=bind,src=" + cfg.storageDir + ",dst=/data,options=rbind:rw",
		cfg.image,
		cfg.containerID,
	}
}

func terminate(cfg config, cmd *exec.Cmd, done <-chan error) error {
	_ = ctr(cfg, "tasks", "kill", "--signal", "SIGTERM", cfg.containerID)
	timer := time.NewTimer(shutdownGrace)
	defer timer.Stop()

	select {
	case <-done:
	case <-timer.C:
		_ = ctr(cfg, "tasks", "kill", "--signal", "SIGKILL", cfg.containerID)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
	}
	return cleanup(cfg)
}

func cleanup(cfg config) error {
	taskErr := ctr(cfg, "tasks", "delete", "--force", cfg.containerID)
	containerErr := ctr(cfg, "containers", "delete", cfg.containerID)
	if taskErr != nil && containerErr != nil {
		return fmt.Errorf("cleanup task: %v; cleanup container: %w", taskErr, containerErr)
	}
	if taskErr != nil {
		return taskErr
	}
	return containerErr
}

func ctr(cfg config, args ...string) error {
	cmd := exec.Command(cfg.ctrBin, ctrArgs(cfg, args...)...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		if len(output) > 0 {
			_, _ = os.Stdout.Write(output)
		}
		return nil
	}
	if isMissingContainerOutput(output) {
		return nil
	}
	if len(output) > 0 {
		_, _ = os.Stderr.Write(output)
	}
	return fmt.Errorf("ctr %s: %w", strings.Join(args, " "), err)
}

func ctrArgs(cfg config, args ...string) []string {
	if cfg.containerdAddress == "" {
		return args
	}
	out := []string{"--address", cfg.containerdAddress}
	out = append(out, args...)
	return out
}

func isMissingContainerOutput(output []byte) bool {
	lower := strings.ToLower(string(output))
	return strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist")
}
