variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "iam_service_resource_name" {
  type    = string
  default = "iam-service"
}

variable "iam_service_runtime_root" {
  type    = string
  default = "/var/lib/iam-service/runtime"
}

variable "iam_service_projected_graph" {
  type    = string
  default = "/run/verself/recovery/iam-service/document.json"
}

job "iam-service" {
  name = "iam-service"
  datacenters = ["*"]
  type = "service"
  group "iam-service" {
    count = 2
    network {
      mode = "host"
      port "internal_https" {
        host_network = "loopback"
      }
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "recover" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/services/iam-service/cmd/iam-service/iam-service_/iam-service"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.iam_service_resource_name}",
          "--runtime-root=${var.iam_service_runtime_root}",
          "--projected-graph=${var.iam_service_projected_graph}",
          "--migrate",
        ]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "iam-service-migration"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "iam-service" {
      driver = "raw_exec"
      user = "iam_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        env = false
        role = "iam-service-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      config {
        command = "${var.iam_service_runtime_root}/current/bin/iam-service"
        args = [
          "--resource-graph=${var.iam_service_projected_graph}",
          "--resource-name=${var.iam_service_resource_name}",
          "--listen-addr=127.0.0.1:$${NOMAD_PORT_public_http}",
          "--internal-listen-addr=127.0.0.1:$${NOMAD_PORT_internal_https}",
        ]
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "iam-service"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 500
        memory = 256
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "iam-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "iam-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "iam-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "iam-service-http-public_http"
          type = "http"
          path = "/readyz"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.email_identity.hmac_key"
        perms = "0600"
        uid = "983"
        gid = "976"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.email_identity.hmac_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.spicedb.grpc_preshared_key"
        perms = "0600"
        uid = "983"
        gid = "976"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.spicedb.grpc_preshared_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.admin_token"
        perms = "0600"
        uid = "983"
        gid = "976"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.admin_token" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.auth_audience"
        perms = "0600"
        uid = "983"
        gid = "976"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.oidc_client_id"
        perms = "0600"
        uid = "983"
        gid = "976"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.oidc_client_id" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.oidc_client_secret"
        perms = "0600"
        uid = "983"
        gid = "976"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.oidc_client_secret" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.action_signing_key"
        perms = "0600"
        uid = "983"
        gid = "976"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.action_signing_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        perms = "0600"
        data = <<-EOT
IAM_ZITADEL_BASE_URL=http://{{- with nomadService "zitadel-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
IAM_SPICEDB_GRPC_ENDPOINT={{- with nomadService "spicedb-grpc" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
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
