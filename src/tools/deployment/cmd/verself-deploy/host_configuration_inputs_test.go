package main

import (
	"slices"
	"testing"
)

func TestHostConfigurationSkipTags(t *testing.T) {
	tests := []struct {
		name         string
		changedPaths []string
		want         []string
	}{
		{
			name:         "grafana role skips guest image and firecracker work",
			changedPaths: []string{"src/host-configuration/components/grafana/vars/main.yml"},
			want:         []string{"guest_rootfs", "firecracker"},
		},
		{
			name:         "vm guest telemetry keeps guest image and firecracker work",
			changedPaths: []string{"src/substrate/vm-guest-telemetry/src/guest.zig"},
			want:         nil,
		},
		{
			name:         "vm bridge keeps guest image and firecracker work",
			changedPaths: []string{"src/substrate/vm-orchestrator/cmd/vm-bridge/main.go"},
			want:         nil,
		},
		{
			name:         "vm orchestrator daemon skips guest image build but keeps firecracker work",
			changedPaths: []string{"src/substrate/vm-orchestrator/server.go"},
			want:         []string{"guest_rootfs"},
		},
		{
			name:         "firecracker role skips only guest image build",
			changedPaths: []string{"src/host-configuration/ansible/roles/firecracker/tasks/main.yml"},
			want:         []string{"guest_rootfs"},
		},
		{
			name:         "ops topology skips only guest image build",
			changedPaths: []string{"src/host-configuration/ansible/group_vars/all/topology/ops.yml"},
			want:         []string{"guest_rootfs"},
		},
		{
			name:         "guest rootfs role keeps guest image and firecracker work",
			changedPaths: []string{"src/host-configuration/ansible/roles/guest_rootfs/tasks/main.yml"},
			want:         nil,
		},
		{
			name:         "module changes keep full host configuration work",
			changedPaths: []string{"MODULE.bazel.lock"},
			want:         nil,
		},
		{
			name:         "catalog changes keep full host configuration work",
			changedPaths: []string{"src/host-configuration/ansible/group_vars/all/catalog.yml"},
			want:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostConfigurationSkipTags(tt.changedPaths)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("hostConfigurationSkipTags(%v) = %v, want %v", tt.changedPaths, got, tt.want)
			}
		})
	}
}

func TestHostConfigurationAnsibleArgs(t *testing.T) {
	got := hostConfigurationAnsibleArgs(hostConfigurationDecision{
		SkipTags: []string{"guest_rootfs", "firecracker"},
	})
	want := []string{"--skip-tags=guest_rootfs,firecracker"}
	if !slices.Equal(got, want) {
		t.Fatalf("hostConfigurationAnsibleArgs() = %v, want %v", got, want)
	}
}

func TestHostConfigurationPathRequiresAnsible(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "component role task is ansible input",
			path: "src/host-configuration/components/spicedb/tasks/main.yml",
			want: true,
		},
		{
			name: "component nftables file is ansible input",
			path: "src/host-configuration/components/spicedb/files/spicedb.nft",
			want: true,
		},
		{
			name: "clickhouse migration is ansible input",
			path: "src/host-configuration/components/clickhouse/migrations/012_nomad_job_events.up.sql",
			want: true,
		},
		{
			name: "component nomad job is not ansible input",
			path: "src/host-configuration/components/spicedb/nomad.json",
			want: false,
		},
		{
			name: "platform component go code is not ansible input",
			path: "src/host-configuration/components/temporal-platform/cmd/verself-temporal-server/main.go",
			want: false,
		},
		{
			name: "platform component build file is not ansible input",
			path: "src/host-configuration/components/temporal-platform/BUILD.bazel",
			want: false,
		},
		{
			name: "shared ansible role remains ansible input",
			path: "src/host-configuration/ansible/roles/firecracker/tasks/main.yml",
			want: true,
		},
		{
			name: "host readme is not ansible input",
			path: "src/host-configuration/README.md",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostConfigurationPathRequiresAnsible(tt.path)
			if got != tt.want {
				t.Fatalf("hostConfigurationPathRequiresAnsible(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
