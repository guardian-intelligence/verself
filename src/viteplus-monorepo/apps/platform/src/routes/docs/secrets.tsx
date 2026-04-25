import { createFileRoute } from "@tanstack/react-router";

import {
  DefinitionCard,
  DefinitionGrid,
  Prose,
  SectionHeading,
  SubHeading,
  SummaryItem,
  SummaryPanel,
} from "~/features/policy/policy-primitives";

export const Route = createFileRoute("/docs/secrets")({
  component: SecretsDocs,
  head: () => ({
    meta: [
      { title: "Secrets & Keys — Verself Platform" },
      {
        name: "description",
        content:
          "Store sensitive values, manage encryption keys, and inject both into Verself sandbox workloads.",
      },
    ],
  }),
});

function SecretsDocs() {
  return (
    <article className="flex flex-col gap-10 [&_h2]:scroll-mt-[var(--header-scroll-offset)] [&_h3]:scroll-mt-[var(--header-scroll-offset)]">
      <header className="flex flex-col gap-4 border-b border-border pb-8">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Platform Docs
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">Secrets &amp; Keys</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          Store sensitive values, manage encryption keys, and inject both into every Verself sandbox
          workload.
        </p>
      </header>

      <Overview />
      <SharedResponsibility />
      <Secrets />
      <Keys />
      <Sandboxes />
      <AccessControl />
      <AuditTrail />
      <Limits />
    </article>
  );
}

function Overview() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="overview">Overview</SectionHeading>
      <Prose>
        <p>
          Verself ships two related services for handling sensitive material.{" "}
          <strong>Secrets</strong> store values your code needs but shouldn't commit to source
          control — API tokens, database passwords, webhook signing secrets, TLS private keys.{" "}
          <strong>Keys</strong> hold cryptographic material your code uses for encrypt, decrypt,
          sign, and verify operations. If you've used AWS Secrets Manager and AWS KMS, most of the
          vocabulary on this page is the same.
        </p>
        <p>
          Both services share one access-control model, one audit trail, and one injection path into
          sandbox workloads, so what you learn about one applies to the other.
        </p>
      </Prose>
      <SummaryPanel>
        <SummaryItem term="Secrets">
          Versioned key-value storage with scoping, rotation, and per-resource grants.
        </SummaryItem>
        <SummaryItem term="Keys">
          Symmetric and asymmetric keys for encrypt/decrypt, sign/verify, and envelope encryption.
        </SummaryItem>
        <SummaryItem term="Scopes">
          Organization, source, environment, and branch — most-specific-first resolution.
        </SummaryItem>
        <SummaryItem term="Isolation">
          Values never land on disk outside tmpfs, never cross organizations, never appear in logs.
        </SummaryItem>
        <SummaryItem term="Audit">
          Every read, write, rotation, and key use is recorded and HMAC-chained.
        </SummaryItem>
        <SummaryItem term="Injection">
          Environment variables and <code>/run/secrets/*</code> tmpfs files into every sandbox.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function SharedResponsibility() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="shared-responsibility">Shared responsibility</SectionHeading>
      <Prose>
        <p>
          Verself is responsible for how secrets and keys are stored, delivered, and isolated. You
          are responsible for what your code does with the values after it receives them.
        </p>
      </Prose>
      <DefinitionGrid>
        <DefinitionCard
          term="Verself's responsibility"
          definition={
            <ul className="list-disc space-y-1.5 pl-5">
              <li>Values stored encrypted at rest.</li>
              <li>
                Values delivered to sandboxes only through the process environment and a RAM-only
                tmpfs mount.
              </li>
              <li>
                Values never written to logs, traces, telemetry, audit payloads, error messages, or
                shared sandbox images.
              </li>
              <li>
                Values never cross organization boundaries, regardless of how scopes or names
                overlap between organizations.
              </li>
              <li>Every read, write, and key use recorded in a tamper-evident audit trail.</li>
            </ul>
          }
        />
        <DefinitionCard
          term="Your responsibility"
          definition={
            <ul className="list-disc space-y-1.5 pl-5">
              <li>What your application code does with values after reading them.</li>
              <li>
                Whether your code writes values to files, logs, or persistent volumes inside your
                sandbox.
              </li>
              <li>
                Whether your code forwards values into outbound API calls, webhook bodies, or crash
                reports.
              </li>
              <li>
                If you save a snapshot of a sandbox after your code has written a secret to disk,
                the snapshot carries that secret. Snapshots are scoped to your organization and
                never shared across tenants, but treat them accordingly.
              </li>
            </ul>
          }
        />
      </DefinitionGrid>
    </section>
  );
}

function Secrets() {
  return (
    <section className="flex flex-col gap-6">
      <SectionHeading id="secrets">Secrets</SectionHeading>
      <Prose>
        <p>
          A secret is a named value plus its version history. Updating a secret creates a new
          version; older versions remain addressable within the retention window, so a workload
          already running on the previous version can finish cleanly while new workloads pick up the
          new value.
        </p>
      </Prose>

      <div className="flex flex-col gap-3">
        <SubHeading id="secrets-scopes">Scopes</SubHeading>
        <Prose>
          <p>
            Every secret belongs to one of four scopes. Reads resolve from most specific to least
            specific, so you can use the same name across scopes and let context decide the value.
          </p>
          <ol>
            <li>
              <strong>Branch</strong> — one branch of one repository. Useful for short-lived preview
              and dev work.
            </li>
            <li>
              <strong>Environment</strong> — one environment such as <code>production</code> or{" "}
              <code>staging</code>.
            </li>
            <li>
              <strong>Source</strong> — one code repository.
            </li>
            <li>
              <strong>Organization</strong> — available everywhere in your organization.
            </li>
          </ol>
          <p>
            A read for <code>STRIPE_KEY</code> on the <code>main</code> branch of the{" "}
            <code>checkout</code> repository in the <code>production</code> environment checks
            branch first, then environment, then source, then organization, and returns the first
            match.
          </p>
        </Prose>
      </div>

      <div className="flex flex-col gap-3">
        <SubHeading id="secrets-versions">Versions</SubHeading>
        <Prose>
          <p>
            Every secret has a current version and retains its previous versions for a configurable
            retention window. Reads return the current version by default; callers can request a
            specific version by label (<code>current</code>, <code>previous</code>) or by version
            number.
          </p>
          <p>
            Rotation never takes effect mid-request. A workload that reads <code>STRIPE_KEY</code>{" "}
            at start gets one consistent value for the lifetime of that read.
          </p>
        </Prose>
      </div>

      <div className="flex flex-col gap-3">
        <SubHeading id="secrets-operations">Operations</SubHeading>
        <Prose>
          <p>
            Create or update a secret with <code>PUT /api/v1/secrets/{"{name}"}</code>, read the
            current version with <code>GET</code>, list organization secrets with{" "}
            <code>GET /api/v1/secrets</code>, and delete with <code>DELETE</code>. Mutating calls
            require an <code>Idempotency-Key</code> header; retrying with the same key is safe and
            returns the original result without double-writing. The full surface is documented in
            the <a href="/docs/reference">API reference</a>.
          </p>
        </Prose>
      </div>
    </section>
  );
}

function Keys() {
  return (
    <section className="flex flex-col gap-6">
      <SectionHeading id="keys">Keys</SectionHeading>
      <Prose>
        <p>
          Keys are cryptographic material your code uses for encryption and signing. A secret holds
          a value you read as-is; a key is never read as-is. Your code sends data to Verself and
          receives back ciphertext, plaintext, a signature, or a data key.
        </p>
      </Prose>

      <div className="flex flex-col gap-3">
        <SubHeading id="key-types">Key types</SubHeading>
        <Prose>
          <ul>
            <li>
              <strong>Symmetric keys</strong> — AES-256-GCM for <code>encrypt</code> and{" "}
              <code>decrypt</code>.
            </li>
            <li>
              <strong>Asymmetric signing keys</strong> — Ed25519 or ECDSA P-256 for{" "}
              <code>sign</code> and <code>verify</code>. The public key is fetchable and can be
              handed to external verifiers.
            </li>
            <li>
              <strong>MAC keys</strong> — HMAC-SHA256 for <code>sign</code> and <code>verify</code>{" "}
              when both producer and verifier are yours and a shared secret is acceptable.
            </li>
            <li>
              <strong>Data keys</strong> — <code>generate-data-key</code> returns a fresh symmetric
              key encrypted under your key, for envelope encryption of large payloads you store
              yourself.
            </li>
          </ul>
          <p>
            Key type is fixed at creation. A key created for HMAC cannot later produce asymmetric
            signatures.
          </p>
        </Prose>
      </div>

      <div className="flex flex-col gap-3">
        <SubHeading id="key-versions">Versions and rotation</SubHeading>
        <Prose>
          <p>
            Keys are versioned. Rotating a key creates a new version and makes it current; previous
            versions remain available for decryption and verification of material produced while
            they were current. Ciphertexts and signatures carry an explicit version prefix, so
            rotation never invalidates data at rest.
          </p>
          <p>
            Set a rotation schedule per key (common choices are 90, 180, or 365 days) or rotate on
            demand through the API or the dashboard. Schedules can be paused and resumed without
            losing rotation history.
          </p>
        </Prose>
      </div>

      <div className="flex flex-col gap-3">
        <SubHeading id="key-aliases">Aliases</SubHeading>
        <Prose>
          <p>
            An alias is a human-friendly pointer to a key. Your application references{" "}
            <code>alias/payments-signer</code>; operations can repoint the alias to a new key during
            a planned rotation without code changes. Aliases are organization-scoped and follow the
            same access-control rules as the key they reference.
          </p>
        </Prose>
      </div>
    </section>
  );
}

function Sandboxes() {
  return (
    <section className="flex flex-col gap-6">
      <SectionHeading id="sandboxes">Using secrets in sandboxes</SectionHeading>
      <Prose>
        <p>
          Sandbox workloads — CI jobs, one-off scripts, scheduled tasks, long-running development
          environments — consume secrets without touching storage paths, credentials, or Verself
          APIs directly. You declare what the workload needs; Verself wires it up at sandbox start.
        </p>
      </Prose>

      <div className="flex flex-col gap-3">
        <SubHeading id="sandbox-profiles">Secret profiles</SubHeading>
        <Prose>
          <p>
            A <strong>secret profile</strong> is a named collection of secret references — not the
            values themselves. A <code>dev-env</code> profile might bundle <code>GITHUB_TOKEN</code>
            , <code>STRIPE_TEST_KEY</code>, and <code>OPENAI_API_KEY</code>. Create the profile
            once; attach it to every sandbox that should receive those secrets.
          </p>
          <p>
            Profiles carry their own access-control grants. A member can be allowed to launch
            sandboxes with the <code>dev-env</code> profile without being granted direct{" "}
            <code>read</code> on the underlying secrets.
          </p>
        </Prose>
      </div>

      <div className="flex flex-col gap-3">
        <SubHeading id="sandbox-injection">Injection surfaces</SubHeading>
        <Prose>
          <p>Two surfaces, both materialized at sandbox start:</p>
          <ul>
            <li>
              <strong>Environment variables</strong> — process-local. Your code reads{" "}
              <code>process.env.STRIPE_KEY</code>, <code>os.environ["STRIPE_KEY"]</code>, or the
              equivalent in any language. No SDK required.
            </li>
            <li>
              <strong>
                <code>/run/secrets/*</code>
              </strong>{" "}
              — files on a RAM-only tmpfs. Use this for SSH private keys, TLS certificates, JSON
              service-account files, and anything that doesn't fit neatly in an environment
              variable. The tmpfs never persists and is never written to the sandbox's durable
              volume.
            </li>
          </ul>
          <p>
            Certain environment variable names are reserved and cannot be overridden by profiles,
            including <code>HOME</code>, <code>PATH</code>, runtime-controlled variables used by the
            sandbox runner, and any name beginning with <code>VERSELF_</code>.
          </p>
        </Prose>
      </div>

      <div className="flex flex-col gap-3">
        <SubHeading id="sandbox-live-rotation">Live rotation in long-running sandboxes</SubHeading>
        <Prose>
          <p>
            Short-lived workloads pick up the current value at start and run to completion on that
            value. Long-running sandboxes, including development environments, receive rotation
            events from Verself and refresh environment variables and tmpfs files in place. Your
            code can watch for file changes or re-read environment variables on demand; rotation
            never takes effect mid-request for a read already in progress.
          </p>
        </Prose>
      </div>

      <div className="flex flex-col gap-3">
        <SubHeading id="sandbox-isolation">Isolation guarantees</SubHeading>
        <Prose>
          <ul>
            <li>
              Secret values are never written to platform state on the sandbox host. Execution
              records store only secret references, never resolved values.
            </li>
            <li>
              Secret values never appear in audit payloads, telemetry, traces, or error messages.
            </li>
            <li>
              Workloads from one organization never reach secrets belonging to another, regardless
              of scope or name overlap.
            </li>
            <li>
              Platform-curated sandbox images — the shared base snapshots every customer clones from
              — never contain customer secrets.
            </li>
          </ul>
        </Prose>
      </div>
    </section>
  );
}

function AccessControl() {
  return (
    <section className="flex flex-col gap-6">
      <SectionHeading id="access-control">Access control</SectionHeading>
      <Prose>
        <p>
          Access to secrets and keys is controlled by three organization roles plus per-resource
          grants. Members do not receive blanket read access to everything in the organization.
        </p>
      </Prose>

      <DefinitionGrid>
        <DefinitionCard
          term="Owner"
          definition="Read, write, delete, rotate, and use any secret or key in the organization. Can grant and revoke access for others. Can change organization-level policies."
        />
        <DefinitionCard
          term="Admin"
          definition="Everything an owner can do on secrets and keys, except changing organization-level policies."
        />
        <DefinitionCard
          term="Member"
          definition="Read secrets and use keys only on resources explicitly granted to the member, their team, or a service account they hold. No blanket access."
        />
      </DefinitionGrid>

      <div className="flex flex-col gap-3">
        <SubHeading id="grants">Grants</SubHeading>
        <Prose>
          <p>
            Owners and admins grant specific operations (<code>read</code>, <code>use</code>,{" "}
            <code>rotate</code>) on specific resources to specific actors — members, teams, or
            service accounts. Grants can be time-boxed and are audited on every use. Revoking a
            grant takes effect on the next read.
          </p>
          <p>
            For automated callers such as CI jobs and service accounts, issue API credentials with
            the exact operations the caller needs. API credentials never inherit blanket role
            permissions.
          </p>
        </Prose>
      </div>
    </section>
  );
}

function AuditTrail() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="audit-trail">Audit trail</SectionHeading>
      <Prose>
        <p>
          Every public operation on secrets and keys, and every injection read into a sandbox, emits
          a structured audit record. Records include actor, organization, operation, target
          resource, version, outcome, request ID, client IP, route, and timestamp.
        </p>
        <p>
          Audit records are stored by Verself's governance service: written to a durable primary
          store on the request path, chained with a keyed HMAC so that tampering with any row
          invalidates the chain from that point forward, and projected to a long-term analytics
          store for querying and dashboards. Retention follows your organization's data-retention
          policy.
        </p>
        <p>
          Secret values never appear in audit records. Secret paths are stored only as keyed HMAC
          hashes, so names like <code>PROD_STRIPE_KEY</code> do not become searchable data in your
          audit store. High-risk events — key rotation, grant changes, production secret reads — are
          surfaced on a dedicated risk feed in the governance dashboard.
        </p>
      </Prose>
    </section>
  );
}

function Limits() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="limits">Limits</SectionHeading>
      <Prose>
        <ul>
          <li>Secret value size: up to 64 KB per version.</li>
          <li>
            Secret name: up to 256 characters; alphanumeric with <code>-</code>, <code>_</code>, and{" "}
            <code>/</code> for path segments.
          </li>
          <li>
            Key name: up to 256 characters; alphanumeric with <code>-</code> and <code>_</code>.
          </li>
          <li>
            Versions retained per resource: the last 100 versions, or every version within the
            rotation retention window — whichever is greater.
          </li>
          <li>Data key size: 128, 192, or 256 bits for symmetric data keys.</li>
          <li>
            Rate limits are documented per operation in the{" "}
            <a href="/docs/reference">API reference</a>.
          </li>
        </ul>
      </Prose>
    </section>
  );
}
