package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

const (
	spiceDBBinary = "local/bin/spicedb"
	keyPath       = "/etc/credstore/spicedb/grpc-preshared-key"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "spicedb-node-runner: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	grpcPort, err := requiredEnv("NOMAD_PORT_grpc")
	if err != nil {
		return err
	}
	metricsPort, err := requiredEnv("NOMAD_PORT_metrics")
	if err != nil {
		return err
	}
	keyBody, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read SpiceDB preshared key: %w", err)
	}
	key := strings.TrimSpace(string(keyBody))
	if key == "" {
		return fmt.Errorf("SpiceDB preshared key is empty")
	}
	env := append(os.Environ(), "SPICEDB_GRPC_PRESHARED_KEY="+key)
	args := []string{
		spiceDBBinary,
		"serve",
		"--datastore-engine=postgres",
		"--datastore-conn-uri=postgres://spicedb@/spicedb?host=/var/run/postgresql&sslmode=disable&application_name=spicedb",
		"--datastore-conn-pool-read-max-open=8",
		"--datastore-conn-pool-read-min-open=1",
		"--datastore-conn-pool-write-max-open=4",
		"--datastore-conn-pool-write-min-open=1",
		"--grpc-addr=127.0.0.1:" + grpcPort,
		"--metrics-addr=127.0.0.1:" + metricsPort,
		"--http-enabled=false",
		"--telemetry-endpoint=",
		"--skip-release-check",
		"--log-format=json",
	}
	return syscall.Exec(spiceDBBinary, args, env)
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}
