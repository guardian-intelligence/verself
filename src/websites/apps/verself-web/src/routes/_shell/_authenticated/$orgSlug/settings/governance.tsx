import { createFileRoute, retainSearchParams, stripSearchParams } from "@tanstack/react-router";

import {
  auditSearchToQuery,
  DEFAULT_AUDIT_LIMIT,
  DEFAULT_AUDIT_ORDER,
  parseAuditSearch,
  type AuditSearch,
} from "~/features/governance/audit-search";
import { GovernanceSettings } from "~/features/governance/components";
import { listGovernanceAuditEvents, listGovernanceDataExports } from "~/server-fns/api";

export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/settings/governance")({
  validateSearch: parseAuditSearch,
  search: {
    middlewares: [
      retainSearchParams<AuditSearch>(["limit", "order"]),
      stripSearchParams<AuditSearch>({
        limit: DEFAULT_AUDIT_LIMIT,
        order: DEFAULT_AUDIT_ORDER,
      }),
    ],
  },
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
