# Go API Client

Package: `github.com/openbao/openbao/api/v2`
Version: v2.5.1 (February 2026)
License: MPL-2.0
Docs: https://pkg.go.dev/github.com/openbao/openbao/api/v2

## Core types

```go
// Create client
config := api.DefaultConfig()                // reads BAO_ADDR, BAO_TOKEN, etc.
config.Address = "https://openbao.example.com:8200"
client, err := api.NewClient(config)

// Token management
client.SetToken("s.xxxxxxxxx")
client.Token()
client.ClearToken()

// Sub-clients
client.Logical()       // *Logical -- generic read/write/delete/list
client.Sys()           // *Sys -- system operations (mount, seal, policy, audit)
client.Auth()          // *Auth -- authentication
client.KVv1("secret")  // *KVv1 -- KV v1 convenience methods
client.KVv2("secret")  // *KVv2 -- KV v2 convenience methods
client.SSH()           // *SSH -- SSH signing
```

## AppRole login in Go

```go
import (
    "context"
    "log"

    "github.com/openbao/openbao/api/v2"
    "github.com/openbao/openbao/api/auth/approle/v2"
)

func main() {
    config := api.DefaultConfig()
    config.Address = "http://127.0.0.1:8200"
    client, err := api.NewClient(config)
    if err != nil {
        log.Fatal(err)
    }

    secretID := &approle.SecretID{
        FromFile: "/etc/openbao/secretid",
        // or FromEnv: "OPENBAO_SECRET_ID"
        // or FromString: "plaintext-secret-id"  (least secure)
    }

    auth, err := approle.NewAppRoleAuth(
        "db02de05-fa39-4855-059b-67221c5c2f63", // roleID
        secretID,
        // approle.WithWrappingToken(),           // for response-wrapped secret IDs
        // approle.WithMountPath("auth/approle"),  // custom mount
    )
    if err != nil {
        log.Fatal(err)
    }

    secret, err := client.Auth().Login(context.Background(), auth)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Authenticated. Token accessor: %s", secret.Auth.Accessor)
    // client is now authenticated -- token is set automatically
}
```

`WithWrappingToken()` is the recommended pattern -- indicates the SecretID file contains a
one-time-use response-wrapping token rather than a raw secret ID.

## KV v2 operations

```go
ctx := context.Background()
kv := client.KVv2("secret")  // mount path

// Write
_, err = kv.Put(ctx, "my-app/config", map[string]interface{}{
    "db_host":     "postgres.internal",
    "db_password": "hunter2",
})

// Read
secret, err := kv.Get(ctx, "my-app/config")
password := secret.Data["db_password"].(string)

// Read specific version
secret, err = kv.GetVersion(ctx, "my-app/config", 2)

// Partial update
_, err = kv.Patch(ctx, "my-app/config", map[string]interface{}{
    "db_password": "new-password",
})

// Delete
_, err = kv.Delete(ctx, "my-app/config")

// Rollback to version 1
_, err = kv.Rollback(ctx, "my-app/config", 1)

// Get metadata (versions, timestamps)
meta, err := kv.GetMetadata(ctx, "my-app/config")
```

## Generic Logical API

Works with any secret engine:

```go
// Read
secret, err := client.Logical().ReadWithContext(ctx, "secret/data/my-app/config")
data := secret.Data["data"].(map[string]interface{})

// Write
_, err = client.Logical().WriteWithContext(ctx, "transit/encrypt/my-key",
    map[string]interface{}{
        "plaintext": base64.StdEncoding.EncodeToString([]byte("hello")),
    })

// List
secret, err = client.Logical().ListWithContext(ctx, "secret/metadata/")
keys := secret.Data["keys"].([]interface{})
```

## Vault client compatibility

OpenBao's Go client is a **fork**, not a drop-in import replacement. You import
`github.com/openbao/openbao/api/v2` instead of `github.com/hashicorp/vault/api`.

However, the **wire protocol is identical** -- same HTTP endpoints, same request/response
formats. The HashiCorp Vault Go client can be pointed at an OpenBao server and it works at
the HTTP level.

**Token format difference:** Vault uses `hvs.<long_random>`, OpenBao uses `s.<random>`
(shorter). Newly issued tokens have the OpenBao format. Legacy Vault tokens accepted until
expiry.

## Applicability to forge-metal

The Go API client is relevant for the CI orchestrator (`internal/` packages). The orchestrator
needs to:

1. Authenticate with AppRole to get a privileged token
2. Generate per-job SecretIDs (response-wrapped)
3. Read secrets needed for provisioning (Cloudflare token, Latitude.sh token)

This replaces the current pattern of shelling out to `sops -d` for secret access. The API
client runs in-process, handles token renewal, and provides type-safe access to secrets.

```go
// Example: orchestrator generates wrapped SecretID for a CI job
secret, err := client.Logical().WriteWithContext(ctx, "auth/approle/role/ci-runner/secret-id",
    nil,
    api.WithWrapTTL("60s"),
)
wrappingToken := secret.WrapInfo.Token
// Write wrappingToken to the zvol clone for the Firecracker VM
```
