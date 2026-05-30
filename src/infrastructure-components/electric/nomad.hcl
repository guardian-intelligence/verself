job "electric" {
  name = "electric"
  datacenters = ["dc1"]
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
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/electric-nomad-runner"
        args = [
          "--ctr=local/bin/ctr",
          "--containerd-address=/run/electric-containerd/containerd.sock",
          "--image=docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176",
          "--env-file=secrets/electric.env",
          "--pg-user=electric",
          "--pg-password-file=/etc/credstore/electric/pg-password",
          "--pg-database=sandbox_rental",
          "--electric-secret-file=/etc/credstore/electric/api-secret",
          "--storage-dir=/var/lib/electric",
          "--instance-id=electric",
          "--replication-stream-id=default",
          "--db-pool-size=15",
          "--runc-binary=local/bin/runc",
        ]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "electric"
        VERSELF_SUPERVISOR = "nomad"
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
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/electric-nomad-runner"
        args = [
          "--ctr=local/bin/ctr",
          "--containerd-address=/run/electric-containerd/containerd.sock",
          "--image=docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176",
          "--env-file=secrets/electric.env",
          "--pg-user=electric_notifications",
          "--pg-password-file=/etc/credstore/electric-notifications/pg-password",
          "--pg-database=notifications_service",
          "--electric-secret-file=/etc/credstore/electric-notifications/api-secret",
          "--storage-dir=/var/lib/electric-notifications",
          "--instance-id=electric-notifications",
          "--replication-stream-id=notifications",
          "--db-pool-size=8",
          "--runc-binary=local/bin/runc",
        ]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "electric-notifications"
        VERSELF_SUPERVISOR = "nomad"
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
      artifact {
        source = "verself-artifact://electric-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/electric-nomad-runner"
        args = [
          "--ctr=local/bin/ctr",
          "--containerd-address=/run/electric-containerd/containerd.sock",
          "--image=docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176",
          "--env-file=secrets/electric.env",
          "--pg-user=electric_iam",
          "--pg-password-file=/etc/credstore/electric-iam/pg-password",
          "--pg-database=iam_service",
          "--electric-secret-file=/etc/credstore/verself-web/electric-api-secret",
          "--storage-dir=/var/lib/electric-iam",
          "--instance-id=electric-iam",
          "--replication-stream-id=iam",
          "--db-pool-size=8",
          "--runc-binary=local/bin/runc",
        ]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "electric-iam"
        VERSELF_SUPERVISOR = "nomad"
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
