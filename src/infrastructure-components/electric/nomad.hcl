job "electric" {
  name = "electric"
  datacenters = ["*"]
  type = "service"

  group "electric-default" {
    count = 1
    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
      }
    }

    task "electric-default-image" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "image='docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176'\nsocket=/run/electric-containerd/containerd.sock\nfor _ in $(seq 1 60); do\n  if local/bin/ctr --address \"$socket\" version >/dev/null 2>&1; then\n    break\n  fi\n  sleep 1\ndone\nlocal/bin/ctr --address \"$socket\" version >/dev/null\nif ! local/bin/ctr --address \"$socket\" images inspect \"$image\" >/dev/null 2>&1; then\n  local/bin/ctr --address \"$socket\" images pull --platform linux/amd64 \"$image\"\nfi\n"]
      }
      resources {
        cpu = 200
        memory = 256
      }
    }

    task "electric-default" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "45s"
      vault {
        role = "electric-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "chmod 0600 secrets/electric.env\nruntime_dir=\"$PWD/local/bin\"\nexec \"$runtime_dir/electric-nomad-runner\" \\\n  --ctr=\"$runtime_dir/ctr\" \\\n  --containerd-address=/run/electric-containerd/containerd.sock \\\n  --image=docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176 \\\n  --env-file=\"$PWD/secrets/electric.env\" \\\n  --storage-dir=/var/lib/electric \\\n  --instance-id=electric \\\n  --replication-stream-id=default \\\n  --db-pool-size=15 \\\n  --runc-binary=\"$runtime_dir/runc\"\n"]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "electric"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        destination = "secrets/electric.env"
        perms = "0600"
        change_mode = "restart"
        data = <<EOH
DATABASE_URL=postgresql://electric:{{ with secret "kv-runtime/data/secret/org/electric.pg.password" }}{{ .Data.data.value }}{{ end }}@127.0.0.1:5432/sandbox_rental
ELECTRIC_SECRET={{ with secret "kv-runtime/data/secret/org/electric.api_secret" }}{{ .Data.data.value }}{{ end }}
EOH
      }
      resources {
        cpu = 500
        memory = 512
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "electric-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "electric-http-tcp"
          type = "tcp"
          port = "http"
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
      auto_revert = true
    }
  }

  group "electric-notifications" {
    count = 1
    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
      }
    }

    task "electric-notifications-image" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "image='docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176'\nsocket=/run/electric-containerd/containerd.sock\nfor _ in $(seq 1 60); do\n  if local/bin/ctr --address \"$socket\" version >/dev/null 2>&1; then\n    break\n  fi\n  sleep 1\ndone\nlocal/bin/ctr --address \"$socket\" version >/dev/null\nif ! local/bin/ctr --address \"$socket\" images inspect \"$image\" >/dev/null 2>&1; then\n  local/bin/ctr --address \"$socket\" images pull --platform linux/amd64 \"$image\"\nfi\n"]
      }
      resources {
        cpu = 200
        memory = 256
      }
    }

    task "electric-notifications" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "45s"
      vault {
        role = "electric-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "chmod 0600 secrets/electric.env\nruntime_dir=\"$PWD/local/bin\"\nexec \"$runtime_dir/electric-nomad-runner\" \\\n  --ctr=\"$runtime_dir/ctr\" \\\n  --containerd-address=/run/electric-containerd/containerd.sock \\\n  --image=docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176 \\\n  --env-file=\"$PWD/secrets/electric.env\" \\\n  --storage-dir=/var/lib/electric-notifications \\\n  --instance-id=electric-notifications \\\n  --replication-stream-id=notifications \\\n  --db-pool-size=8 \\\n  --runc-binary=\"$runtime_dir/runc\"\n"]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "electric-notifications"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        destination = "secrets/electric.env"
        perms = "0600"
        change_mode = "restart"
        data = <<EOH
DATABASE_URL=postgresql://electric_notifications:{{ with secret "kv-runtime/data/secret/org/electric-notifications.pg.password" }}{{ .Data.data.value }}{{ end }}@127.0.0.1:5432/notifications_service
ELECTRIC_SECRET={{ with secret "kv-runtime/data/secret/org/electric-notifications.api_secret" }}{{ .Data.data.value }}{{ end }}
EOH
      }
      resources {
        cpu = 500
        memory = 512
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "electric-notifications-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "electric-notifications-http-tcp"
          type = "tcp"
          port = "http"
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
      auto_revert = true
    }
  }

  group "electric-iam" {
    count = 1
    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
      }
    }

    task "electric-iam-image" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "image='docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176'\nsocket=/run/electric-containerd/containerd.sock\nfor _ in $(seq 1 60); do\n  if local/bin/ctr --address \"$socket\" version >/dev/null 2>&1; then\n    break\n  fi\n  sleep 1\ndone\nlocal/bin/ctr --address \"$socket\" version >/dev/null\nif ! local/bin/ctr --address \"$socket\" images inspect \"$image\" >/dev/null 2>&1; then\n  local/bin/ctr --address \"$socket\" images pull --platform linux/amd64 \"$image\"\nfi\n"]
      }
      resources {
        cpu = 200
        memory = 256
      }
    }

    task "electric-iam" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "45s"
      vault {
        role = "electric-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "chmod 0600 secrets/electric.env\nruntime_dir=\"$PWD/local/bin\"\nexec \"$runtime_dir/electric-nomad-runner\" \\\n  --ctr=\"$runtime_dir/ctr\" \\\n  --containerd-address=/run/electric-containerd/containerd.sock \\\n  --image=docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176 \\\n  --env-file=\"$PWD/secrets/electric.env\" \\\n  --storage-dir=/var/lib/electric-iam \\\n  --instance-id=electric-iam \\\n  --replication-stream-id=iam \\\n  --db-pool-size=8 \\\n  --runc-binary=\"$runtime_dir/runc\"\n"]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "electric-iam"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        destination = "secrets/electric.env"
        perms = "0600"
        change_mode = "restart"
        data = <<EOH
DATABASE_URL=postgresql://electric_iam:{{ with secret "kv-runtime/data/secret/org/electric-iam.pg.password" }}{{ .Data.data.value }}{{ end }}@127.0.0.1:5432/iam_service
ELECTRIC_SECRET={{ with secret "kv-runtime/data/secret/org/electric-iam.api_secret" }}{{ .Data.data.value }}{{ end }}
EOH
      }
      resources {
        cpu = 500
        memory = 512
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "electric-iam-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "electric-iam-http-tcp"
          type = "tcp"
          port = "http"
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
      auto_revert = true
    }
  }
}
