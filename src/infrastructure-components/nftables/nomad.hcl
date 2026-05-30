job "nftables" {
  name = "nftables"
  datacenters = ["dc1"]
  type = "batch"

  group "nftables" {
    count = 1

    task "apply" {
      driver = "raw_exec"
      user = "root"

      config {
        command = "$${VERSELF_NFTABLES_RUNTIME}/opt/verself/nftables/bin/nftables-apply"
        args = [
          "--artifact-root", "$${VERSELF_NFTABLES_RUNTIME}/opt/verself/nftables",
          "--nft-bin", "$${VERSELF_NFTABLES_RUNTIME}/opt/verself/nftables/bin/nft",
          "--ip-bin", "/usr/sbin/ip",
          "--ld-library-path", "$${VERSELF_NFTABLES_RUNTIME}/opt/verself/nftables/lib/x86_64-linux-gnu",
        ]
      }

      env {
        LD_LIBRARY_PATH = "$${VERSELF_NFTABLES_RUNTIME}/opt/verself/nftables/lib/x86_64-linux-gnu"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "nftables"
        VERSELF_NFTABLES_RUNTIME = "verself-artifact://nftables-runtime"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 50
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
