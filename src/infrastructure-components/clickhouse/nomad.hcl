variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "clickhouse_resource_name" {
  type    = string
  default = "clickhouse"
}

job "clickhouse" {
  name        = "clickhouse"
  datacenters = ["*"]
  type        = "service"

  group "clickhouse" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "native_tls" {
        host_network = "loopback"
        static       = 9440
        to           = 9440
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      restart {
        attempts = 0
        mode     = "fail"
      }

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/clickhouse/cmd/clickhouse-recover/clickhouse-recover_/clickhouse-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.clickhouse_resource_name}",
        ]
      }

      resources {
        cpu    = 100
        memory = 256
      }
    }

    task "monitor" {
      driver = "raw_exec"
      user   = "root"

      restart {
        attempts = 5
        interval = "5m"
        delay    = "5s"
        mode     = "delay"
      }

      config {
        command = "/opt/verself/clickhouse/current/bin/clickhouse-recover"
        args = [
          "monitor",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.clickhouse_resource_name}",
        ]
      }

      resources {
        cpu    = 50
        memory = 128
      }

      service {
        name         = "clickhouse-native-tls"
        port         = "native_tls"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "native-tls-listener"
          type     = "tcp"
          port     = "native_tls"
          interval = "10s"
          timeout  = "2s"
        }
      }
    }
  }
}
