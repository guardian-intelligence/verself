# Agent and Proxy

OpenBao Agent is a client-side daemon that handles authentication, secret retrieval, template
rendering, and process supervision. It is the primary mechanism for injecting secrets into
applications without embedding OpenBao logic in the application itself.

Docs: https://openbao.org/docs/agent-and-proxy/agent/

## Modes

| Mode | Description | Use case |
|------|-------------|----------|
| Standard daemon | Long-running, provides auto-auth + API proxy + caching + template rendering | Services needing continuously refreshed secrets |
| Process supervisor | Runs a child process with secrets as env vars via `env_template` | CI jobs, batch processes |
| One-shot | `exit_after_auth = true`, authenticates, renders templates, exits | Init containers |

## Agent vs. Proxy

| Capability | Agent | Proxy |
|-----------|-------|-------|
| Auto-auth | Yes | Yes |
| Caching | Yes | Yes |
| API proxy | Yes (being deprecated) | **Yes (primary purpose)** |
| Template rendering | **Yes** | No |
| Process supervisor | **Yes** | No |

For forge-metal, Agent is the right choice -- it has template rendering and process supervision.

## Auto-auth

Authenticates to OpenBao and maintains a valid token. Exponential backoff on failure.

**Supported methods:** AppRole, Cert (TLS client cert), JWT, Kerberos, Kubernetes, Token file.

**Important limitation:** "Auto-auth does not support using tokens with a limited number of uses."

### AppRole auto-auth config

```hcl
auto_auth {
  method {
    type       = "approle"
    mount_path = "auth/approle"
    config = {
      role_id_file_path                   = "/etc/openbao/roleid"
      secret_id_file_path                 = "/etc/openbao/secretid"
      remove_secret_id_file_after_reading = true  # delete after first read
    }
  }

  sink {
    type     = "file"
    wrap_ttl = "30m"  # optional response-wrapping for MITM protection
    config = {
      path = "/tmp/openbao-token"
    }
  }
}
```

Sinks support **Diffie-Hellman encryption** (`dh_type = "curve25519"`) for encrypting tokens
at rest.

## Template rendering

Templates use **Consul Template** markup language.

### Reading secrets

```
{{ with secret "secret/data/my-app" }}
username={{ .Data.data.username }}
password={{ .Data.data.password }}
{{ end }}
```

### PKI certificates

```
{{ with pkiCert "pki/issue/my-domain" "common_name=foo.example.com" }}
{{ .Data.Key }}
{{ .Data.Cert }}
{{ end }}
```

### Template stanza options

| Option | Purpose |
|--------|---------|
| `source` | Path to template file |
| `destination` | Output file path (required) |
| `contents` | Inline template (mutually exclusive with `source`) |
| `create_dest_dirs` | Auto-create parent dirs (default: true) |
| `error_on_missing_key` | Error on missing keys ("highly recommended you set this to true") |
| `perms` | File permissions (default: 0644) |
| `exec` | Command to run when template changes |

### Renewal behavior

- Renewable secrets (leased): renewed at 2/3 of lease duration
- Non-renewable (KV v2): re-fetched every 5 minutes (configurable via `static_secret_render_interval`)
- Non-renewable leased (databases, KV v1): re-fetched at 85% of TTL

## Process supervisor mode

Runs a child process with secrets injected as environment variables:

```hcl
env_template "DB_PASSWORD" {
  contents = "{{ with secret \"secret/data/db\" }}{{ .Data.data.password }}{{ end }}"
}

env_template "API_KEY" {
  contents = "{{ with secret \"secret/data/api\" }}{{ .Data.data.key }}{{ end }}"
}

exec {
  command                   = ["/usr/bin/my-app", "--start"]
  restart_on_secret_changes = "always"  # restart child when secrets change
  restart_stop_signal       = "SIGTERM"
}
```

The Agent waits for all `env_template` blocks to render before starting the child process.

## Complete example: AppRole + templates + process supervisor

```hcl
pid_file = "/run/openbao-agent.pid"

vault {
  address = "http://127.0.0.1:8200"
}

auto_auth {
  method {
    type = "approle"
    config = {
      role_id_file_path                   = "/etc/openbao/roleid"
      secret_id_file_path                 = "/etc/openbao/secretid"
      remove_secret_id_file_after_reading = true
    }
  }
}

template_config {
  static_secret_render_interval = "10m"
  exit_on_retry_failure         = true
}

# Render a config file
template {
  contents    = <<-EOT
    {{ with secret "secret/data/myapp" }}
    DB_HOST={{ .Data.data.db_host }}
    DB_PASS={{ .Data.data.db_pass }}
    {{ end }}
  EOT
  destination = "/etc/myapp/config.env"
  perms       = "0600"
  error_on_missing_key = true
  exec {
    command = ["systemctl", "reload", "myapp"]
  }
}
```

## Applicability to forge-metal

### For CI jobs (Firecracker VMs)

Process supervisor mode is the natural fit. The host orchestrator writes AppRole credentials
to the zvol clone, then inside the VM:

```hcl
# /etc/openbao/ci-agent.hcl
vault {
  address = "http://<host-ip>:8200"
}

auto_auth {
  method {
    type = "approle"
    config = {
      role_id_file_path                   = "/run/openbao/roleid"
      secret_id_file_path                 = "/run/openbao/secretid"
      remove_secret_id_file_after_reading = true
    }
  }
}

env_template "NPM_TOKEN" {
  contents = "{{ with secret \"secret/data/ci/npm\" }}{{ .Data.data.token }}{{ end }}"
}

env_template "DEPLOY_KEY" {
  contents = "{{ with secret \"secret/data/ci/deploy\" }}{{ .Data.data.key }}{{ end }}"
}

exec {
  command = ["/bin/sh", "/run/ci/build.sh"]
}
```

The Agent authenticates, renders secrets as env vars, runs the build script, and exits when
the script finishes. The VM is then destroyed along with the zvol clone.

### For long-running services

Standard daemon mode with templates. The Agent renders config files for services like
ClickHouse or Forgejo and reloads them when secrets rotate. Runs as a systemd service
alongside the application services.
