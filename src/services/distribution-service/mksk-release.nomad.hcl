job "distribution-release-mksk" {
  name = "distribution-release-mksk"
  datacenters = ["dc1"]
  type = "batch"

  parameterized {
    payload = "required"
    meta_optional = ["package", "channel", "version", "source_commit"]
  }

  group "distribution-release-mksk" {
    task "prepare-artifact-root" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "install -d -m 0750 -o distribution_release -g zot /artifacts/releases/mksk/$${NOMAD_ALLOC_ID} /tmp/distribution-release-home"]
      }
      resources {
        cpu = 50
        memory = 32
      }
    }

    task "publish" {
      driver = "raw_exec"
      user = "distribution_release"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      artifact {
        source = "verself-artifact://distribution-service"
        destination = "local"
        chown = true
      }
      dispatch_payload {
        file = "release-payload.json"
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "exec local/bin/distribution-release mksk publish --payload local/release-payload.json --artifact-root /artifacts/releases/mksk/$${NOMAD_ALLOC_ID} --tools-dir local"]
      }
      env {
        DISTRIBUTION_RELEASE_COSIGN_KEY = "/etc/credstore/distribution-service/release-cosign-key"
        DISTRIBUTION_RELEASE_ORIGIN_REGISTRY_URL = "http://127.0.0.1:5080"
        DISTRIBUTION_RELEASE_PUBLIC_REGISTRY_URL = "https://oci.verself.sh"
        DISTRIBUTION_RELEASE_SIGNER_ID = "spiffe://prod.verself.sh/svc/distribution-release"
        DISTRIBUTION_RELEASE_SOURCE_REPOSITORY_URL = "https://github.com/guardian-intelligence/verself.git"
        DISTRIBUTION_RELEASE_ZOT_PASSWORD_FILE = "/etc/zot/publisher-password"
        DISTRIBUTION_RELEASE_ZOT_USERNAME = "artifact-publisher"
        HOME = "/tmp/distribution-release-home"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "distribution-release-mksk"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        XDG_CACHE_HOME = "/tmp/distribution-release-home/.cache"
      }
      resources {
        cpu = 4000
        memory = 8192
      }
    }
  }
}
