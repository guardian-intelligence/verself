import { createFileRoute } from "@tanstack/react-router";

import {
  DefinitionCard,
  DefinitionGrid,
  Prose,
  SectionHeading,
  SummaryItem,
  SummaryPanel,
} from "~/features/policy/policy-primitives";

export const Route = createFileRoute("/_workshop/docs/sdk")({
  component: SDKDocs,
  head: () => ({
    meta: [
      { title: "SDK - Verself Platform" },
      {
        name: "description",
        content:
          "Public structure for Verself SDK documentation: machine authentication, credential sources, resource clients, idempotency, pagination, errors, and observability.",
      },
    ],
  }),
});

type SectionStatus = "current" | "next" | "coming-soon";

type SDKSection = {
  readonly id: string;
  readonly title: string;
  readonly status: SectionStatus;
  readonly summary: string;
  readonly body?: readonly string[];
  readonly topics: readonly string[];
  readonly example?: string;
};

const SDK_SECTIONS: readonly SDKSection[] = [
  {
    id: "installation",
    title: "Installation",
    status: "coming-soon",
    summary:
      "Published TypeScript and Go package coordinates, release channels, checksums, and version pinning.",
    topics: ["TypeScript package", "Go module", "Release channels", "Checksums", "SBOMs"],
    example: `npm install @verself/sdk
go get github.com/verself/verself-go`,
  },
  {
    id: "client-construction",
    title: "Client Construction",
    status: "next",
    summary:
      "A single installation-level client that resolves service origins and exposes curated resource modules.",
    body: [
      "SDK callers should pass the installation apex, not individual service hosts, for the common path. Service URL overrides remain available for self-hosted tests and diagnostics.",
      "The public constructor should accept a credential source or token source. Raw access tokens remain useful for low-level debugging, but they should not be the primary SDK setup path.",
    ],
    topics: ["Installation apex", "Service URL resolution", "HTTP transport", "Fetch injection"],
    example: `const verself = await Verself.fromCredentialFile({
  baseURL: "https://verself.sh",
  org: "acme-corp",
  path: "/run/secrets/verself-credential.json",
});`,
  },
  {
    id: "authentication",
    title: "Authentication",
    status: "next",
    summary:
      "Credential sources exchange non-human identity proof for short-lived access tokens used by resource clients.",
    body: [
      "The SDK should abstract token acquisition, refresh, audience selection, and credential material parsing. Product resource clients should receive an access-token supplier instead of reading credential files directly.",
      "Credential files are a fallback for runtimes without workload identity. Workload trust should become the preferred SDK path for CI, agents, and self-hosted deployments.",
    ],
    topics: [
      "Credential file source",
      "Workload identity source",
      "Token exchange",
      "Audience selection",
      "Refresh policy",
    ],
    example: `const client = await Verself.fromWorkloadIdentity({
  baseURL: "https://verself.sh",
  org: "guardian-intelligence",
  provider: "github-actions",
});`,
  },
  {
    id: "credentials",
    title: "Credentials",
    status: "next",
    summary: "Machine credentials are the public non-human identity surface for SDK integrations.",
    body: [
      "A credential authenticates a backing machine principal in IAM. The SDK should expose credential lifecycle methods without requiring callers to manage the principal as a separate first-class workflow.",
      "Creation returns issued material once. SDK helpers should write credential bundles only when the caller explicitly asks for a file and should make owner-only file modes the default for local writes.",
    ],
    topics: [
      "Create credential",
      "Issued material",
      "Private-key JWT",
      "Client secret fallback",
      "Rotate credential",
      "Revoke credential",
    ],
    example: `const created = await verself.credentials.create({
  name: "e2e-acme",
  permissions: ["sandbox:execution:read", "sandbox:logs:read"],
  authMethod: "private_key_jwt",
});`,
  },
  {
    id: "workload-trust",
    title: "Workload Trust",
    status: "next",
    summary:
      "Trust bindings map external runtime identities into bounded Verself machine principals.",
    body: [
      "The SDK should make secretless exchange feel like the ordinary machine-auth path. Callers provide a trusted issuer profile and the SDK obtains the runtime assertion from the environment when possible.",
      "Trust creation must be bounded by issuer, audience, subject, repository or deployment facts, maximum permissions, and maximum token lifetime.",
    ],
    topics: [
      "GitHub Actions OIDC",
      "Verself runner identity",
      "Self-hosted SPIFFE",
      "Bounded delegation",
      "Token exchange",
    ],
    example: `await verself.credentials.trust.createGitHubActions({
  name: "e2e-main",
  repository: "guardianintelligence/verself-sh",
  ref: "refs/heads/main",
  permissions: ["sandbox:execution:read"],
  maxTokenTTLSeconds: 900,
});`,
  },
  {
    id: "organization-context",
    title: "Organization Context",
    status: "next",
    summary:
      "Every SDK call runs in an organization context resolved at token exchange or explicit client construction.",
    body: [
      "The SDK should avoid hidden global organization state. A client is bound to one organization, or a call explicitly supplies the organization when listing or exchanging across allowed organizations.",
      "Organization slugs are the public input; SDK internals can resolve IDs and carry the required token claims.",
    ],
    topics: ["Org slug", "Org ID resolution", "Token claims", "Multi-org listing"],
  },
  {
    id: "resource-identity",
    title: "Resource Identity",
    status: "next",
    summary:
      "Durable resources expose immutable IDs, globally unique URN resource names, scoped slugs, and display names.",
    body: [
      "The SDK should treat `id` as the storage identity, `resourceName` as the canonical cross-resource identity, `slug` as mutable parent-scoped UX, and `displayName` as a non-unique label.",
      "A resource name is generated by the server and remains stable across slug, display-name, and domain changes. It uses RFC 8141 URN syntax, includes the installation ID and immutable resource path, and gives self-hosted installations unambiguous audit and billing evidence.",
      "Duplicate display names are allowed. Duplicate active slugs under the same parent and resource type return a typed conflict unless the request matches an earlier idempotent create.",
    ],
    topics: [
      "URN resource names",
      "Opaque IDs",
      "Parent-scoped slugs",
      "Display names",
      "Identifier conflicts",
      "External identity conflicts",
    ],
    example: `{
  "id": "proj_01J8QK22N3F7W8A9K4Z6Q0B1CD",
  "resourceName": "urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT/projects/proj_01J8QK22N3F7W8A9K4Z6Q0B1CD",
  "slug": "api",
  "displayName": "API"
}`,
  },
  {
    id: "resource-name-formats",
    title: "Resource Name Formats",
    status: "next",
    summary:
      "SDK schemas should document the URN format for every resource type that can be referenced outside one parent route.",
    body: [
      "Use `resourceName` for the canonical public field. URN is the wire syntax; resource name is the SDK concept.",
      "Google-style APIs often call this field `name`. Verself uses `resourceName` so generated SDK types do not confuse canonical identity with slugs, display names, profile names, or provider names.",
      "Generated service routes may still use typed IDs internally. SDK methods should accept resource selectors, resolve slugs and short IDs where scoped input is ergonomic, and emit canonical resource names in durable DTOs, audit records, policy targets, imports, exports, and support bundles.",
      "Request fields that refer to another durable resource should use a resource-name field when the reference crosses parents, crosses resource types, appears in policy, appears in audit, or can leave the originating database.",
    ],
    topics: [
      "resourceName",
      "parentResourceName",
      "targetResourceName",
      "actorResourceName",
      "credentialResourceName",
      "ResourceSelector",
    ],
    example: `urn:verself:<installation-id>:orgs/<org-id>
urn:verself:<installation-id>:orgs/<org-id>/credentials/<credential-id>
urn:verself:<installation-id>:orgs/<org-id>/workloadTrusts/<trust-id>
urn:verself:<installation-id>:orgs/<org-id>/projects/<project-id>
urn:verself:<installation-id>:orgs/<org-id>/projects/<project-id>/repositories/<repository-id>
urn:verself:<installation-id>:orgs/<org-id>/runs/<run-id>
urn:verself:<installation-id>:orgs/<org-id>/runs/<run-id>/attempts/<attempt-id>
urn:verself:<installation-id>:orgs/<org-id>/schedules/<schedule-id>
urn:verself:<installation-id>:orgs/<org-id>/secrets/<secret-id>
urn:verself:<installation-id>:orgs/<org-id>/transitKeys/<key-id>`,
  },
  {
    id: "resources",
    title: "Resource Modules",
    status: "next",
    summary:
      "Curated modules group public API operations by product resource rather than by service transport client.",
    body: [
      "The target public grammar is top-level product modules: orgs, members, credentials, projects, repositories, runs, logs, schedules, secrets, notifications, audit, and billing.",
      "Backing service names remain implementation detail. Generated OpenAPI transports stay inside the SDK package and are not imported by customer code, CLI commands, or browser server routes directly.",
    ],
    topics: [
      "Orgs",
      "Members",
      "Credentials",
      "Projects",
      "Repositories",
      "Runs",
      "Logs",
      "Schedules",
      "Secrets",
      "Notifications",
      "Audit",
      "Billing",
    ],
  },
  {
    id: "api-shape",
    title: "API Shape",
    status: "next",
    summary:
      "Service APIs should be designed from the SDK method outward and then projected through OpenAPI.",
    body: [
      "The implementation order for public features is SDK resource method, stable DTOs and errors, Huma route, public OpenAPI projection, generated SDK transport, curated SDK wrapper, then CLI or docs example.",
      "If the SDK method would be awkward, the public API shape should be revisited before a facade grows around the awkwardness.",
    ],
    topics: [
      "SDK-driven operation names",
      "Stable DTOs",
      "Resource names",
      "Public OpenAPI projections",
      "Token exchange",
      "Credential lifecycle",
      "Trust bindings",
    ],
  },
  {
    id: "mutations",
    title: "Mutations",
    status: "current",
    summary:
      "Create, update, rotate, revoke, and schedule operations should carry idempotency keys by default.",
    body: [
      "SDK mutation helpers should accept caller-provided idempotency keys and generate stable operation-scoped keys when the caller supplies an idempotency seed.",
      "Observed-state writes should surface version conflicts as typed errors instead of retrying over concurrent changes.",
    ],
    topics: ["Idempotency keys", "Observed versions", "Conflict errors", "Retry boundaries"],
  },
  {
    id: "pagination",
    title: "Pagination",
    status: "coming-soon",
    summary:
      "List operations should expose page methods and iterators over cursor-based API contracts.",
    topics: ["Cursor requests", "Page objects", "Async iterators", "Limit handling"],
  },
  {
    id: "errors",
    title: "Errors",
    status: "current",
    summary:
      "SDK errors normalize HTTP status, problem details, request path, and operation context.",
    body: [
      "The public SDK should expose typed error predicates for authentication failure, authorization denial, validation failure, conflict, rate limiting, and service unavailability.",
    ],
    topics: ["Problem details", "Status codes", "Error predicates", "Retriability"],
  },
  {
    id: "observability",
    title: "Observability",
    status: "current",
    summary:
      "SDK clients propagate trace context and should make audit joins straightforward for automation.",
    body: [
      "Callers can provide trace context for API calls. The SDK should keep trace propagation explicit so CI, agents, and canaries can join SDK calls to ClickHouse evidence.",
    ],
    topics: ["Traceparent", "User agent", "Audit actor", "Credential ID", "Request IDs"],
  },
  {
    id: "versioning",
    title: "Versioning",
    status: "coming-soon",
    summary: "SDK releases should pin API versions and make compatibility guarantees explicit.",
    topics: ["API version headers", "SDK release cadence", "Deprecation policy", "Canary clients"],
  },
  {
    id: "standards",
    title: "Standards",
    status: "current",
    summary: "The SDK follows established OAuth, HTTP, tracing, and workload-identity primitives.",
    body: [
      "Machine authentication is based on OAuth token exchange, client credentials, private-key JWT, and workload assertions. Future human CLI login belongs behind OAuth device authorization.",
      "Transport behavior uses RFC 9457 problem details, W3C trace context, HTTP retry semantics, and Idempotency-Key for retriable mutations.",
    ],
    topics: [
      "OAuth client credentials",
      "Private-key JWT",
      "OAuth token exchange",
      "OAuth security BCP",
      "Device authorization",
      "Problem details",
      "Traceparent",
      "Idempotency-Key",
      "SPIFFE JWT-SVID",
    ],
  },
  {
    id: "language-support",
    title: "Language Support",
    status: "next",
    summary:
      "TypeScript and Go are the first curated SDKs; additional languages follow product demand and OpenAPI coverage.",
    topics: ["TypeScript", "Go", "Python", "Rust", "Terraform provider"],
  },
  {
    id: "design-contract",
    title: "Design Contract",
    status: "next",
    summary:
      "The SDK is the customer-facing abstraction boundary above generated OpenAPI transports.",
    body: [
      "SDKs own auth selection, token refresh, retries, idempotency, pagination, waiters, error normalization, trace propagation, and DTO conversion.",
      "Product services should not import SDK packages. The CLI, browser server routes, customer automation, and docs examples should use curated SDKs instead of direct service HTTP calls.",
    ],
    topics: [
      "No handwritten service HTTP",
      "No service generated clients in user code",
      "No SPIFFE auth in SDKs",
      "Stable DTOs",
      "Public examples use SDKs",
    ],
  },
];

function SDKDocs() {
  return (
    <article className="flex flex-col gap-10 [&_h2]:scroll-mt-[var(--header-scroll-offset)]">
      <header className="flex flex-col gap-4 border-b border-border pb-8">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Platform Docs
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">SDK</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          Program against Verself through curated SDKs that own authentication, retries,
          idempotency, pagination, errors, trace propagation, and resource-shaped DTOs.
        </p>
      </header>

      <section className="flex flex-col gap-4">
        <SectionHeading id="overview">Overview</SectionHeading>
        <SummaryPanel>
          <SummaryItem term="Primary caller">
            Non-human automation: CI jobs, agents, SDK integrations, CLI commands, and server-side
            application code.
          </SummaryItem>
          <SummaryItem term="Authentication model">
            Credential-first machine identity with workload trust as the preferred secretless path.
          </SummaryItem>
          <SummaryItem term="Boundary">
            SDKs wrap public OpenAPI projections and hide generated transport clients behind
            resource modules.
          </SummaryItem>
          <SummaryItem term="Design pressure">
            Missing SDK coverage should block CLI and docs examples until the SDK grows the missing
            method.
          </SummaryItem>
        </SummaryPanel>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="model">Model</SectionHeading>
        <DefinitionGrid>
          <DefinitionCard
            term="Machine principal"
            definition="The durable non-human IAM subject that receives permissions and appears in audit decisions."
          />
          <DefinitionCard
            term="Credential"
            definition="One authentication method for a machine principal. Credentials can be rotated or revoked without changing the principal's audit identity."
          />
          <DefinitionCard
            term="Workload trust"
            definition="A secretless mapping from an external runtime assertion, such as GitHub OIDC or a SPIFFE JWT-SVID, into a bounded Verself machine principal."
          />
          <DefinitionCard
            term="Access token"
            definition="The short-lived runtime token the SDK sends to public service APIs after credential or workload exchange."
          />
          <DefinitionCard
            term="Resource name"
            definition="A durable globally unique URN for a Verself resource, used for IAM, audit, billing evidence, exports, imports, and cross-resource references."
          />
        </DefinitionGrid>
      </section>

      {SDK_SECTIONS.map((section) => (
        <SDKDocsSection key={section.id} section={section} />
      ))}
    </article>
  );
}

function SDKDocsSection({ section }: { section: SDKSection }) {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <SectionHeading id={section.id}>{section.title}</SectionHeading>
        <StatusBadge status={section.status} />
      </div>
      <Prose>
        <p>{section.summary}</p>
        {section.body?.map((paragraph) => (
          <p key={paragraph}>{paragraph}</p>
        ))}
        <ul>
          {section.topics.map((topic) => (
            <li key={topic}>{topic}</li>
          ))}
        </ul>
      </Prose>
      {section.example ? <Command>{section.example}</Command> : null}
    </section>
  );
}

function StatusBadge({ status }: { status: SectionStatus }) {
  const label = status === "coming-soon" ? "Coming Soon" : status === "next" ? "Next" : "Current";
  return (
    <span
      data-status={status}
      className="rounded-sm border border-border px-2 py-1 font-mono text-[10px] font-medium uppercase tracking-normal text-muted-foreground data-[status=current]:text-foreground data-[status=next]:text-foreground"
    >
      {label}
    </span>
  );
}

function Command({ children }: { children: string }) {
  return (
    <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-4 font-mono text-sm leading-6 text-[var(--treatment-ink)]">
      <code>{children}</code>
    </pre>
  );
}
