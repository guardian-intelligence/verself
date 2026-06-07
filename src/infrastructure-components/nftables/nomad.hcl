variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "nftables_resource_name" {
  type    = string
  default = "nftables"
}

job "nftables" {
  name        = "nftables"
  datacenters = ["*"]
  type        = "batch"

  group "nftables" {
    count = 1

    task "apply" {
      driver = "raw_exec"
      user = "root"

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/nftables/cmd/nftables-apply/nftables-apply_/nftables-apply"
        args = [
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.nftables_resource_name}",
        ]
      }

      resources {
        cpu    = 50
        memory = 64
      }

      restart {
        attempts = 2
        delay = "5s"
        interval = "60s"
        mode = "fail"
      }
    }
  }
}
