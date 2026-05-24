package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	opruntime "github.com/verself/operator-runtime/runtime"
)

// Nomad's HTTP API on the worker. We dial through the operator SSH session
// rather than opening a forwarded local listener for one-shot service lookups.
const nomadHTTPRemoteAddr = "127.0.0.1:4646"

// errNomadServiceNotRegistered means Nomad's API responded successfully but
// returned no entries. The service either isn't deployed or its allocations are
// unhealthy.
var errNomadServiceNotRegistered = errors.New("nomad service not registered")

type nomadServiceEntry struct {
	Address string `json:"Address"`
	Port    int    `json:"Port"`
}

// lookupNomadService resolves a Nomad service name to a host:port using the
// Nomad HTTP API at 127.0.0.1:4646, dialed through the operator SSH session.
// Returns the first registered allocation; multi-allocation services are
// fronted by HAProxy in production, so the operator shortcut picking one is
// sufficient for targeted host-side commands.
func lookupNomadService(ctx context.Context, ssh *opruntime.SSHClient, service string) (string, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return ssh.DialContext(ctx, "tcp", nomadHTTPRemoteAddr)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}

	url := "http://" + nomadHTTPRemoteAddr + "/v1/service/" + service
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build nomad request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("query nomad: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nomad %s returned %d", service, resp.StatusCode)
	}
	var entries []nomadServiceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return "", fmt.Errorf("decode nomad response: %w", err)
	}
	if len(entries) == 0 {
		return "", errNomadServiceNotRegistered
	}
	entry := entries[0]
	if entry.Address == "" || entry.Port == 0 {
		return "", fmt.Errorf("nomad %s entry missing address/port", service)
	}
	return net.JoinHostPort(entry.Address, strconv.Itoa(entry.Port)), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
