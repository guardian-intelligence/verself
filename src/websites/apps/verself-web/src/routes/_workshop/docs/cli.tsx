import { createFileRoute } from "@tanstack/react-router";

import { Prose, SectionHeading } from "~/features/policy/policy-primitives";

export const Route = createFileRoute("/_workshop/docs/cli")({
  component: CLIDocs,
  head: () => ({
    meta: [
      { title: "CLI - Verself Platform" },
      {
        name: "description",
        content:
          "Public structure for the Verself CLI documentation: machine authentication, organizations, credentials, and sandbox compute operations.",
      },
    ],
  }),
});

type SectionStatus = "current" | "next" | "coming-soon";

type CLISection = {
  readonly id: string;
  readonly title: string;
  readonly status: SectionStatus;
  readonly summary: string;
  readonly body?: readonly string[];
  readonly topics: readonly string[];
  readonly commands?: string;
};

const CLI_SECTIONS: readonly CLISection[] = [
  {
    id: "install",
    title: "Install",
    status: "coming-soon",
    summary: "Public release channels, checksums, version pinning, and shell completion.",
    topics: ["Release artifacts", "Checksum verification", "Version output", "Completions"],
    commands: `verself --version`,
  },
  {
    id: "authentication",
    title: "Authentication",
    status: "next",
    summary: "Non-human authentication for headless CLI, CI, SDK integrations, and agent runs.",
    body: [
      "Use a credential file only when the runtime cannot present a stronger workload identity. The file contains one machine credential bundle and is exchanged for short-lived access tokens for the selected organization and service audience.",
      "Use workload trust when the runtime can prove its own identity. GitHub Actions OIDC, future Verself runner identity, and self-hosted SPIFFE identities should exchange runtime assertions for short-lived Verself tokens instead of storing long-lived secrets.",
      "The CLI stores profile metadata locally. It should not persist human bearer tokens, prompt for passwords, or treat a credential file as an all-purpose global token.",
    ],
    topics: [
      "Credential-file auth",
      "Workload identity exchange",
      "Short-lived access tokens",
      "Current identity",
    ],
    commands: `verself auth use --profile ci --org guardian-intelligence --credential-file /run/secrets/verself-credential.json
verself auth exchange --profile ci --org guardian-intelligence --issuer github-actions
verself auth whoami
verself auth logout`,
  },
  {
    id: "profiles",
    title: "Profiles",
    status: "next",
    summary:
      "Local named contexts that select credential material, organization context, service origins, and output preferences.",
    body: [
      "A profile is local CLI state. It names the credential material to use, the active organization, and the Verself installation the CLI should call. A profile does not grant access by itself; IAM evaluates the authenticated principal on every request.",
      "Use separate profiles when you regularly switch between organizations or machine identities. For example, one profile can represent the platform CI credential and another can represent an ACME Corp e2e credential.",
      "`verself orgs use` updates the organization context for a profile. Future authenticated commands should request tokens for that organization instead of relying on a globally scoped bearer token.",
    ],
    topics: ["Profile storage", "Active profile", "Organization context", "Service origins"],
    commands: `verself auth profiles
verself orgs use guardian-intelligence --profile guardian
verself auth token --profile guardian --audience iam-service`,
  },
  {
    id: "resource-identifiers",
    title: "Resource Identifiers",
    status: "next",
    summary:
      "Durable resources use opaque IDs, globally unique URN resource names, parent-scoped slugs, and display names for different jobs.",
    body: [
      "JSON output should include `id`, `resourceName`, optional `slug`, and `displayName` for durable resources. The `resourceName` field is the canonical identifier for audit joins, imports, exports, policy references, and cross-resource flags.",
      "Slugs are for scoped human input such as `--org guardian-intelligence` and `--project api`. Display names are labels and should never be used for identity resolution.",
      "Flags that reference resources outside the active command scope should name the field explicitly, such as `--target-resource-name`, `--parent-resource-name`, `--credential-resource-name`, or `--repository-resource-name`.",
      "Duplicate active slugs under the same parent and resource type are conflicts. Duplicate display names are allowed. Idempotent retries return the original resource only when the key and request body match.",
    ],
    topics: [
      "URN resource names",
      "Opaque IDs",
      "Parent-scoped slugs",
      "Display names",
      "Identifier conflicts",
      "Idempotent creates",
    ],
    commands: `verself projects inspect urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT/projects/proj_01J8QK22N3F7W8A9K4Z6Q0B1CD
verself projects inspect api --org guardian-intelligence
verself projects create "API" --slug api --idempotency-key project-create-api-2026-05-11`,
  },
  {
    id: "organizations",
    title: "Organizations",
    status: "next",
    summary: "Customer-facing organization creation and management through the public IAM service.",
    body: [
      "An organization is the tenant boundary for users, machine principals, repositories, sandbox runs, billing, audit records, and policy. Every authenticated CLI command runs in exactly one organization context unless the command is explicitly listing organizations available to the caller.",
      "Creating an organization creates the directory record, initializes its IAM graph, and grants the creator owner access. Organization slugs are stable customer-facing identifiers and should be used in command examples and automation.",
      "Updates are versioned writes. The CLI should send the observed organization version so concurrent changes fail loudly instead of overwriting each other.",
    ],
    topics: ["Create organization", "List organizations", "Inspect organization", "Update profile"],
    commands: `verself orgs create acme-corp --display-name "ACME Corp"
verself orgs list
verself orgs inspect acme-corp
verself orgs update acme-corp --display-name "ACME Corporation"`,
  },
  {
    id: "members",
    title: "Members",
    status: "next",
    summary:
      "Human member invitation, role assignment, membership removal, and capability inspection.",
    body: [
      "Members are human users in an organization. Use member commands to invite a person, inspect current access, change their role, or remove their access from the organization.",
      "The public role model is owner, admin, and member. Owners and admins manage IAM. Members can use product capabilities granted to the member role but do not automatically manage organization policy.",
      "Machine access is managed through credentials and workload trust. Machine principals can receive IAM grants, but they are not browser or console users.",
    ],
    topics: ["Invite member", "Accept invite", "Change role", "Remove member", "Capabilities"],
    commands: `verself orgs members invite ops@acme.example --role admin
verself orgs members list
verself orgs members update user_123 --role member
verself orgs members remove user_123`,
  },
  {
    id: "credentials",
    title: "Credentials",
    status: "next",
    summary:
      "Credential-first non-human identity management for CI, agents, SDK integrations, and API clients.",
    body: [
      "A credential creates or attaches to a durable non-human IAM principal. The CLI does not require users to manage that backing principal through a separate command surface for common workflows.",
      "Create one credential per operational purpose. Narrow credentials are easier to audit, rotate, and revoke than a shared automation identity reused across agents, CI, and SDK integrations.",
      "New credential material is revealed once. Store it in a secret manager or owner-only file, then use `verself auth use --credential-file` only in runtimes that cannot use workload trust.",
    ],
    topics: [
      "Create credential",
      "Private-key JWT",
      "Client secret fallback",
      "Roll credential",
      "Revoke credential",
      "Backing machine principal",
    ],
    commands: `verself credentials create e2e-acme --org acme-corp --permission sandbox:execution:read --permission sandbox:logs:read --method private-key-jwt --output credential-file
verself credentials list --org acme-corp
verself credentials rotate cred_123 --output credential-file
verself credentials revoke cred_123`,
  },
  {
    id: "workload-trust",
    title: "Workload Trust",
    status: "next",
    summary:
      "Secretless mappings from external workload identities into Verself machine principals.",
    body: [
      "Workload trust is the preferred authentication path for hosted CI, Verself runners, and self-hosted installations that already have runtime identity. The workload presents an OIDC, JWT-SVID, or equivalent assertion and receives a short-lived Verself access token.",
      "Trust bindings should pin issuer, audience, subject shape, repository or deployment facts, maximum permissions, and maximum token lifetime. A broad issuer-only binding is too easy to abuse.",
      "Credential files remain available for runtimes without an identity provider, but they are the fallback path rather than the target architecture.",
    ],
    topics: [
      "GitHub Actions OIDC",
      "Verself runner identity",
      "Self-hosted SPIFFE",
      "Bounded delegation",
      "Short-lived token exchange",
    ],
    commands: `verself credentials trust create github-actions e2e-main --org guardian-intelligence --repo guardianintelligence/verself-sh --ref refs/heads/main --permission sandbox:execution:read
verself credentials trust list --org guardian-intelligence
verself auth exchange --issuer github-actions --org guardian-intelligence`,
  },
  {
    id: "runner-onboarding",
    title: "Runner Onboarding",
    status: "coming-soon",
    summary:
      "GitHub App installation, repository connection, runner labels, and Verself checkout configuration.",
    topics: ["GitHub installation", "Repository connection", "Runner labels", "Checkout action"],
    commands: `verself github installations list
verself github installations connect`,
  },
  {
    id: "projects",
    title: "Projects and Environments",
    status: "current",
    summary:
      "Project records and environment records used by service APIs, console views, and workflow execution.",
    topics: ["List projects", "Create project", "Archive project", "Manage environments"],
    commands: `verself projects list
verself projects create "Console" --slug console
verself projects environments list <project-id>`,
  },
  {
    id: "repositories",
    title: "Repositories and Workflow Runs",
    status: "current",
    summary:
      "Source repositories, refs, trees, checkout grants, workflow run records, and dispatch commands.",
    topics: ["Repositories", "Refs and trees", "Checkout grants", "Workflow dispatch"],
    commands: `verself repos list --project-id <project-id>
verself repos checkout-grants create <repo-id> --ref main
verself repos workflow-runs dispatch <repo-id> --project-id <project-id> --workflow-path .github/workflows/ci.yml`,
  },
  {
    id: "sandboxes",
    title: "Sandboxes, Runs, and Schedules",
    status: "current",
    summary:
      "Execution records, attempts, logs, analytics, and recurring workflow dispatch schedules.",
    topics: [
      "List runs",
      "Read logs",
      "Search logs",
      "Create schedule",
      "Pause or resume schedule",
    ],
    commands: `verself runs list --status succeeded
verself runs logs <execution-id>
verself schedules create --project-id <project-id> --source-repository-id <repo-id> --workflow-path .github/workflows/ci.yml --interval-seconds 900`,
  },
  {
    id: "secrets",
    title: "Secrets and Environment",
    status: "current",
    summary:
      "Environment-scoped secret values for platform bootstrap and customer-facing environment workflows.",
    topics: ["Read environment value", "Set secret", "Pull environment", "Run with environment"],
    commands: `verself env get <key> --org <org> --project <project> --environment <environment>
verself env pull .env --org <org> --project <project> --environment <environment>
verself env run --org <org> --project <project> --environment <environment> -- <command>`,
  },
  {
    id: "notifications",
    title: "Notifications",
    status: "current",
    summary:
      "Inbox state, read cursor, delivery preferences, dismissal, clearing, and test notification delivery.",
    topics: ["List notifications", "Read summary", "Update preferences", "Mark read", "Dismiss"],
    commands: `verself notifications list --limit 20
verself notifications summary
verself notifications preferences set --version 1 --enabled true --web-enabled true`,
  },
  {
    id: "governance",
    title: "Governance and Audit",
    status: "current",
    summary: "Audit event search and organization data exports over the public Governance API.",
    topics: ["Search audit events", "Create export", "Download export", "Trace context"],
    commands: `verself audit events --high-risk --limit 50
verself audit exports create --scope audit
verself audit exports download <export-id> ./export.tar.gz`,
  },
  {
    id: "billing",
    title: "Billing",
    status: "current",
    summary:
      "Entitlements, contracts, plans, and current billing statement projections used by the console.",
    topics: ["Entitlements", "Contracts", "Plans", "Current statement"],
    commands: `verself billing entitlements
verself billing plans --product-id sandbox-ci
verself billing statement --product-id sandbox-ci`,
  },
  {
    id: "self-hosting",
    title: "Self-Hosted Operator Commands",
    status: "coming-soon",
    summary:
      "Operator-local bootstrap, company configuration, site secrets, and deployment handoff for independent Verself installations.",
    topics: ["Company configuration", "Bootstrap", "SOPS material", "Site handoff"],
    commands: `verself company configure guardian --site prod
verself bootstrap --company guardian --repo-root .`,
  },
  {
    id: "reference",
    title: "Reference",
    status: "coming-soon",
    summary:
      "Global flags, environment variables, output formats, config paths, exit codes, and command index.",
    topics: ["Global flags", "Environment variables", "JSON output", "Config paths", "Exit codes"],
  },
];

function CLIDocs() {
  return (
    <article className="flex flex-col gap-10 [&_h2]:scroll-mt-[var(--header-scroll-offset)]">
      <header className="flex flex-col gap-4 border-b border-border pb-8">
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">CLI</h1>
      </header>

      <section className="flex flex-col gap-4">
        <SectionHeading id="overview">Overview</SectionHeading>
        <Prose>
          <p>
            Verself gives you multiple ways to interact with and configure your Verself
            installation. The command-line interface is the customer-facing terminal surface for
            organization management, machine credentials, runner onboarding, logs, schedules, and
            other sandbox compute operations.
          </p>
        </Prose>
      </section>

      {CLI_SECTIONS.map((section) => (
        <CLIDocsSection key={section.id} section={section} />
      ))}
    </article>
  );
}

function CLIDocsSection({ section }: { section: CLISection }) {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <SectionHeading id={section.id}>{section.title}</SectionHeading>
        <StatusBadge status={section.status} />
      </div>
      <Prose>
        <p>{section.summary}</p>
        {section.body?.map((paragraph) => (
          <p key={paragraph}>{renderInlineCode(paragraph)}</p>
        ))}
        <ul>
          {section.topics.map((topic) => (
            <li key={topic}>{topic}</li>
          ))}
        </ul>
      </Prose>
      {section.commands ? <Command>{section.commands}</Command> : null}
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

function renderInlineCode(text: string) {
  const parts = text.split(/(`[^`]+`)/g);
  return parts.map((part, index) => {
    if (part.startsWith("`") && part.endsWith("`")) {
      return <code key={`${part}-${index}`}>{part.slice(1, -1)}</code>;
    }
    return part;
  });
}

function Command({ children }: { children: string }) {
  return (
    <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-4 font-mono text-sm leading-6 text-[var(--treatment-ink)]">
      <code>{children}</code>
    </pre>
  );
}
