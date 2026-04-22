import { createFileRoute, retainSearchParams, stripSearchParams } from "@tanstack/react-router";

// Inline the SearchMiddleware shape — @tanstack/react-router doesn't re-
// export the type from router-core in this pinned version, so we type the
// callback by its structural contract. Same shape as SearchMiddleware<T>.
type SearchMiddleware<T> = (ctx: { search: T; next: (search: T) => T }) => T;
import {
  auditSearchToQuery,
  DEFAULT_AUDIT_LIMIT,
  DEFAULT_AUDIT_ORDER,
  isDefaultAuditColumns,
  parseAuditSearch,
  type AuditSearch,
} from "~/features/governance/audit-search";
import { GovernanceSettings } from "~/features/governance/components";
import { listGovernanceAuditEvents, listGovernanceDataExports } from "~/server-fns/api";

// Drops `cols` from the URL whenever it matches the canonical default order,
// keeping shareable links clean for investigators who never deviated from the
// default view. The default-value shape of stripSearchParams can't express
// "equal to this array" so we run a small middleware here.
const stripDefaultCols: SearchMiddleware<AuditSearch> = ({ search, next }) => {
  const output = next(search);
  if (isDefaultAuditColumns(output.cols)) {
    const { cols: _cols, ...rest } = output;
    return rest;
  }
  return output;
};

export const Route = createFileRoute("/_shell/_authenticated/settings/governance")({
  validateSearch: parseAuditSearch,
  // retainSearchParams carries column visibility, page size, view preset, and
  // sort order across filter/pagination changes so the user's table shape
  // survives every Link without per-Link bookkeeping. stripSearchParams +
  // stripDefaultCols drop default values so shareable URLs stay clean until a
  // user deviates. Order matters: retain runs first so defaults that came in
  // from a retained param still get stripped.
  search: {
    middlewares: [
      retainSearchParams<AuditSearch>(["cols", "limit", "view", "order"]),
      stripSearchParams<AuditSearch>({
        limit: DEFAULT_AUDIT_LIMIT,
        order: DEFAULT_AUDIT_ORDER,
      }),
      stripDefaultCols,
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
