variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "spicedb_resource_name" {
  type    = string
  default = "spicedb"
}

job "spicedb" {
  name        = "spicedb"
  datacenters = ["*"]
  type        = "service"

  group "spicedb" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "grpc" {
        host_network = "loopback"
      }

      port "metrics" {
        host_network = "loopback"
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      vault {
        env  = false
        role = "spicedb-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      restart {
        attempts = 3
        delay    = "10s"
        interval = "120s"
        mode     = "delay"
      }

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/spicedb/cmd/spicedb-recover/spicedb-recover_/spicedb-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.spicedb_resource_name}",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }

    task "server" {
      driver       = "raw_exec"
      user         = "root"
      kill_signal  = "SIGTERM"
      kill_timeout = "30s"

      vault {
        env  = false
        role = "spicedb-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      config {
        command = "/var/lib/spicedb/runtime/current/bin/spicedb-recover"
        args = [
          "server",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.spicedb_resource_name}",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
          "--grpc-port=$${NOMAD_PORT_grpc}",
          "--metrics-port=$${NOMAD_PORT_metrics}",
        ]
      }

      resources {
        cpu    = 500
        memory = 1024
      }

      service {
        name         = "spicedb-grpc"
        port         = "grpc"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "spicedb-grpc-tcp"
          type     = "tcp"
          port     = "grpc"
          interval = "1s"
          timeout  = "3s"
        }
      }

      service {
        name         = "spicedb-metrics"
        port         = "metrics"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "spicedb-metrics-http"
          type     = "http"
          path     = "/metrics"
          port     = "metrics"
          interval = "1s"
          timeout  = "3s"
        }
      }
    }

    update {
      max_parallel      = 1
      health_check      = "checks"
      min_healthy_time  = "3s"
      healthy_deadline  = "300s"
      progress_deadline = "600s"
      canary            = 1
      auto_revert       = true
      auto_promote      = true
    }
  }
}
