import {
  createFileRoute,
  retainSearchParams,
  stripSearchParams,
} from "@tanstack/react-router";
import {
  auditSearchToQuery,
  DEFAULT_AUDIT_LIMIT,
  parseAuditSearch,
  type AuditSearch,
} from "~/features/governance/audit-search";
import { GovernanceSettings } from "~/features/governance/components";
import { listGovernanceAuditEvents, listGovernanceDataExports } from "~/server-fns/api";

export const Route = createFileRoute("/_shell/_authenticated/settings/governance")({
  validateSearch: parseAuditSearch,
  // retainSearchParams carries column visibility and page size across
  // filter/pagination changes so the user's table shape survives every Link
  // in the UI without per-Link bookkeeping. stripSearchParams drops the
  // default page size from the URL so shareable links stay clean until
  // someone explicitly deviates. Order matters: retain runs first, then
  // strip, so the "retained" default limit still gets stripped.
  search: {
    middlewares: [
      retainSearchParams<AuditSearch>(["cols", "limit"]),
      stripSearchParams<AuditSearch>({ limit: DEFAULT_AUDIT_LIMIT }),
    ],
  },
  // loaderDeps narrows the search params the audit loader cares about, so
  // column visibility changes don't trigger a refetch.
  loaderDeps: ({ search }) => ({ auditQuery: auditSearchToQuery(search) }),
  loader: async ({ deps }) => {
    const [audit, exports] = await Promise.all([
      listGovernanceAuditEvents({ data: deps.auditQuery }),
      listGovernanceDataExports(),
    ]);
    return { audit, exports };
  },
  component: GovernanceSettingsRoute,
});

function GovernanceSettingsRoute() {
  const { audit, exports } = Route.useLoaderData();
  const search = Route.useSearch();
  return (
    <GovernanceSettings
      auditEvents={audit.events}
      auditLimit={audit.limit}
      auditNextCursor={audit.next_cursor}
      exports={exports}
      search={search}
    />
  );
}
