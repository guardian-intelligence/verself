package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
)

const (
	otelcolUser  = "otelcol"
	otelcolGroup = "otelcol"
	dataDir      = "/var/lib/otelcol"
	storageDir   = "/var/lib/otelcol/storage"
	spiffeDir    = "/var/lib/otelcol/clickhouse-spiffe"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "otelcol-node-runner: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected exactly one mode")
	}
	switch args[0] {
	case "prepare":
		return prepare()
	default:
		return fmt.Errorf("unknown mode %q", args[0])
	}
}

func prepare() error {
	uid, gid, err := otelcolIDs()
	if err != nil {
		return err
	}
	for _, dir := range []struct {
		path string
		mode os.FileMode
	}{
		{path: dataDir, mode: 0o750},
		{path: storageDir, mode: 0o750},
		{path: spiffeDir, mode: 0o700},
	} {
		if err := os.MkdirAll(dir.path, dir.mode); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir.path, err)
		}
		if err := os.Chown(dir.path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", dir.path, err)
		}
		if err := os.Chmod(dir.path, dir.mode); err != nil {
			return fmt.Errorf("chmod %s: %w", dir.path, err)
		}
	}
	return nil
}

func otelcolIDs() (int, int, error) {
	u, err := user.Lookup(otelcolUser)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup user %s: %w", otelcolUser, err)
	}
	g, err := user.LookupGroup(otelcolGroup)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %s: %w", otelcolGroup, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid for %s: %w", otelcolUser, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid for %s: %w", otelcolGroup, err)
	}
	return uid, gid, nil
}
