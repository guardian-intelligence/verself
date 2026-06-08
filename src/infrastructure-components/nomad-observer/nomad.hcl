variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "nomad_observer_resource_name" {
  type    = string
  default = "nomad-observer"
}

job "nomad-observer" {
  name        = "nomad-observer"
  datacenters = ["*"]
  type        = "service"

  group "observer" {
    count = 1

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer_/nomad-observer"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.nomad_observer_resource_name}",
        ]
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }

    task "observer" {
      driver = "raw_exec"
      user   = "nomad_observer"

      kill_signal = "SIGTERM"
      kill_timeout = "15s"

      config {
        command = "/var/lib/nomad-observer/runtime/current/bin/nomad-observer"
        args = [
          "run",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=/run/verself/recovery/nomad-observer/document.json",
          "--resource-name=${var.nomad_observer_resource_name}",
        ]
      }

      resources {
        cpu = 100
        memory = 128
      }

      restart {
        attempts = 5
        delay = "10s"
        interval = "300s"
        mode = "delay"
      }
    }

    update {
      max_parallel = 1
      health_check = "task_states"
      min_healthy_time = "5s"
      healthy_deadline = "120s"
      progress_deadline = "300s"
      auto_revert = true
    }
  }
}
