job "vm-orchestrator" {
  name = "vm-orchestrator"
  datacenters = ["dc1"]
  type = "service"

  group "daemon" {
    count = 1

    task "stage-guest-images" {
      driver = "raw_exec"
      user = "root"

      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      artifact {
        source = "verself-artifact://vm-orchestrator-cli"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://vm-orchestrator-substrate-inputs"
        destination = "local/substrate-inputs"
        chown = true
      }

      artifact {
        source = "verself-artifact://vm-orchestrator-toolchain-images"
        destination = "local/toolchain-images"
        chown = true
      }

      config {
        command = "local/bin/vm-orchestrator-cli"
        args = [
          "stage-guest-images",
          "--substrate-inputs", "local/substrate-inputs",
          "--toolchain-images", "local/toolchain-images",
          "--guest-images-dir", "/var/lib/verself/guest-images",
          "--work-dir", "/tmp/verself-substrate-build",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "vm-orchestrator-stage"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 8000
        # Nomad reserves lifecycle-task memory for the whole allocation, even after prestart exits.
        memory = 128
      }
    }

    task "vm-orchestrator" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "75m"

      artifact {
        source = "verself-artifact://vm-orchestrator"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/vm-orchestrator"
        args = [
          "--listen-unix", "/run/vm-orchestrator/api.sock",
          "--socket-group", "vm-clients",
          "--pool", "vspool",
          "--image-dataset", "images",
          "--golden-dataset", "goldens",
          "--workload-dataset", "workloads",
          "--storage-key-dir", "/var/lib/verself/vm-orchestrator/storage-keys",
          "--default-substrate-ref", "substrate",
          "--kernel-path", "/var/lib/verself/guest-images/vmlinux",
          "--firecracker-bin", "/usr/local/bin/firecracker",
          "--jailer-bin", "/usr/local/bin/jailer",
          "--jailer-root", "/vspool/checkpoints/jailer",
          "--snapshot-cache-dir", "/vspool/checkpoints/firecracker-snapshot-cache",
          "--firecracker-snapshots", "true",
          "--jailer-uid", "10000",
          "--jailer-gid", "10000",
          "--guest-pool-cidr", "172.16.0.0/16",
          "--state-db-path", "/var/lib/verself/vm-orchestrator/state.db",
          "--host-service-ip", "10.255.0.1",
          "--host-service-port", "18080",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "vm-orchestrator"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        # Firecracker VMM children inherit this task cgroup, so this reservation
        # must cover guest RAM, not just the Go daemon.
        cpu = 24000
        memory = 53248
      }

      restart {
        attempts = 10
        delay = "2s"
        interval = "300s"
        mode = "delay"
      }
    }

    task "seed-images" {
      driver = "raw_exec"
      user = "root"

      lifecycle {
        hook = "poststart"
        sidecar = false
      }

      artifact {
        source = "verself-artifact://vm-orchestrator-cli"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/vm-orchestrator-cli"
        args = [
          "seed-catalog",
          "--socket", "/run/vm-orchestrator/api.sock",
          "--catalog", "local/seed-catalog.json",
          "--allow-destroying-active-clones",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "vm-orchestrator-seed"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 100
        memory = 10
      }

      template {
        change_mode = "restart"
        destination = "local/seed-catalog.json"
        data = <<-EOT
{
  "images": [
    {
      "ref": "substrate",
      "strategy": "dd_from_file",
      "source_path": "/var/lib/verself/guest-images/substrate.ext4",
      "size_bytes": 2147483648,
      "volblocksize": "16K"
    },
    {
      "ref": "gh-actions-runner",
      "strategy": "dd_from_file",
      "source_path": "/var/lib/verself/guest-images/toolchains/gh-actions-runner.ext4",
      "size_bytes": 1073741824,
      "volblocksize": "16K"
    }
  ]
}
EOT
      }
    }

    update {
      max_parallel = 1
      health_check = "task_states"
      min_healthy_time = "5s"
      healthy_deadline = "900s"
      progress_deadline = "75m"
      auto_revert = true
    }
  }
}
