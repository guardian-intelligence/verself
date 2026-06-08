# RFC: Disaster Recovery Bootstrap Trust

Status: Draft

This RFC defines how trust enters the system during recovery: how the first
credential is established, how a reimaged node or a new site obtains the seed it
needs before its own vault is available, and how an operator obtains temporary
cross-environment authority to stand up or recover a site. It complements
`disaster-recovery-reference.md`, which defines the steady-state reconciler
contract. That reference notes that "a transient operator token used during fresh
bootstrap is revoked before recovery reports complete"; this RFC specifies how
that transient authority is obtained, scoped, and audited.

The governing priority is low-touch recovery. Bootstrapping from zero and
recovering from disaster are priority one after maintaining ongoing security, so
the number of secrets a human must supply to recover is minimized and every
remaining human-supplied secret is reduced to a single hardware- or
identity-rooted authority.

## Trust Tiers

Recovery is organized as a trust hierarchy. The discipline is to make Tier 0 as
small as possible and to make Tier 1 cascade from it without operator action.

- **Tier 0 — root of trust.** The minimal authority that unlocks everything else.
  In this design it is the controller OpenBao, auto-unsealed from a node TPM root
  of trust, with an offline Shamir/PGP quorum as its breakglass.
- **Tier 1 — operational secrets.** Everything OpenBao holds per site: DNS tokens,
  Stripe keys, GitHub App private material, object-storage credentials. Unlocked
  by Tier 0, delivered to workloads dynamically through Nomad's vault integration,
  never resident on workload disk.

Critical third-party keys such as the DNS provider token are Tier 1. They are
imported into the site's OpenBao from the provider control plane, scoped to least
privilege, and read at runtime. They are needed during `fly` (to repoint records
and run ACME), which happens after the site's vault is unsealed, so they are never
part of the bootstrap chicken-and-egg.

## Recovery Regimes

Recovery splits into two regimes on a single assumption: at least one trusted
control plane node in the relevant trust domain is up.

### Warm DR — a peer is available

Node replacement, single-node loss, and reimage are the common incidents. A
recovering node rejoins through attested membership rather than by copying
credentials.

1. The joining node proves its identity by node attestation (TPM measurement or
   the SPIRE node-attestation path already run per site).
2. The live control plane authorizes the join and issues the joiner a scoped,
   short-lived SVID.
3. The joiner runs `raft join` against a live peer. The OpenBao integrated-storage
   store replicates as ciphertext; the joiner unseals locally through the shared
   seal authority. No plaintext secret is exported from one node to another, and
   no operator secret is entered.

Warm DR is intra-trust-domain. A `gamma` node recovers from a `gamma` peer and a
`prod` node from a `prod` peer, consistent with each site being its own SPIFFE
trust domain and with `secrets-and-integrations.md` keeping prod runtime
credentials out of lower environments.

The mechanism is the OpenBao HA integrated-storage join model: a new node catches
up from the cluster and auto-unseals through the configured seal, so the operator
re-enters nothing.

### Cold start — no peer is available

Genesis (the first node of the system) and simultaneous total loss of a trust
domain require Tier 0. This regime is rare and tolerates higher touch. It runs the
bootstrap trust ceremony defined below.

The "at least one node up" assumption holds only at two or more control plane
nodes per site. At the current single-node-per-site topology every site-node loss
is a cold start, so the Tier-0 path is exercised on every recovery until the
three-node control plane in the target topology exists. The design targets the
three-node quorum; the cold-start root is retained until that quorum is in place.

## The Controller Authority

The controller OpenBao is the seal root and the bootstrap issuer for the fleet. It
is the `controller_openbao` authority already defined in
`secrets-and-integrations.md` as the pre-host source of truth for provider handoff
and first-host recovery inputs. It is a dedicated instance in its own controller
trust domain, not a namespace co-tenant with any site vault, so its seal and blast
radius are isolated from the customer-facing surfaces it unlocks. It co-locates on
a control-plane node at the current scale and runs as a three-node Raft cluster at
the target topology. Unifying the seal root and the bootstrap issuer concentrates
the high-value surface in one hardened component rather than spreading standing
credentials across environments.

- **Seal authority.** It provides the OpenBao Transit auto-unseal key that unseals
  every site's vault. Transit unseal wraps and unwraps the seal key; the controller
  never reads a site's operational secrets through this role. Warm-DR joiners
  auto-unseal against it without operator action.
- **Bootstrap issuer.** It mints, just in time, the scoped temporary authority a
  `preflight` run needs to seed or recover a target.

### The seal chain has no third party

The unseal chain is self-hosted end to end, low-touch in the steady state, and
roots no trust or control in a third party.

```text
site vaults        --Transit auto-unseal-->  controller OpenBao   (low touch, self-hosted)
controller OpenBao --auto-unseal-->          node TPM root of trust (low touch, self-hosted)
total controller loss / genesis --breakglass--> offline Shamir/PGP quorum
```

Site vaults auto-unseal against the controller. The controller auto-unseals from
the node's TPM 2.0 root of trust, the same hardware root used by the attested-build
ceremony, so a controller restart is touchless and depends on no external key
service. The offline Shamir/PGP quorum, modeled by the existing `PGPRecipient`
holders, is the single irreducible Tier-0 secret and is used only at genesis and to
recover the controller after total loss. Cloud KMS is excluded as a seal root
because it places trust and control in a third party on the critical recovery path.

The controller's TPM root is an OpenBao-native PKCS#11 seal backed by the node's
TPM 2.0 through a pinned `tpm2-pkcs11` provider. A tracer bullet established that
the official OpenBao `2.5.2` release the repo currently consumes as a prebuilt
`.deb` is built `CGO_ENABLED=0` without the `hsm` tag and rejects a `pkcs11` seal
with "this build of OpenBao has PKCS#11 disabled"; the `transit` seal that backs
the site-to-controller link is present in that same build. PKCS#11 is gated behind
the `hsm` build tag and CGO and is available in the OpenBao open-source tree, so
the controller build replaces the prebuilt `.deb` with a source build carrying
`-tags=hsm` and `CGO_ENABLED=1` linked against a pinned `tpm2-pkcs11`. Building
OpenBao from source through Bazel also aligns the component with the repo's
build-every-byte and commit-pin contract.

A tracer bullet built this variant and proved a killed-then-restarted controller
auto-unseals against the node TPM with no operator interaction. On the validated
TPM the `tpm2-pkcs11` provider rejects `CKM_AES_GCM`, so the seal uses
`RSA_PKCS_OAEP` (`mechanism 0x0009`, `rsa_oaep_hash sha256`) against an RSA-2048
token key, and the TPM token and key are provisioned before `operator init`.

A TPM-sealed Shamir auto-feed was considered as a no-rebuild alternative and
rejected: it makes Shamir the steady-state restart path with one set of shares
serving as both the normal unseal material and the breakglass material, which
contradicts the autonomous-actuator and Shamir-is-breakglass posture in
`disaster-recovery-reference.md`. A native PKCS#11 seal keeps the two separate. The
TPM key is the normal seal actuator, and `operator init` under an auto-unseal seal
emits recovery keys that are PGP-wrapped to the `PGPRecipient` holders as the
offline breakglass quorum. This shifts the `openbao` CRD from a `seal.shamir`
stanza to a `seal.pkcs11` stanza with a recovery configuration; the PGP recipients
carry over unchanged.

## Component Binary Dependencies

The PKCS#11 controller introduces dependencies that the coding contract requires to
be Bazel-owned and installed as repo-built artifacts by the preflight playbook
rather than host-installed. They divide into three roles with distinct lifecycles
and placement.

| Dependency | Role | Lifecycle | Placement |
| --- | --- | --- | --- |
| `bao` built `-tags=hsm CGO_ENABLED=1` | seal consumer | runtime, fleet-wide | the existing openbao build target, changed from a prebuilt `.deb` to a cgo source build |
| `libtpm2_pkcs11.so` and its closure (`tpm2-tss`, `libcrypto`, `sqlite3`) | the PKCS#11 provider the process `dlopen`s at unseal | runtime, controller only | a component-owned runtime artifact bundled into the openbao runtime tar |
| `tpm2_ptool` and the tpm2-pkcs11 bindings | provisions the TPM token and key | genesis only, before `operator init` | a component-owned bootstrap tool, controller only |

Build time and runtime are separable. Building `bao` needs only the Go toolchain, a
C compiler, and glibc; the `pkcs11` wrapper links `miekg/pkcs11`, which `dlopen`s
the provider at runtime, so the tpm2 libraries are absent from the build graph. The
tpm2-pkcs11 closure is a pure runtime dependency loaded from the `lib` path in the
seal stanza, and it is required only on the controller, because site vaults use the
`transit` seal and never load the module. A single `hsm` binary therefore ships
fleet-wide with the provider closure present only on the controller.

Packaging the C closure is the substantive work, with two approaches. A hermetic
source build of `tpm2-pkcs11` and `tpm2-tss` from pinned tarballs through
`rules_foreign_cc` matches the build-every-byte contract and is the recommended
target. Pinned `.deb` extraction of `libtpm2-pkcs11-1` and the `libtss2` closure by
digest, bundled into the runtime tar, is a faster interim that validates the full
controller wiring before the source build lands and is reversible. A pure-Go seal
backed by `go-tpm`, avoiding PKCS#11 and the C closure entirely, was considered and
rejected because it places hand-maintained cryptography in the seal path of the most
security-critical component, against the preference for battle-tested standard
mechanisms.

The recommendation is the source build as the target, with the pinned-`.deb` interim
used only if the controller wiring needs validation first. The genesis ceremony
provisions the TPM token with `tpm2_ptool` before `operator init`, and the OpenBao
process needs access to `/dev/tpmrm0`, granted by a `tss` group or by running as
root on hosts where the device node is `root`-owned.

## The Bootstrap Trust Ceremony

`preflight` obtains temporary cross-environment authority through a just-in-time,
operator-mediated, audited ceremony. This is privileged-access break-glass built
on secure introduction. It preserves runtime isolation: the standing property
that a lower environment's workloads cannot reach prod is untouched; the ceremony
opens a human-gated, self-expiring channel only at recovery time.

1. **Operator authentication.** The operator authenticates as a human through
   Zitadel OIDC with MFA. The trust anchor is the operator's identity, not a
   secret resident in the environment being recovered.
2. **Authorization.** iam-service and SpiceDB answer whether this operator may
   bootstrap this target. The grant is a Zanzibar relation on a human identity.
3. **Tiered authorization.** Prod bootstrap requires a two-of-N Control Group
   approval drawn from the SpiceDB `prod-bootstrap-approver` role, with separation
   of duties so the requester cannot self-approve. Gamma and dev bootstrap require
   a single authenticated operator and no second approver. Rigor applies where
   blast radius warrants it; lower environments stay low-touch.
4. **Scoped, wrapped, expiring issuance.** The controller issues a
   response-wrapped, single-use, short-TTL token scoped to the minimal capability
   subset the detected case needs, from three distinct bootstrap capabilities:
   `snapshot-read` (read-only on `verself-recovery/<site>/...`), `seal-unwrap-init`
   (unseal or initialize the target's vault), and `mint-down` (create new
   site-scoped credentials). A restore grants `snapshot-read` and `seal-unwrap-init`
   and never `mint-down`; a fresh standup grants `mint-down` and `seal-unwrap-init`
   and never `snapshot-read`. The scope always excludes the target's running service
   secrets.
5. **Single unwrap and seed.** `preflight` unwraps once and seeds the node, then
   the token expires. Response wrapping makes interception tamper-evident: an
   intercepted token that is unwrapped in transit causes the legitimate unwrap to
   fail, so a leak is detected rather than silent.
6. **Audit.** governance-service records the ceremony as OCSF events on its
   tamper-evident HMAC chain. Every break-glass issuance is evidence.

### Scoping discipline

Temporary access means a short-lived token scoped to the bootstrap set, not a copy
of a site's master. Least privilege applies more strictly during break-glass. The
bootstrap scope reads the target's snapshot, unwraps the seal, and mints new
site-scoped credentials. It does not read every service's runtime secrets. This
single constraint keeps a compromise on isolation from becoming an abandonment of
it.

### Cross-environment flow is mint-down

A node belongs to exactly one site. Standing up a prod node uses prod's own seed,
which is intra-domain. Standing up a node in another site yields that site's own
seed or freshly minted site-scoped material; `preflight` reaches the management
authority, which returns the target's seed or mints down into the new site. The
controller authority issues new scoped credentials rather than handing over a
site's existing runtime secrets, so a compromise of the issuer permits minting,
which is loud and revocable, rather than silent reads of live traffic.

## Seed Acquisition Before The Vault Is Up

Restoring a vault requires reading its encrypted snapshot from object storage, but
the object-storage credential ordinarily lives in that vault. The ceremony breaks
this circularity: the bootstrap token's object-storage-read scope, issued by the
controller authority and gated on attestation or operator identity, authorizes the
snapshot read before the target vault exists. The snapshot itself is the durable
artifact that survives total wipe and is modeled by `snapshots.restore` on
`OpenBaoCluster`. Restore-versus-fresh-init is then decided by observed storage,
per the reconciler contract.

## Relationship To The Keyring

Operator-supplied provider keys do not need a per-site keyring API. The bootstrap
ceremony replaces it for the new-node and new-site cases: `preflight` fetches a
scoped, expiring, wrapped token rather than an operator pasting raw provider keys.
Two residual surfaces remain for stored long-lived provider material:

- **Federate what the provider supports.** Cloud roots (AWS, GCP, Azure) and
  OpenBao itself use OIDC or X.509 workload-identity federation through the SPIRE
  trust domain, so no long-lived key is stored. JWT-SVID exchanges for
  `AssumeRoleWithWebIdentity` and GCP Workload Identity Federation; X.509-SVID
  exchanges for AWS IAM Roles Anywhere.
- **Genesis import for the non-federatable tail.** Providers with no federation —
  the DNS provider, the registrar, Stripe, Resend, GitHub App keys — are imported
  once into the relevant vault at genesis and rotated thereafter. This is the only
  irreducible high-touch moment and is acceptable as a one-time ceremony.

## Build Versus Buy

Every primitive required already runs in the repo, so this is wiring rather than
new infrastructure: Zitadel for operator authentication and MFA, iam-service and
SpiceDB for authorization, OpenBao response wrapping and Control Groups for
just-in-time scoped delivery and dual authorization, governance-service for
tamper-evident OCSF audit, SPIRE for node attestation and the attested-join
identity, and OpenBao integrated storage for warm-DR replication and auto-unseal.

No external secret manager is introduced. OpenBao is the only component that
natively provides root-mints-children dynamic issuance and is already the
preflight root service. Adopting a Kubernetes-shaped credential model
(External Secrets Operator, Crossplane) or a hosted store (Doppler, Pulumi ESC,
Infisical) would either require Kubernetes, duplicate OpenBao, or add a hosted
dependency to the critical recovery path.

## Decisions

### Warm DR joins through attestation and replicates ciphertext

A recovering node proves identity by attestation and joins the Raft cluster; the
encrypted store replicates and the node unseals locally. No endpoint exports
plaintext secrets between nodes, which removes a bulk-export attack surface and
keeps the audit story clean.

### A single controller authority is both seal root and bootstrap issuer

Concentrating the seal root and the bootstrap issuer in one hardened, offline,
dual-authorized authority is preferable to distributing standing cross-environment
credentials. The authority can unseal and mint but does not read a site's
operational secrets through those roles.

### Cross-environment bootstrap is just-in-time, scoped, and audited

Temporary authority is issued per ceremony, scoped to the bootstrap set, wrapped
for tamper-evidence, gated by operator identity and dual authorization for prod,
and recorded as audit evidence. Standing cross-environment runtime credentials are
not used.

### Tier 0 collapses to one offline quorum

The controller authority is the only component sealed by human-held material, and
that material is an offline Shamir/PGP quorum used at genesis and for recovering
the authority itself. Site vaults auto-unseal through the controller authority; the
operator supplies no per-site unseal material.

## Anti-patterns

- A standing cross-environment credential that a lower environment's workloads
  hold continuously. The ceremony is operator-mediated, scoped, and expiring.
- A bulk plaintext-export endpoint used to seed a recovering node. Warm DR
  replicates ciphertext and unseals locally.
- A bootstrap grant scoped to a site's full secret set rather than the bootstrap
  set. Break-glass is least-privilege.
- Operator-typed unseal shares as the steady-state restart path. Shamir is
  breakglass for the controller authority only; site vaults auto-unseal.
- A cross-environment seed that copies a site's existing runtime secrets rather
  than minting new scoped material down into the target.

## Requirements Traceability

| Requirement | Mechanism |
| --- | --- |
| Low-touch recovery | Warm DR auto-unseals through the controller authority with no operator secret; Tier 0 collapses to one offline quorum. |
| Critical third-party keys available | Tier-1 import into the site vault, least-privilege scope, delivered at runtime. |
| Temporary cross-environment bootstrap | Just-in-time response-wrapped, scoped, dual-authorized, audited ceremony in `preflight`. |
| Isolation preserved under compromise | Standing runtime isolation untouched; ceremony is human-gated and expiring; cross-environment flow mints down rather than reading across. |
| Survives total wipe | Encrypted snapshot in object storage plus the controller authority sealed by an offline quorum. |

## Resolved Decisions

### Controller placement and trust domain

The controller is a dedicated OpenBao instance in its own controller trust domain,
not the prod site vault carrying a management role and not a namespace co-tenant
with a site. Prod-carried would make the customer-facing, highest-exposure surface
the crown-jewel seal root and couple every site's unseal to prod. A dedicated
controller isolates the crown jewel and decouples site availability. It co-locates
at the current scale and runs as three-node Raft at the target.

### Bootstrap capabilities are three distinct scopes

The bootstrap grant is the minimal subset of `snapshot-read`, `seal-unwrap-init`,
and `mint-down` for the detected case. Recovering an existing site reads its
snapshot and unseals and is never granted `mint-down`; standing up a new site mints
scoped credentials and is never granted `snapshot-read`. The case is selected by
observed storage, consistent with the reconciler's restore-versus-fresh-init rule.

### Controller unavailability defers rather than caches

A site does not cache a local copy of its master to ride out a controller outage,
because that widens where the seal key resides and undermines the single-seal-
authority property. The controller is made HA (three-node Raft) so brief
unavailability is rare; a full controller-quorum outage is a cold-start-class event
during which site unseal defers. Recovery is ordered controller-first
(TPM auto-unseal, or Shamir breakglass) then sites.

### Tiered Control Group approval

Prod bootstrap requires two-of-N approval from the SpiceDB `prod-bootstrap-approver`
role with separation of duties; gamma and dev require a single authenticated
operator. Two-person integrity applies where blast radius warrants it; lower
environments stay low-touch.

### Controller genesis ceremony

On a clean node, `bao operator init` emits a root token and PGP-wrapped recovery
shares distributed to the offline quorum holders. The root token is used in process
memory only to configure TPM auto-unseal, base SPIFFE and JWT auth, audit sinks, the
bootstrap-issuer policies and Control Groups, and to import the genesis
bootstrap-exception roots (the Cloudflare account-admin and Latitude credentials).
The root token is then revoked and discarded, never written to disk, git, or logs,
per the transition rules in `secrets-and-integrations.md`. Thereafter the controller
auto-unseals via TPM and the Shamir quorum is breakglass only. A quarterly gameday
exercises both controller genesis and Shamir-breakglass recovery to keep quorum
custody and the runbook valid.

### Stripe Projects is provisioning-plane only

Stripe Projects provisions third-party providers and hands off their credentials
locally; its `env --pull` writes to a local file and is not a production secret
distribution system. It is used in the provisioning plane to provision and rotate
integrations, after which an authenticated import writes catalog-approved names into
OpenBao, which is the runtime truth. It is never on the recovery critical path and
is never a root of trust, because it places a third party in the control path that
the no-third-party-root requirement excludes. The genesis bootstrap-exception roots are resident in the
controller, never re-pulled from Stripe during recovery. The `provider_project`
storage class in `secrets-and-integrations.md` already encodes this as local handoff
that no Nomad job consumes.

## Validated Hardware

The seal mechanism was validated on the two production-class nodes. Both are
Latitude bare metal with identical CPU, memory, and OS, and a discrete TPM 2.0.

| Node | Role | CPU | RAM | OS / kernel | glibc | TPM |
| --- | --- | --- | --- | --- | --- | --- |
| `vs-gamma-w0` | gamma | AMD EPYC 4484PX (24 threads) | 93 GiB | Ubuntu 24.04.4 / 6.8.0-124 | 2.39 | Infineon SLB9672, fw `0x0010000D`/`0x00454500`, spec rev 1.59 |
| `vs-dev-w0` | prod | AMD EPYC 4484PX (24 threads) | 93 GiB | Ubuntu 24.04.4 / 6.8.0-111 | 2.39 | TPM 2.0 present; manufacturer and firmware not read (no tpm2 tooling on prod) |

The RSA-OAEP requirement is a property of the Infineon SLB9672 firmware on
`vs-gamma-w0`, which rejects `CKM_AES_GCM`. The prod node carries the same board and
CPU class, so its TPM is very likely the same part, but its firmware is unconfirmed;
the seal mechanism must be re-confirmed on the prod TPM before the controller seal
config is relied on there, because the AES-GCM-versus-RSA-OAEP behavior is
firmware-specific. The cgo `bao` binary is portable across both nodes without
rebuild because they share Ubuntu 24.04.4 and glibc 2.39.

## Tracer Bullets

- Done: the pinned official OpenBao `2.5.2` `.deb` is `CGO_ENABLED=0`, built without
  the `hsm` tag, and rejects a `pkcs11` seal ("this build of OpenBao has PKCS#11
  disabled"); `transit` seal is present. The box exposes a TPM 2.0 at `/dev/tpm0`.
  Outcome: the controller's TPM root requires a source build of OpenBao with
  `-tags=hsm CGO_ENABLED=1` against a pinned `tpm2-pkcs11`, replacing the `.deb`.
- Done: OpenBao `2.5.2` rebuilt from source with `-tags=hsm CGO_ENABLED=1` (a
  dynamically-linked cgo binary pulling `go-kms-wrapping/wrappers/pkcs11`), wired to
  a `pkcs11` seal backed by the node TPM through `tpm2-pkcs11` 1.9.0, initialized,
  then killed and restarted with no operator interaction. The restart auto-unsealed
  ("core: unsealed with stored key", `Sealed false`) and prior secrets survived.
  This confirms a self-hosted, TPM-rooted, touchless controller unseal. Three
  findings constrain the implementation: this TPM's `tpm2-pkcs11` rejects
  `CKM_AES_GCM` (`CKR_MECHANISM_INVALID`), so the seal uses `RSA_PKCS_OAEP`
  (`mechanism 0x0009`, `rsa_oaep_hash sha256`) with an RSA-2048 token key;
  `tpm2-pkcs11` requires the token and key to exist before `operator init`, so the
  genesis ceremony provisions the TPM token first; and the OpenBao process needs
  access to `/dev/tpmrm0` (the `tss` group) and the `TPM2_PKCS11_STORE` token path.
  `operator init` under the auto-unseal seal emitted recovery keys with recovery
  seal type `shamir`, confirming the PGP-wrapped breakglass model carries over.
  The same procedure was repeated on the gamma node (`vs-gamma-w0`) against its own
  TPM, isolated from the running gamma OpenBao on a separate port, and produced an
  identical touchless auto-unseal with the same RSA-OAEP requirement; the node was
  left with its TPM persistent handles restored and the running OpenBao untouched.
  The mechanism is confirmed across two distinct TPMs.
- Next: package this as the controller build through Bazel — pin `tpm2-pkcs11`,
  `tpm2-tss`, and their C dependencies hermetically, and replace the `.deb`
  consumption in `src/infrastructure-components/openbao`.
- Next: a controller-then-site auto-unseal rehearsal — kill the controller quorum,
  recover it from the TPM seal, confirm sites Transit-auto-unseal against it without
  operator material.
- Next: a response-wrapped, two-of-N Control Group prod bootstrap issuance end to
  end, with the tamper-evident unwrap-failure path exercised and OCSF evidence in
  ClickHouse.

## Prior Art

- OpenBao and Vault integrated storage and auto-unseal:
  https://developer.hashicorp.com/vault/docs/concepts/integrated-storage and
  https://developer.hashicorp.com/vault/docs/concepts/seal#auto-unseal
- Response wrapping for secure introduction:
  https://developer.hashicorp.com/vault/docs/concepts/response-wrapping
- Control Groups for dual authorization:
  https://developer.hashicorp.com/vault/docs/enterprise/control-groups
- ACME and DNS-01 challenge delegation: https://datatracker.ietf.org/doc/html/rfc8555
- OAuth 2.0 Token Introspection as the conceptual model for liveness checks:
  https://datatracker.ietf.org/doc/html/rfc7662
- Workload identity federation for keyless provider access: AWS
  `AssumeRoleWithWebIdentity` and IAM Roles Anywhere
  (https://docs.aws.amazon.com/rolesanywhere/latest/userguide/trust-model.html)
  and GCP Workload Identity Federation
  (https://cloud.google.com/iam/docs/workload-identity-federation)
