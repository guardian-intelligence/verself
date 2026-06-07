variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "temporal_resource_name" {
  type    = string
  default = "temporal"
}

job "temporal" {
  name        = "temporal"
  datacenters = ["*"]
  type        = "service"

  group "temporal" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "frontend_grpc" {
        host_network = "loopback"
      }
      port "frontend_http" {
        host_network = "loopback"
      }
      port "frontend_membership" {
        host_network = "loopback"
      }
      port "history_grpc" {
        host_network = "loopback"
      }
      port "history_membership" {
        host_network = "loopback"
      }
      port "internal_frontend_grpc" {
        host_network = "loopback"
      }
      port "internal_frontend_http" {
        host_network = "loopback"
      }
      port "internal_membership" {
        host_network = "loopback"
      }
      port "matching_grpc" {
        host_network = "loopback"
      }
      port "matching_membership" {
        host_network = "loopback"
      }
      port "metrics" {
        host_network = "loopback"
      }
      port "pprof" {
        host_network = "loopback"
      }
      port "worker_grpc" {
        host_network = "loopback"
      }
      port "worker_membership" {
        host_network = "loopback"
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      restart {
        attempts = 3
        delay    = "10s"
        interval = "120s"
        mode     = "delay"
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/temporal-platform/cmd/temporal-recover/temporal-recover_/temporal-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.temporal_resource_name}",
        ]
      }

      resources {
        cpu    = 150
        memory = 256
      }
    }

    task "temporal-server" {
      driver       = "raw_exec"
      user         = "temporal_server"
      kill_signal  = "SIGTERM"
      kill_timeout = "30s"

      config {
        command = "/var/lib/temporal/runtime/current/bin/verself-temporal-server"
        args = [
          "--config=local/config.yaml",
          "--auth-config=/etc/temporal/auth.json",
          "--schema-binary=/var/lib/temporal/runtime/current/bin/temporal-schema",
          "--spiffe-socket=unix:///run/spire-agent/sockets/agent.sock",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES    = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME           = "temporal-server"
        VERSELF_SUPERVISOR          = "nomad"
      }

      resources {
        cpu    = 1000
        memory = 2048
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "temporal-frontend-grpc"
        port         = "frontend_grpc"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "temporal-frontend-grpc-tcp"
          type     = "tcp"
          port     = "frontend_grpc"
          interval = "1s"
          timeout  = "3s"
        }
      }

      service {
        name         = "temporal-metrics"
        port         = "metrics"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "temporal-metrics-http"
          type     = "http"
          path     = "/metrics"
          port     = "metrics"
          interval = "1s"
          timeout  = "3s"
        }
      }

      template {
        change_mode = "restart"
        destination = "local/config.yaml"
        data = <<-EOT
log:
  stdout: true
  level: info
  format: json

persistence:
  defaultStore: postgres-default
  visibilityStore: postgres-visibility
  numHistoryShards: 4
  datastores:
    postgres-default:
      sql:
        pluginName: postgres12
        databaseName: temporal
        connectAddr: /var/run/postgresql
        connectProtocol: unix
        user: temporal
        maxConns: 20
        maxIdleConns: 20
        maxConnLifetime: 1h
        connectAttributes:
          sslmode: disable
          application_name: temporal-server
          connect_timeout: "5"
    postgres-visibility:
      sql:
        pluginName: postgres12
        databaseName: temporal_visibility
        connectAddr: /var/run/postgresql
        connectProtocol: unix
        user: temporal
        maxConns: 10
        maxIdleConns: 10
        maxConnLifetime: 1h
        connectAttributes:
          sslmode: disable
          application_name: temporal-server
          connect_timeout: "5"

global:
  membership:
    maxJoinDuration: 30s
    broadcastAddress: 127.0.0.1
  pprof:
    port: {{ env "NOMAD_PORT_pprof" }}
  metrics:
    prometheus:
      framework: opentelemetry
      timerType: histogram
      listenAddress: 127.0.0.1:{{ env "NOMAD_PORT_metrics" }}

services:
  frontend:
    rpc:
      grpcPort: {{ env "NOMAD_PORT_frontend_grpc" }}
      membershipPort: {{ env "NOMAD_PORT_frontend_membership" }}
      bindOnIP: 127.0.0.1
      httpPort: {{ env "NOMAD_PORT_frontend_http" }}
  internal-frontend:
    rpc:
      grpcPort: {{ env "NOMAD_PORT_internal_frontend_grpc" }}
      membershipPort: {{ env "NOMAD_PORT_internal_membership" }}
      bindOnIP: 127.0.0.1
      httpPort: {{ env "NOMAD_PORT_internal_frontend_http" }}
  history:
    rpc:
      grpcPort: {{ env "NOMAD_PORT_history_grpc" }}
      membershipPort: {{ env "NOMAD_PORT_history_membership" }}
      bindOnIP: 127.0.0.1
  matching:
    rpc:
      grpcPort: {{ env "NOMAD_PORT_matching_grpc" }}
      membershipPort: {{ env "NOMAD_PORT_matching_membership" }}
      bindOnIP: 127.0.0.1
  worker:
    rpc:
      grpcPort: {{ env "NOMAD_PORT_worker_grpc" }}
      membershipPort: {{ env "NOMAD_PORT_worker_membership" }}
      bindOnIP: 127.0.0.1

clusterMetadata:
  enableGlobalNamespace: false
  failoverVersionIncrement: 10
  masterClusterName: active
  currentClusterName: active
  clusterInformation:
    active:
      enabled: true
      initialFailoverVersion: 1
      rpcName: frontend
      rpcAddress: 127.0.0.1:{{ env "NOMAD_PORT_frontend_grpc" }}
      httpAddress: 127.0.0.1:{{ env "NOMAD_PORT_frontend_http" }}

dcRedirectionPolicy:
  policy: noop

archival:
  history:
    state: disabled
    enableRead: false
  visibility:
    state: disabled
    enableRead: false

namespaceDefaults:
  archival:
    history:
      state: disabled
    visibility:
      state: disabled

dynamicConfigClient:
  filepath: {{ env "NOMAD_TASK_DIR" }}/dynamicconfig.yaml
  pollInterval: 60s
EOT
      }

      template {
        change_mode = "restart"
        destination = "local/dynamicconfig.yaml"
        data = <<-EOT
limit.maxIDLength:
  - value: 255
    constraints: {}
EOT
      }
    }

    task "temporal-bootstrap" {
      driver = "raw_exec"
      user   = "temporal_server"

      lifecycle {
        hook    = "poststart"
        sidecar = false
      }

      config {
        command = "/var/lib/temporal/runtime/current/bin/temporal-bootstrap"
        args = [
          "--frontend-address=127.0.0.1:$${NOMAD_PORT_frontend_grpc}",
          "--resource-graph=/run/verself/recovery/temporal/document.json",
          "--resource-name=${var.temporal_resource_name}",
          "--spiffe-socket=unix:///run/spire-agent/sockets/agent.sock",
          "--attempts=30",
          "--retry-delay=1s",
        ]
      }

      env {
        HOME                       = "/tmp"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES   = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME          = "temporal-bootstrap"
        VERSELF_SUPERVISOR         = "nomad"
      }

      resources {
        cpu    = 200
        memory = 512
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
