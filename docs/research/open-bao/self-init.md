# Self-Init (Declarative Self-Initialization)

OpenBao-only feature (not in Vault). Automates the entire first-launch ceremony --
`operator init` + unseal + initial auth/policy/audit setup -- from the server config file alone.
No manual API calls, no shell scripts, no root token handling.

Shipped in: **v2.4.0** (July 2025)
RFC: https://openbao.org/docs/rfcs/self-init/
GitHub issue: https://github.com/openbao/openbao/issues/1340
Implementation PR: https://github.com/openbao/openbao/pull/1506

## The problem it solves

From the RFC (authored by Alexander Scheel / `cipherboy`):

> "OpenBao currently has a stateful, one-time initialization process. This makes operating it in
> a fully declarative environment, such as NixOS or OpenTofu, rather hard. In particular,
> initialization returns two items: a set of unseal or recovery key shards and a highly privileged
> root token; some initial setup using this root token should also be taken (such as initial auth
> method creation, policies, and audit logging), before this root token is ideally revoked."

> "A one-time initialization is very clearly scoped and lets proper authentication mechanisms
> take over sooner."

## Design decisions

- **Request/response pattern**: Configuration defines a sequence of API requests executed against
  the Core at first boot. Reuses OpenBao's existing API surface -- no new DSL.
- **One-time only**: Applied exactly once on first startup. Refuses to re-run on an already-initialized
  instance.
- **Root token auto-revoked**: The root token generated during init is used internally to execute
  the profile requests, then revoked. Never returned to any caller.
- **No recovery keys by default**: `RecoveryConfig` set to zero shares. Recovery keys can be
  generated later via authenticated endpoints.

## Source code

| File | Purpose |
|------|---------|
| `helper/profiles/config.go` | HCL parsing: `OuterConfig`, `RequestConfig` structs, `ParseOuterConfig()` |
| `helper/profiles/profiles.go` | Core engine: `ProfileEngine`, `Evaluate()`, `buildRequest()`, `evaluateField()` |
| `helper/profiles/history.go` | Request/response chaining: `EvaluationHistory` with 3D maps |
| `helper/profiles/env_source.go` | `eval_source = "env"` -- reads environment variables |
| `helper/profiles/file_source.go` | `eval_source = "file"` -- reads files from disk |
| `helper/profiles/request_source.go` | `eval_source = "request"` -- references previous request data |
| `helper/profiles/response_source.go` | `eval_source = "response"` -- references previous response data |
| `command/server.go` | Entry point: `ServerCommand.Initialize()` and `doSelfInit()` |

## Initialization flow (from `command/server.go`)

```
ServerCommand.Run()
  -> ServerCommand.Initialize(core, config)
       1. Check len(config.Initialization) > 0, else return nil (no-op)
       2. Check core.SealAccess().RecoveryKeySupported() -- REQUIRES auto-unseal
       3. Check core.Initialized() -- if already initialized, skip
       4. core.Initialize() with zero recovery shares
       5. MarkSelfInitStarted() -- write "failed" marker to barrier storage
       6. waitForLeader() -- up to 35 seconds for HA leadership election
       7. doSelfInit() -- create ProfileEngine, execute all requests
       8. MarkSelfInitComplete() -- delete marker from storage
```

The `doSelfInit()` function wires up the profile engine:

```go
p, err := profiles.NewEngine(
    profiles.WithEnvSource(),
    profiles.WithFileSource(),
    profiles.WithRequestSource(),
    profiles.WithResponseSource(),
    profiles.WithDefaultToken(rootToken),
    profiles.WithOuterBlockName("initialize"),
    profiles.WithProfile(config.Initialization),
    profiles.WithRequestHandler(func(ctx context.Context, req *logical.Request) (*logical.Response, error) {
        return core.HandleRequest(ctx, req)
    }),
    profiles.WithLogger(c.logger.Named("self-init")),
)
```

Requests go through `core.HandleRequest` directly -- the same path as normal API calls.
Audit logging, ACL checks, etc. all apply.

## HCL configuration syntax

The `initialize` stanza goes in the main OpenBao server config file.

### Request parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `operation` | Yes | ACL capability: `create`, `read`, `update`, `delete`, `list` |
| `path` | Yes | API path to call |
| `token` | No | Override token (defaults to auto-generated root token) |
| `data` | No | Request body as map |
| `allow_failure` | No | Boolean, default `false`. If `true`, failure does not halt init |

### Dynamic data sources (`eval_source` + `eval_type`)

Any field value can be replaced with a dynamic source object:

```hcl
"field_name" = {
  eval_type   = "string"   # Go types: string, int, float64, bool, []string, map, any
  eval_source = "env"      # Source: env, file, request, response, cel
  # ... source-specific params ...
}
```

| Source | Parameters | Description |
|--------|-----------|-------------|
| `env` | `env_var`, `require_present` | Read from environment variable |
| `file` | `path` | Read from file (supports `/dev/stdin`) |
| `request` | `initialize_name`, `req_name`, `field_selector` | Reference a previous request's data |
| `response` | `initialize_name`, `resp_name`, `field_selector` | Reference a previous response's data |
| `cel` | `expression` | Common Expression Language with injected `requests`/`responses` |

### Full example: audit + policy + auth + initial user

```hcl
initialize "audit" {
  request "enable-audit" {
    operation = "update"
    path = "sys/audit/stdout"
    data = {
      type = "file"
      options = {
        file_path = "/dev/stdout"
        log_raw = true
      }
    }
  }
}

initialize "policy" {
  request "add-operator-policy" {
    operation = "update"
    path = "sys/policies/acl/operator"
    data = {
      policy = <<-EOT
        path "secret/*" { capabilities = ["create", "update", "read", "delete", "list"] }
        path "sys/health" { capabilities = ["read"] }
        path "sys/policies/*" { capabilities = ["read", "list"] }
        path "auth/*" { capabilities = ["create", "update", "read", "delete", "list"] }
      EOT
    }
  }
}

initialize "secrets" {
  request "enable-kv" {
    operation = "update"
    path = "sys/mounts/secret"
    data = {
      type = "kv-v2"
    }
  }
}

initialize "auth" {
  request "enable-userpass" {
    operation = "update"
    path = "sys/auth/userpass"
    data = { type = "userpass" }
  }

  request "create-admin" {
    operation = "update"
    path = "auth/userpass/users/admin"
    data = {
      "password" = {
        eval_type      = "string"
        eval_source    = "env"
        env_var        = "OPENBAO_ADMIN_PASSWORD"
        require_present = true
      }
      "token_policies" = ["operator"]
    }
  }
}

initialize "approle" {
  request "enable-approle" {
    operation = "update"
    path = "sys/auth/approle"
    data = { type = "approle" }
  }

  request "create-ci-role" {
    operation = "update"
    path = "auth/approle/role/ci-runner"
    data = {
      secret_id_ttl   = "10m"
      token_ttl       = "20m"
      token_policies  = ["ci-read"]
    }
  }
}
```

## Zero-touch bootstrap: static seal + self-init

Combining `seal "static"` with `initialize` stanzas gives **fully declarative, zero-touch
OpenBao bootstrap**:

```hcl
seal "static" {
  current_key_id = "20260329-1"
  current_key    = "file:///etc/openbao/unseal.key"
}

listener "tcp" {
  address     = "127.0.0.1:8200"
  tls_disable = true
}

storage "raft" {
  path    = "/var/lib/openbao/raft"
  node_id = "node1"
}

initialize "audit" {
  # ... audit stanzas
}

initialize "auth" {
  # ... auth method + user stanzas
}

initialize "secrets" {
  # ... secret engine stanzas
}
```

The sequence at first boot:
1. Static seal provides auto-unseal (satisfies `RecoveryKeySupported()`)
2. `Initialize()` detects uninitialized
3. Creates barrier with zero recovery shares
4. Auto-unseals immediately
5. Executes all `initialize` stanza requests using ephemeral root token
6. Revokes root token
7. Done -- server is fully configured

Subsequent restarts: auto-unseals from static key, skips init (already done).

## Known issues

### Self-Init panic (PR #2442, open)

A nil `context.Context` propagation bug causes a panic during `MarkSelfInitComplete`. The fix
adds a state machine (`vault/self_init.go`) that writes a "failed" marker to storage before
self-init begins and deletes it on success. Enables crash detection: if marker found at
startup, previous self-init was interrupted.

Source: https://github.com/openbao/openbao/pull/2442

### Failures not fatal (issue #2190, open)

If self-init fails (e.g., bad path in config), a subsequent restart succeeds because the
barrier already exists and `Initialized()` returns true. Server starts in incomplete state.
Fixed by the marker state machine in PR #2442.

Source: https://github.com/openbao/openbao/issues/2190

### HA cluster race (issue #2274, open)

In Kubernetes StatefulSets, follower nodes may self-initialize before `retry_join` succeeds,
creating multiple independent clusters. **Not relevant for forge-metal's single-node
deployment.**

Source: https://github.com/openbao/openbao/issues/2274

## Applicability to forge-metal

This is the most important OpenBao feature for forge-metal's deployment model. Combined with
static seal, it eliminates the manual initialization ceremony entirely:

1. Ansible deploys the unseal key file and HCL config with `initialize` stanzas
2. systemd starts `bao server`
3. First boot: self-init configures everything (audit, auth, policies, secrets engines)
4. Subsequent boots: auto-unseal, skip init
5. No root token ever touches disk or logs
6. The operator authenticates via userpass (password injected via `eval_source = "env"`)

The `eval_source = "env"` mechanism means Ansible can pass the initial admin password via
`Environment=OPENBAO_ADMIN_PASSWORD=...` in the systemd unit (or via a one-time env file),
without it appearing in the HCL config on disk.
