job "deployment-service" {
  name = "deployment-service"
  datacenters = ["*"]
  type = "service"
  group "deployment-service" {
    count = 1
    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "deployment-service-setup" {
      driver = "raw_exec"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      config {
        command = "/usr/bin/install"
        args = ["-d", "-o", "deployment_service", "-g", "deployment_service", "-m", "0750", "/home/deployment_service", "/var/lib/verself/deployment-service", "/var/lib/verself/deployment-service/.cache", "/var/lib/verself/deployment-service/.cache/bazelisk", "/var/lib/verself/deployment-service/.docker", "/var/lib/verself/deployment-service/tmp"]
      }
      resources {
        cpu = 50
        memory = 32
      }
    }
    task "deployment-service-zot-auth" {
      driver = "raw_exec"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://deployment-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/deployment-service-zot-auth"
        args = [
          "--registry=127.0.0.1:5080",
          "--username=artifact-publisher",
          "--password-file=/etc/zot/publisher-password",
          "--output=/var/lib/verself/deployment-service/.docker/config.json",
          "--owner=deployment_service",
          "--group=deployment_service",
        ]
      }
      resources {
        cpu = 50
        memory = 32
      }
    }
    task "deployment-service-migrate" {
      driver = "raw_exec"
      user = "deployment_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://deployment-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/deployment-service"
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        VERSELF_PG_DSN = "postgres://deployment_service@/deployment_service?host=/var/run/postgresql&sslmode=disable"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "deployment-service" {
      driver = "raw_exec"
      user = "deployment_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      # env = true so Nomad exports VAULT_TOKEN; the sigstore hashivault
      # provider reads VAULT_TOKEN/BAO_TOKEN from env (falling back to
      # ~/.vault-token) and cannot read Nomad's secrets-dir token file.
      vault {
        env  = true
        role = "deployment-service-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://deployment-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/deployment-service"
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        BAO_ADDR = "https://127.0.0.1:8200"
        # sigstore hashivault reads the CA bundle only from VAULT_CACERT;
        # BAO_CACERT is ignored.
        VAULT_CACERT = "/etc/verself/openbao/ca.pem"
        BAZELISK_HOME = "/var/lib/verself/deployment-service/.cache/bazelisk"
        HOME = "/var/lib/verself/deployment-service"
        LOGNAME = "deployment_service"
        DOCKER_CONFIG = "/var/lib/verself/deployment-service/.docker"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "deployment-service"
        PATH = "$${NOMAD_TASK_DIR}/bin:/opt/verself/profile/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        TMPDIR = "/var/lib/verself/deployment-service/tmp"
        USER = "deployment_service"
        VERSELF_DEPLOY_BAZEL_JOBS = "4"
        VERSELF_DEPLOY_GITHUB_ALLOWED_REFS = "__VERSELF_DEPLOY_GITHUB_ALLOWED_REFS__"
        VERSELF_DEPLOY_GITHUB_ALLOWED_REPOSITORIES = "__VERSELF_DEPLOY_GITHUB_ALLOWED_REPOSITORIES__"
        VERSELF_DEPLOY_GITHUB_ALLOWED_WORKFLOW_REFS = "__VERSELF_DEPLOY_GITHUB_ALLOWED_WORKFLOW_REFS__"
        VERSELF_DEPLOY_GITHUB_OIDC_AUDIENCE = "__VERSELF_DEPLOYMENT_SERVICE_BASE_URL__"
        VERSELF_DEPLOY_REPO_INIT_TIMEOUT = "2m"
        VERSELF_DEPLOY_REPO_ROOT = "/var/lib/verself/deployment-service/repo"
        VERSELF_DEPLOY_REPO_URL = "__VERSELF_DEPLOY_REPO_URL__"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_NOMAD_ADDR = "http://127.0.0.1:4646"
        VERSELF_PG_DSN = "postgres://deployment_service@/deployment_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "4"
        VERSELF_R2_CONTROL_PLANE_ADDR = "https://cloudflare-r2-control-plane-internal-https"
        VERSELF_RECOVERY_SSH_READY = "true"
        VERSELF_SITE = "__VERSELF_SITE__"
        VERSELF_SUPERVISOR = "nomad"
        XDG_CACHE_HOME = "/var/lib/verself/deployment-service/.cache"
      }
      resources {
        cpu = 1500
        memory = 4096
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "deployment-service-public-api"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "deployment-service-http-public_http"
          type = "http"
          path = "/healthz"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
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
