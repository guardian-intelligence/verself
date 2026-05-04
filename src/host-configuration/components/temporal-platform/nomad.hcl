job "temporal" {
  name = "temporal"
  datacenters = ["dc1"]
  type = "service"
  group "temporal" {
    count = 1
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
    task "temporal-schema" {
      driver = "raw_exec"
      user = "temporal_server"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      config {
        args = ["-ec", "/opt/verself/profile/bin/temporal-schema setup --config \"$VERSELF_TEMPORAL_CONFIG_PATH\" --store postgres-default --version 0.0\n/opt/verself/profile/bin/temporal-schema update --config \"$VERSELF_TEMPORAL_CONFIG_PATH\" --store postgres-default --schema-name postgresql/v12/temporal\n/opt/verself/profile/bin/temporal-schema setup --config \"$VERSELF_TEMPORAL_CONFIG_PATH\" --store postgres-visibility --version 0.0\n/opt/verself/profile/bin/temporal-schema update --config \"$VERSELF_TEMPORAL_CONFIG_PATH\" --store postgres-visibility --schema-name postgresql/v12/visibility\n"]
        command = "/bin/sh"
      }
      env {
        HOME = "/tmp"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "temporal-schema"
        VERSELF_SUPERVISOR = "nomad"
        VERSELF_TEMPORAL_CONFIG_PATH = "$${NOMAD_TASK_DIR}/config.yaml"
      }
      resources {
        cpu = 200
        memory = 512
      }
      template {
        change_mode = "noop"
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
        change_mode = "noop"
        destination = "local/dynamicconfig.yaml"
        data = <<-EOT
limit.maxIDLength:
  - value: 255
    constraints: {}
EOT
      }
    }
    task "temporal-server" {
      driver = "raw_exec"
      user = "temporal_server"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      config {
        command = "/opt/verself/profile/bin/verself-temporal-server"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "temporal-server"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_SUPERVISOR = "nomad"
        VERSELF_TEMPORAL_CONFIG_PATH = "$${NOMAD_TASK_DIR}/config.yaml"
        VERSELF_TEMPORAL_NAMESPACE_ROLES = "spiffe://spiffe.verself.sh/svc/sandbox-rental-service|sandbox-rental-service|admin,spiffe://spiffe.verself.sh/svc/billing-service|billing-service|admin"
        VERSELF_TEMPORAL_SYSTEM_ADMIN_IDS = "spiffe://spiffe.verself.sh/svc/temporal-server"
      }
      resources {
        cpu = 1000
        memory = 2048
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "temporal-frontend-grpc"
        port = "frontend_grpc"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "temporal-frontend-grpc-tcp"
          type = "tcp"
          port = "frontend_grpc"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "temporal-metrics"
        port = "metrics"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "temporal-metrics-http"
          type = "http"
          path = "/metrics"
          port = "metrics"
          interval = "1s"
          timeout = "3s"
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
      user = "temporal_server"
      lifecycle {
        hook = "poststart"
        sidecar = false
      }
      config {
        args = ["-ec", "last_status=1\nfor attempt in $(seq 1 30); do\n  if /opt/verself/profile/bin/temporal-bootstrap; then\n    exit 0\n  fi\n  last_status=$?\n  sleep 1\ndone\nexit \"$last_status\"\n"]
        command = "/bin/sh"
      }
      env {
        HOME = "/tmp"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "temporal-bootstrap"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_SUPERVISOR = "nomad"
        VERSELF_TEMPORAL_BOOTSTRAP_NAMESPACES = "sandbox-rental-service,billing-service"
        VERSELF_TEMPORAL_BOOTSTRAP_NAMESPACE_RETENTION = "24h"
        VERSELF_TEMPORAL_FRONTEND_ADDRESS = "127.0.0.1:$${NOMAD_PORT_frontend_grpc}"
      }
      resources {
        cpu = 200
        memory = 512
      }
    }
    update {
      max_parallel = 1
      health_check = "checks"
      min_healthy_time = "3s"
      healthy_deadline = "300s"
      progress_deadline = "600s"
      canary = 1
      auto_revert = true
      auto_promote = true
    }
  }
}
