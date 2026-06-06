variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "electric_resource_name" {
  type    = string
  default = "electric"
}

job "electric-containerd" {
  name        = "electric-containerd"
  datacenters = ["*"]
  type        = "service"

  group "containerd" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    task "containerd" {
      driver       = "raw_exec"
      user         = "root"
      kill_signal  = "SIGTERM"
      kill_timeout = "45s"

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/electric/cmd/electric-recover/electric-recover_/electric-recover"
        args = [
          "containerd",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.electric_resource_name}",
        ]
      }

      resources {
        cpu    = 200
        memory = 256
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }
    }

    update {
      max_parallel      = 1
      health_check      = "task_states"
      min_healthy_time  = "3s"
      healthy_deadline  = "300s"
      progress_deadline = "600s"
      auto_revert       = true
    }
  }
}
