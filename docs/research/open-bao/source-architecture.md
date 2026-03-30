# OpenBao Source Architecture

Repository: https://github.com/openbao/openbao
Language: Go (1.25.6 per `go.mod`)
Binary: `bao` (renamed from `openbao` during Nix build)
License: MPL 2.0

## Entrypoint

`main.go` is minimal -- delegates immediately to the command package:

```go
func main() {
    os.Exit(command.Run(os.Args[1:]))
}
```

## Top-level directory layout

| Directory | Purpose |
|-----------|---------|
| `command/` | CLI commands: `server/`, `agent/`, `proxy/`, all auth/secrets/operator commands |
| `vault/` | Core server logic: `core.go`, `init.go`, `seal/`, `barrier/`, `cluster/`, `routing/`, `identity/`, `tokens/` |
| `physical/` | Storage backends: `raft/`, `postgresql/`, `crosstest/` |
| `builtin/` | Built-in plugins: `credential/` (approle, cert, jwt, etc.), `logical/`, `audit/` |
| `sdk/` | Plugin SDK (v2): `logical`, `physical`, `framework` interfaces |
| `api/` | Go client API library (v2) |
| `helper/` | Internal helpers: `configutil/` (HCL parsing), `namespace/`, `metricsutil/` |
| `http/` | HTTP API handlers |
| `audit/` | Audit subsystem |
| `plugins/` | External plugin infrastructure |
| `ui/` | Web UI (React, migrating from EmberJS) |

## Core struct (`vault/core.go`)

The `Core` struct is the central manager. It holds references to:

- `physical.Backend` -- storage
- `barrier.SecurityBarrier` -- encryption layer over storage
- `routing.Router` -- mount management (secret engines, auth methods)
- `Seal` -- unseal mechanism (Shamir, static key, transit, etc.)
- All registered credential, logical, and audit backends

The server starts via `command/server/` which:
1. Parses HCL config into `configutil.SharedConfig`
2. Instantiates `Core` with the configured storage and seal
3. Starts the HTTP listener
4. Begins the unseal process

## Config parsing

`helper/configutil/kms.go` parses `seal` stanzas generically into `KMS` structs with a `Type`
string and `Config` map. The server's `configureSeals` function maps type strings (e.g.,
`"static"`, `"transit"`, `"pkcs11"`) to the corresponding `go-kms-wrapping` wrapper
implementations.

## API compatibility with Vault

All API routes are prefixed with `/v1/`. The HTTP header accepts both `X-Vault-Token`
(backwards compat) and `X-Bao-Token`. Environment variables use `BAO_` prefix (`BAO_ADDR`,
`BAO_TOKEN`, `BAO_CACERT`). Most Vault client libraries work with minimal changes (swap the
address and token env vars).

## Plugin architecture

Built-in plugins live in `builtin/`. The plugin interface is defined in `sdk/` and allows
external plugins via gRPC. OpenBao 2.5 added OCI-based plugin distribution -- plugins can be
pulled from container registries declaratively.
