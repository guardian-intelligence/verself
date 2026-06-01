job "zitadel" {
  name = "zitadel"
  datacenters = ["*"]
  type = "service"

  group "zitadel" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 8085
        to = 8085
      }
    }

    task "setup" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      vault {
        role = "zitadel-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://zitadel-setup-apply"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://zitadel-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/zitadel-setup-apply"
        args = [
          "--config=$${NOMAD_TASK_DIR}/config.yaml",
          "--steps=$${NOMAD_TASK_DIR}/steps.yaml",
          "--masterkey=$${NOMAD_SECRETS_DIR}/masterkey",
          "--admin-pat-path=$${NOMAD_TASK_DIR}/zitadel-admin/admin.pat",
        ]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/openbao/tls/cert.pem"
        HOME = "/var/lib/zitadel"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "zitadel-setup-apply"
        VERSELF_ZITADEL_BIN = "local/bin/zitadel"
        VERSELF_ZITADEL_ADMIN_PAT_OPENBAO_SECRETS = "auth-control-plane.zitadel.admin_token,iam-service.zitadel.admin_token"
        VERSELF_ZITADEL_EXTERNAL_DOMAIN = "__VERSELF_PRODUCT_DOMAIN__"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 200
        memory = 256
      }

      template {
        change_mode = "restart"
        destination = "secrets/masterkey"
        perms = "0400"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/zitadel.masterkey" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "local/config.yaml"
        data = <<-EOT
Log:
  Level: info
  Formatter:
    Format: json

Port: 8085
ExternalDomain: __VERSELF_PRODUCT_DOMAIN__
ExternalPort: 443
ExternalSecure: true

TLS:
  Enabled: false

Database:
  postgres:
    Host: /var/run/postgresql
    Port: 5432
    Database: zitadel
    User:
      Username: zitadel
      Password: ""
      SSL:
        Mode: disable
    MaxOpenConns: 10
    MaxIdleConns: 5
    MaxConnLifetime: 30m

DefaultInstance:
  Features:
    LoginV2:
      Required: false
  DomainPolicy:
    SMTPSenderAddressMatchesInstanceDomain: false
  SMTPConfiguration:
    SMTP:
      Host: "smtp.resend.com:465"
      User: "resend"
      Password: "{{ with secret "kv-runtime/data/secret/org/zitadel.smtp.password" }}{{ .Data.data.value }}{{ end }}"
    TLS: true
    From: "__VERSELF_EMAIL_FROM_ADDRESS__"
    FromName: "verself"
  LabelPolicy:
    DisableWatermark: true
    HideLoginNameSuffix: true
  LoginPolicy:
    AllowRegister: false

Machine:
  Identification:
    PrivateIp:
      Enabled: false
    Hostname:
      Enabled: true

Instrumentation:
  Trace:
    Exporter:
      Type: grpc
      Endpoint: 127.0.0.1:4317
      Insecure: true
    Fraction: 1
  Metrics:
    Enabled: true
EOT
      }

      template {
        change_mode = "restart"
        destination = "local/steps.yaml"
        data = <<-EOT
FirstInstance:
  InstanceName: verself
  PatPath: "{{ env "NOMAD_TASK_DIR" }}/zitadel-admin/admin.pat"
  Org:
    Name: Guardian Intelligence LLC
    Human:
      UserName: anveio
      FirstName: Anveio
      LastName: Platform
      Email:
        Address: "anveio@__VERSELF_PRODUCT_DOMAIN__"
        Verified: true
      Password: "{{ with secret "kv-runtime/data/secret/org/zitadel.admin_password" }}{{ .Data.data.value }}{{ end }}"
      PasswordChangeRequired: false
    Machine:
      Machine:
        Username: verself-admin
        Name: verself admin
      Pat:
        ExpirationDate: "2099-01-01T00:00:00Z"
EOT
      }
    }

    # Nomad renders Vault templates as the agent user; the launcher fixes
    # task-local ownership, then drops to the zitadel user for the server.
    task "server" {
      driver = "raw_exec"
      user = "root"

      vault {
        role = "zitadel-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://zitadel-runtime"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://zitadel-setup-apply"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/zitadel-setup-apply"
        args = [
          "--mode=start",
          "--config=$${NOMAD_TASK_DIR}/config.yaml",
          "--masterkey=$${NOMAD_SECRETS_DIR}/masterkey",
        ]
      }

      env {
        HOME = "/var/lib/zitadel"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "zitadel"
        VERSELF_ZITADEL_EXTERNAL_DOMAIN = "__VERSELF_PRODUCT_DOMAIN__"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 700
        memory = 1024
      }

      service {
        name = "zitadel-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }

      template {
        change_mode = "restart"
        destination = "secrets/masterkey"
        perms = "0400"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/zitadel.masterkey" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "local/config.yaml"
        data = <<-EOT
Log:
  Level: info
  Formatter:
    Format: json

Port: 8085
ExternalDomain: __VERSELF_PRODUCT_DOMAIN__
ExternalPort: 443
ExternalSecure: true

TLS:
  Enabled: false

Database:
  postgres:
    Host: /var/run/postgresql
    Port: 5432
    Database: zitadel
    User:
      Username: zitadel
      Password: ""
      SSL:
        Mode: disable
    MaxOpenConns: 10
    MaxIdleConns: 5
    MaxConnLifetime: 30m

DefaultInstance:
  Features:
    LoginV2:
      Required: false
  DomainPolicy:
    SMTPSenderAddressMatchesInstanceDomain: false
  SMTPConfiguration:
    SMTP:
      Host: "smtp.resend.com:465"
      User: "resend"
      Password: "{{ with secret "kv-runtime/data/secret/org/zitadel.smtp.password" }}{{ .Data.data.value }}{{ end }}"
    TLS: true
    From: "__VERSELF_EMAIL_FROM_ADDRESS__"
    FromName: "verself"
  LabelPolicy:
    DisableWatermark: true
    HideLoginNameSuffix: true
  LoginPolicy:
    AllowRegister: false

Machine:
  Identification:
    PrivateIp:
      Enabled: false
    Hostname:
      Enabled: true

Instrumentation:
  Trace:
    Exporter:
      Type: grpc
      Endpoint: 127.0.0.1:4317
      Insecure: true
    Fraction: 1
  Metrics:
    Enabled: true
EOT
      }
    }
  }
}
