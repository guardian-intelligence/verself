import { createFileRoute, retainSearchParams, stripSearchParams } from "@tanstack/react-router";

import {
  auditSearchToQuery,
  DEFAULT_AUDIT_LIMIT,
  DEFAULT_AUDIT_ORDER,
  parseAuditSearch,
  type AuditSearch,
} from "~/features/governance/audit-search";
import { GovernanceSettings } from "~/features/governance/components";
import { listGovernanceAPIActivities, listGovernanceDataExports } from "~/server-fns/api";

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
    const [apiActivities, exports] = await Promise.all([
      listGovernanceAPIActivities({ data: deps.auditQuery }),
      listGovernanceDataExports(),
    ]);
    return { apiActivities, exports };
  },
  component: GovernanceSettingsRoute,
});

function GovernanceSettingsRoute() {
  const { apiActivities, exports } = Route.useLoaderData();
  const search = Route.useSearch();
  return (
    <GovernanceSettings
      apiActivities={apiActivities.api_activities}
      activityLimit={apiActivities.limit}
      activityNextCursor={apiActivities.next_cursor}
      exports={exports}
      search={search}
    />
  );
}
