job "garage" {
  name = "garage"
  datacenters = ["dc1"]
  type = "service"

  group "garage-0" {
    count = 1

    network {
      mode = "host"
      port "s3" {
        host_network = "loopback"
        static = 3900
        to = 3900
      }
      port "rpc" {
        host_network = "loopback"
        static = 3901
        to = 3901
      }
      port "admin" {
        host_network = "loopback"
        static = 3903
        to = 3903
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "garage"

      config {
        command = "/opt/verself/profile/bin/garage"
        args = ["-c", "/etc/garage/garage-0.toml", "server"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "garage"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 500
        memory = 512
      }

      service {
        name = "garage-s3"
        port = "s3"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "garage-admin"
        port = "admin"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }

  group "garage-1" {
    count = 1

    network {
      mode = "host"
      port "s3" {
        host_network = "loopback"
        static = 3910
        to = 3910
      }
      port "rpc" {
        host_network = "loopback"
        static = 3911
        to = 3911
      }
      port "admin" {
        host_network = "loopback"
        static = 3913
        to = 3913
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "garage"

      config {
        command = "/opt/verself/profile/bin/garage"
        args = ["-c", "/etc/garage/garage-1.toml", "server"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "garage"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 500
        memory = 512
      }

      service {
        name = "garage-s3"
        port = "s3"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "garage-admin"
        port = "admin"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }

  group "garage-2" {
    count = 1

    network {
      mode = "host"
      port "s3" {
        host_network = "loopback"
        static = 3920
        to = 3920
      }
      port "rpc" {
        host_network = "loopback"
        static = 3921
        to = 3921
      }
      port "admin" {
        host_network = "loopback"
        static = 3923
        to = 3923
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "garage"

      config {
        command = "/opt/verself/profile/bin/garage"
        args = ["-c", "/etc/garage/garage-2.toml", "server"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "garage"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 500
        memory = 512
      }

      service {
        name = "garage-s3"
        port = "s3"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "garage-admin"
        port = "admin"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
