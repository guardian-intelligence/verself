# Convergence Inventory

This inventory records the current gamma recovery state for each component that
has a component CRD in the Guardian graph. A component is converged when the
boarded graph is current, the component-owned Nomad job is healthy, and two
consecutive submissions do not create unexpected allocation churn.

## Gamma

| Component | CRD | Current State | Blocking Conditions | Dependencies Needed For Convergence |
| --- | --- | --- | --- | --- |
| Substrate boarding | `substrate.guardianintelligence.org/v1alpha1/Substrate/gamma-primary` | Converged | None | SSH access, local build artifacts, upload/extract/verify hooks |
| Nomad | `nomad.guardianintelligence.org/v1alpha1/NomadCluster/nomad` | Converged | None | Boarded repo, Nomad runtime artifact, root access for systemd and host config |
| OpenBao | `openbao.guardianintelligence.org/v1alpha1/OpenBaoCluster/openbao` | Blocked | `RootTrustMaterialAvailable=False`, `OpenBaoRecoveryComplete=False` | PGP recipient identities for fresh initialization, or matching unseal/recovery material for an existing or restored store |
| Cloudflare Control Plane | `cloudflare.guardianintelligence.org/v1alpha1/CloudflareControlPlane/gamma-cloudflare` | Declared, not submitted | `CloudflareAccountAuthorityAvailable=False`, `CloudflareRuntimeSecretStoreAvailable=False`, `CloudflarePublicTLSMaterialReady=False` | Operator-provided Cloudflare account-admin token, recovered OpenBao runtime secret store |
| HAProxy | `haproxy.guardianintelligence.org/v1alpha1/HAProxyGateway/public-edge` | Auto-reverted to board-only allocation | `PublicTLSCertificateMaterialAvailable=False` | Public certificate files for `gamma.verself.sh` and `gamma.guardianintelligence.org` |
| Object Storage Service | `objectstorage.guardianintelligence.org/v1alpha1/ObjectStorageService/object-storage` | Declared, not yet submitted | Not evaluated by component job | OpenBao-backed R2 credentials, PostgreSQL, ClickHouse, SPIFFE |

## Latest Gamma Evidence

Live command:

```sh
guardian fly src/guardian-specification/examples/gamma/gamma.cue -o json --stream
```

Observed results:

- boarding verified the extracted repo tree on gamma;
- the boarded repo contains `.guardian/fly/document.json` with Cloudflare and
  HAProxy component CRDs;
- remote Nomad validation succeeds for the updated HAProxy and Cloudflare job
  files;
- Nomad is running and reachable on gamma;
- OpenBao reported an uninitialized Shamir store and blocked on missing PGP
  recipient identities;
- Cloudflare is declared in the boarded graph but cannot recover runtime
  secrets until OpenBao and operator provider authority are available; the
  Cloudflare account-admin token is absent on gamma;
- submitting the updated HAProxy job exercises the component-owned prestart and
  fails on missing `/etc/haproxy/certs/gamma.guardianintelligence.org.pem`;
- Nomad auto-reverts HAProxy to the previous board-only allocation after the
  failed public-edge update;
- component truth comes from Nomad status, component report files, service
  health checks, and telemetry rather than Guardian command output.

Evidence commands:

```sh
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default openbao'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default cloudflare-integration-recovery'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default haproxy-upstreams'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/haproxy/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/integrations/cloudflare/control-plane/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc logs -address=http://127.0.0.1:4646 -namespace=default -stderr -task setup 4bdf671f'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/openbao/report.json'
ssh -T ubuntu@206.223.228.87 'sudo test -f /run/verself/recovery/credentials/cloudflare-account-admin-api-token && echo cloudflare-token:present || echo cloudflare-token:missing'
ssh -T ubuntu@206.223.228.87 'for p in /etc/haproxy/certs/gamma.verself.sh.pem /etc/haproxy/certs/gamma.guardianintelligence.org.pem; do sudo test -f "$p" && echo present:$p || echo missing:$p; done'
```

## Next Component

The next recovery target after Cloudflare is `object-storage-service`. It should
remain blocked until Cloudflare-generated R2 runtime credentials are persisted in
OpenBao and PostgreSQL, ClickHouse, and SPIFFE are available through their owning
recovery paths.
