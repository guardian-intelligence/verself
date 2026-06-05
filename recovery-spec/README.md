# Recovery Spec

`recovery-spec` defines small, versioned resources used by Nomad recovery tasks.
The first supported resource is `CloudflareRecovery`: it gives the Cloudflare
integration task exactly the provider state it must converge and the local
secret file it must use to prove account authority.

The contract is intentionally narrow. New fields should be added only when a
component reads them during recovery.
