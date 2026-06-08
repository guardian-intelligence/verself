variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "object_storage_resource_name" {
  type    = string
  default = "object-storage"
}

variable "object_storage_runtime_root" {
  type    = string
  default = "/var/lib/object-storage-service/runtime"
}

job "object-storage-service" {
  name        = "object-storage-service"
  datacenters = ["*"]
  type        = "service"

  group "object-storage-service" {
    count = 2

    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
      }
    }

    task "setup" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service_/object-storage-service"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-name=${var.object_storage_resource_name}",
          "--runtime-root=${var.object_storage_runtime_root}",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }

    task "object-storage-service" {
      driver         = "raw_exec"
      user           = "object_storage_service"
      kill_signal    = "SIGTERM"
      kill_timeout   = "30s"
      shutdown_delay = "5s"

      vault {
        env  = false
        role = "object-storage-service-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "${var.object_storage_runtime_root}/current/bin/object-storage-service"
        args = [
          "--role=s3",
          "--resource-graph=/run/verself/recovery/object-storage/document.json",
          "--resource-name=${var.object_storage_resource_name}",
          "--listen-addr=127.0.0.1:$${NOMAD_PORT_public_http}",
        ]
      }

      env {
        CREDENTIALS_DIRECTORY        = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES    = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME           = "object-storage-service"
        VERSELF_SUPERVISOR          = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.credential_kek"
        perms       = "0600"
        uid         = "960"
        gid         = "960"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.credential_kek" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.r2.proxy_access_key_id"
        perms       = "0600"
        uid         = "960"
        gid         = "960"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.proxy_access_key_id" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.r2.proxy_secret_access_key"
        perms       = "0600"
        uid         = "960"
        gid         = "960"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.proxy_secret_access_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      resources {
        cpu    = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "object-storage-service-public-http"
        port         = "public_http"
        provider     = "nomad"
        address_mode = "auto"
        check {
          name            = "object-storage-service-http-public_http"
          type            = "http"
          path            = "/healthz"
          protocol        = "https"
          tls_skip_verify = true
          port            = "public_http"
          interval        = "1s"
          timeout         = "3s"
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

  group "object-storage-admin" {
    count = 1

    network {
      mode = "host"
      port "admin_http" {
        host_network = "loopback"
      }
    }

    task "setup" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service_/object-storage-service"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-name=${var.object_storage_resource_name}",
          "--runtime-root=${var.object_storage_runtime_root}",
          "--migrate=false",
        ]
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }

    task "object-storage-admin" {
      driver         = "raw_exec"
      user           = "object_storage_admin"
      kill_signal    = "SIGTERM"
      kill_timeout   = "30s"
      shutdown_delay = "5s"

      vault {
        env  = false
        role = "object-storage-service-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "${var.object_storage_runtime_root}/current/bin/object-storage-service"
        args = [
          "--role=admin",
          "--resource-graph=/run/verself/recovery/object-storage/document.json",
          "--resource-name=${var.object_storage_resource_name}",
          "--admin-listen-addr=127.0.0.1:$${NOMAD_PORT_admin_http}",
        ]
      }

      env {
        CREDENTIALS_DIRECTORY        = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES    = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME           = "object-storage-admin"
        VERSELF_SUPERVISOR          = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.auth_audience"
        perms       = "0600"
        uid         = "961"
        gid         = "961"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.credential_kek"
        perms       = "0600"
        uid         = "961"
        gid         = "961"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.credential_kek" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.r2.admin_access_key_id"
        perms       = "0600"
        uid         = "961"
        gid         = "961"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.r2.admin_secret_access_key"
        perms       = "0600"
        uid         = "961"
        gid         = "961"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.admin_secret_access_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      resources {
        cpu    = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "object-storage-admin-internal-https"
        port         = "admin_http"
        provider     = "nomad"
        address_mode = "auto"
        check {
          name     = "object-storage-admin-tcp-admin_http"
          type     = "tcp"
          port     = "admin_http"
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
