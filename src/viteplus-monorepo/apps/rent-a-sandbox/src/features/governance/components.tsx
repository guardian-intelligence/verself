import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { getRouteApi, Link, useRouter } from "@tanstack/react-router";
import { ArrowDown, Check, Columns3, ListFilter, Plus, X } from "lucide-react";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Input } from "@forge-metal/ui/components/ui/input";
import {
  PageSection,
  PageSections,
  SectionActions,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@forge-metal/ui/components/ui/popover";
import { Select } from "@forge-metal/ui/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import { cn } from "@forge-metal/ui/lib/utils";
import { formatDateTimeUTC } from "~/lib/format";
import { createGovernanceDataExport, downloadGovernanceDataExport } from "~/server-fns/api";
import type { GovernanceAuditEvent, GovernanceExportJob } from "~/server-fns/api";
import {
  AUDIT_COLUMN_IDS,
  AUDIT_FILTER_DEFINITIONS,
  AUDIT_FILTER_KEYS,
  AUDIT_LIMIT_CHOICES,
  DEFAULT_AUDIT_LIMIT,
  activeFilters,
  resolveVisibleColumns,
  type AuditColumnId,
  type AuditFilterKey,
  type AuditLimit,
  type AuditSearch,
  type FilterDefinition,
} from "./audit-search";

// User-facing path used by <Link to=...> and navigate({ to }). The internal
// flattened route ID (which includes the _shell/_authenticated layout
// segments) is only used by getRouteApi below.
const GOVERNANCE_ROUTE = "/settings/governance" as const;

// getRouteApi gives us the typed useNavigate() without importing the Route
// factory from the route module, which would create an import cycle
// (route → components → route).
const routeApi = getRouteApi("/_shell/_authenticated/settings/governance");

interface GovernanceSettingsProps {
  auditEvents: Array<GovernanceAuditEvent>;
  auditLimit: number;
  auditNextCursor: string;
  exports: Array<GovernanceExportJob>;
  search: AuditSearch;
}

export function GovernanceSettings({
  auditEvents,
  auditLimit,
  auditNextCursor,
  exports,
  search,
}: GovernanceSettingsProps) {
  const router = useRouter();
  const createExport = useMutation({
    mutationFn: () =>
      createGovernanceDataExport({
        data: {
          include_logs: false,
          scopes: ["identity", "billing", "sandbox", "audit"],
        },
      }),
    onSuccess: async () => {
      await router.invalidate();
    },
  });
  const downloadExport = useMutation({
    mutationFn: (exportID: string) =>
      downloadGovernanceDataExport({
        data: { export_id: exportID },
      }),
    onSuccess: (artifact) => {
      downloadBase64Artifact(artifact.data_base64, artifact.content_type, artifact.file_name);
    },
  });
  const error = createExport.error ?? downloadExport.error;

  return (
    <PageSections>
      <PageSection>
        <SectionHeader>
          <SectionHeaderContent>
            <SectionTitle>Data export</SectionTitle>
            <SectionDescription>
              Download organization data, billing records, sandbox metadata, and audit evidence.
            </SectionDescription>
          </SectionHeaderContent>
          <SectionActions>
            <Button
              type="button"
              onClick={() => createExport.mutate()}
              disabled={createExport.isPending}
              data-testid="create-data-export"
            >
              {createExport.isPending ? "Creating export" : "Create data export"}
            </Button>
          </SectionActions>
        </SectionHeader>
        {error ? <p className="text-sm text-destructive">{formatError(error)}</p> : null}
        <ExportsTable
          exports={exports}
          onDownload={(exportID) => downloadExport.mutate(exportID)}
          downloadingExportID={downloadExport.isPending ? downloadExport.variables : undefined}
        />
      </PageSection>

      <PageSection>
        <SectionHeader>
          <SectionHeaderContent>
            <SectionTitle>Audit trail</SectionTitle>
            <SectionDescription>
              Chronological record of policy-evaluated operations in this organization. Newest
              first.
            </SectionDescription>
          </SectionHeaderContent>
        </SectionHeader>
        <AuditTrail
          events={auditEvents}
          limit={auditLimit}
          nextCursor={auditNextCursor}
          search={search}
        />
      </PageSection>
    </PageSections>
  );
}

function ExportsTable({
  exports,
  onDownload,
  downloadingExportID,
}: {
  exports: Array<GovernanceExportJob>;
  onDownload: (exportID: string) => void;
  downloadingExportID: string | undefined;
}) {
  if (exports.length === 0) {
    return <p className="text-sm text-muted-foreground">No exports have been created yet.</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Created</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Scope</TableHead>
          <TableHead>Files</TableHead>
          <TableHead>Size</TableHead>
          <TableHead>Artifact</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {exports.map((job) => (
          <TableRow key={job.export_id}>
            <TableCell>{formatDateTimeUTC(job.created_at)}</TableCell>
            <TableCell>
              <StatusBadge status={job.state} />
            </TableCell>
            <TableCell>{job.scopes.join(", ")}</TableCell>
            <TableCell>{job.files.length}</TableCell>
            <TableCell>{formatBytes(job.artifact_bytes)}</TableCell>
            <TableCell>
              {job.download_url ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => onDownload(job.export_id)}
                  data-testid={`download-data-export-${job.export_id}`}
                >
                  {downloadingExportID === job.export_id ? "Downloading" : "Download"}
                </Button>
              ) : (
                <span className="text-muted-foreground">Unavailable</span>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function AuditTrail({
  events,
  limit,
  nextCursor,
  search,
}: {
  events: Array<GovernanceAuditEvent>;
  limit: number;
  nextCursor: string;
  search: AuditSearch;
}) {
  const visibleColumns = resolveVisibleColumns(search);
  const activeKeys = activeFilters(search);
  const hasCursor = search.cursor !== undefined && search.cursor !== "";
  const anyNonDefault =
    activeKeys.length > 0 ||
    search.cols !== undefined ||
    (search.limit !== undefined && search.limit !== DEFAULT_AUDIT_LIMIT) ||
    hasCursor;

  return (
    <div className="flex flex-col gap-3">
      <AuditToolbar search={search} visibleColumns={visibleColumns} anyNonDefault={anyNonDefault} />

      <ActiveFilterChips search={search} activeKeys={activeKeys} />

      <div className="overflow-x-auto rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              {visibleColumns.map((id) => (
                <TableHead key={id}>
                  <ColumnHeader id={id} />
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {events.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={visibleColumns.length}
                  className="py-6 text-center text-sm text-muted-foreground"
                >
                  No audit events match this view.
                </TableCell>
              </TableRow>
            ) : (
              events.map((event) => (
                <TableRow key={event.event_id}>
                  {visibleColumns.map((id) => (
                    <TableCell key={id} className="align-top">
                      {renderCell(id, event)}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <AuditFooter
        count={events.length}
        limit={limit}
        nextCursor={nextCursor}
        hasCursor={hasCursor}
      />
    </div>
  );
}

function AuditToolbar({
  search,
  visibleColumns,
  anyNonDefault,
}: {
  search: AuditSearch;
  visibleColumns: ReadonlyArray<AuditColumnId>;
  anyNonDefault: boolean;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Popover>
        <PopoverTrigger
          render={
            <Button type="button" variant="outline" size="sm" data-testid="audit-add-filter">
              <ListFilter aria-hidden="true" />
              Add filter
            </Button>
          }
        />
        <PopoverContent align="start" className="w-80 gap-3">
          <PopoverHeader>
            <PopoverTitle>Filter audit events</PopoverTitle>
          </PopoverHeader>
          <div className="flex flex-col gap-3">
            {AUDIT_FILTER_KEYS.map((key) => (
              <FilterControl
                key={key}
                definition={AUDIT_FILTER_DEFINITIONS[key]}
                value={search[key]}
              />
            ))}
          </div>
        </PopoverContent>
      </Popover>

      <ColumnsPopover visibleColumns={visibleColumns} />

      <div className="ml-auto flex items-center gap-2">
        {anyNonDefault ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            render={
              <Link
                to={GOVERNANCE_ROUTE}
                // Functional form so retainSearchParams sees prev=this and
                // next-returns={} — otherwise a static search={} is merged
                // by the router and filters survive the reset.
                search={() => ({})}
                data-testid="audit-reset"
              />
            }
          >
            Reset view
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function FilterControl({
  definition,
  value,
}: {
  definition: FilterDefinition;
  value: AuditSearch[AuditFilterKey];
}) {
  const navigate = routeApi.useNavigate();

  // applyFilter writes a functional search update: spread prev so
  // retainSearchParams-managed keys like cols/limit stay in place, set the
  // new filter value (or undefined to clear), and explicitly null out
  // cursor because any filter change invalidates its (recorded_at, sequence)
  // tuple against the new WHERE clause.
  const applyFilter = (next: AuditSearch[AuditFilterKey]) => {
    navigate({
      to: GOVERNANCE_ROUTE,
      search: (prev) => ({
        ...prev,
        [definition.key]: next,
        cursor: undefined,
      }),
    });
  };

  const [draft, setDraft] = useState(typeof value === "string" ? value : "");

  const commitText = () => {
    const trimmed = draft.trim();
    if (trimmed === "") {
      applyFilter(undefined);
      return;
    }
    if (trimmed !== value) applyFilter(trimmed);
  };

  if (definition.kind === "boolean") {
    const on = value === true;
    return (
      <label className="flex cursor-pointer items-start gap-2 text-xs">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={on}
          onChange={(event) => applyFilter(event.target.checked ? true : undefined)}
          data-testid={`audit-filter-${definition.key}`}
        />
        <span className="flex flex-col gap-0.5">
          <span className="font-medium text-foreground">{definition.label}</span>
          <span className="text-muted-foreground">{definition.help}</span>
        </span>
      </label>
    );
  }

  if (definition.kind === "enum" && definition.options) {
    return (
      <div className="flex flex-col gap-1 text-xs">
        <label className="font-medium text-foreground" htmlFor={`audit-filter-${definition.key}`}>
          {definition.label}
        </label>
        <Select
          id={`audit-filter-${definition.key}`}
          value={typeof value === "string" ? value : ""}
          onChange={(event) => {
            const next = event.target.value;
            applyFilter(next === "" ? undefined : next);
          }}
          data-testid={`audit-filter-${definition.key}`}
        >
          <option value="">Any</option>
          {definition.options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>
        <p className="text-muted-foreground">{definition.help}</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1 text-xs">
      <label className="font-medium text-foreground" htmlFor={`audit-filter-${definition.key}`}>
        {definition.label}
      </label>
      <Input
        id={`audit-filter-${definition.key}`}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={commitText}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            commitText();
          }
        }}
        placeholder={definition.help}
        data-testid={`audit-filter-${definition.key}`}
      />
    </div>
  );
}

function ColumnsPopover({ visibleColumns }: { visibleColumns: ReadonlyArray<AuditColumnId> }) {
  const navigate = routeApi.useNavigate();

  const toggle = (id: AuditColumnId, visible: boolean) => {
    navigate({
      to: GOVERNANCE_ROUTE,
      search: (prev) => {
        const current = prev.cols ?? visibleColumns;
        const nextSet = visible
          ? Array.from(new Set([...current, id]))
          : current.filter((col) => col !== id);
        // Re-sort to canonical column order so the URL is stable regardless
        // of which order the user toggled in.
        const ordered = AUDIT_COLUMN_IDS.filter((col) => nextSet.includes(col));
        return { ...prev, cols: ordered };
      },
    });
  };

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button type="button" variant="outline" size="sm" data-testid="audit-columns">
            <Columns3 aria-hidden="true" />
            Columns
          </Button>
        }
      />
      <PopoverContent align="start" className="w-64 gap-3">
        <PopoverHeader>
          <PopoverTitle>Columns</PopoverTitle>
        </PopoverHeader>
        <div className="flex flex-col gap-1">
          {AUDIT_COLUMN_IDS.map((id) => {
            const checked = visibleColumns.includes(id);
            return (
              <button
                key={id}
                type="button"
                onClick={() => toggle(id, !checked)}
                className={cn(
                  "flex items-center justify-between rounded-md px-2 py-1.5 text-xs transition-colors hover:bg-accent hover:text-accent-foreground",
                  checked ? "text-foreground" : "text-muted-foreground",
                )}
                data-testid={`audit-column-toggle-${id}`}
                aria-pressed={checked}
              >
                <span>{columnLabel(id)}</span>
                {checked ? (
                  <Check aria-hidden="true" className="size-3.5" />
                ) : (
                  <Plus aria-hidden="true" className="size-3.5" />
                )}
              </button>
            );
          })}
        </div>
        <p className="text-xs text-muted-foreground">
          Column choice persists in the page URL. Share the link to share the view.
        </p>
      </PopoverContent>
    </Popover>
  );
}

function ActiveFilterChips({
  search,
  activeKeys,
}: {
  search: AuditSearch;
  activeKeys: ReadonlyArray<AuditFilterKey>;
}) {
  if (activeKeys.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {activeKeys.map((key) => {
        const definition = AUDIT_FILTER_DEFINITIONS[key];
        const raw = search[key];
        const label =
          definition.kind === "boolean" ? definition.label : `${definition.label}: ${String(raw)}`;
        return (
          <Link
            key={key}
            to={GOVERNANCE_ROUTE}
            // Clear this one filter and the cursor (the cursor is anchored
            // to the old filter set). retainSearchParams keeps cols/limit;
            // other filter values survive because we spread prev first.
            search={(prev) => ({ ...prev, [key]: undefined, cursor: undefined })}
            className="group inline-flex items-center gap-1 rounded-full border border-border bg-muted/40 px-2 py-0.5 text-xs text-foreground transition-colors hover:border-destructive hover:bg-destructive/10 hover:text-destructive"
            data-testid={`audit-chip-${key}`}
            aria-label={`Remove filter: ${label}`}
          >
            <span>{label}</span>
            <X aria-hidden="true" className="size-3" />
          </Link>
        );
      })}
    </div>
  );
}

function AuditFooter({
  count,
  limit,
  nextCursor,
  hasCursor,
}: {
  count: number;
  limit: number;
  nextCursor: string;
  hasCursor: boolean;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-muted-foreground">
      <div>
        {count === 0
          ? "No events on this page."
          : `${count} event${count === 1 ? "" : "s"} on this page (newest first, up to ${limit}).`}
      </div>
      <div className="flex items-center gap-3">
        <LimitSelect value={limit} />
        {/* Previous uses browser history rather than a computed cursor
            because the server only supports forward cursor walks — there's
            no reliable way to derive the previous page's cursor without
            tracking a stack. Browser back is idempotent and correct. */}
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => window.history.back()}
          disabled={!hasCursor}
          data-testid="audit-prev"
        >
          Previous
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!nextCursor}
          data-testid="audit-next"
          render={
            nextCursor ? (
              <Link to={GOVERNANCE_ROUTE} search={(prev) => ({ ...prev, cursor: nextCursor })} />
            ) : undefined
          }
        >
          Next
        </Button>
      </div>
    </div>
  );
}

function LimitSelect({ value }: { value: number }) {
  const navigate = routeApi.useNavigate();
  return (
    <label className="flex items-center gap-2">
      <span>Per page</span>
      <Select
        value={String(value)}
        onChange={(event) => {
          const next = Number(event.target.value) as AuditLimit;
          navigate({
            to: GOVERNANCE_ROUTE,
            // Page size change resets the cursor — the (recorded_at,
            // sequence) tuple's "page" meaning only holds for the old size.
            search: (prev) => ({ ...prev, limit: next, cursor: undefined }),
          });
        }}
        data-testid="audit-limit"
        className="w-auto min-w-0"
      >
        {AUDIT_LIMIT_CHOICES.map((choice) => (
          <option key={choice} value={choice}>
            {choice}
          </option>
        ))}
      </Select>
    </label>
  );
}

function ColumnHeader({ id }: { id: AuditColumnId }) {
  if (id === "time") {
    return (
      <span
        className="inline-flex items-center gap-1"
        title="Chronological order (newest first). Other sort orders are not yet supported server-side."
      >
        Time
        <ArrowDown aria-hidden="true" className="size-3" />
      </span>
    );
  }
  return <span>{columnLabel(id)}</span>;
}

function renderCell(id: AuditColumnId, event: GovernanceAuditEvent) {
  switch (id) {
    case "time":
      return (
        <span className="whitespace-nowrap font-mono text-xs">
          {formatDateTimeUTC(event.recorded_at)}
        </span>
      );
    case "risk":
      return <RiskBadge risk={event.risk_level} />;
    case "actor":
      return (
        <div className="flex flex-col gap-0.5">
          <span>{actorLabel(event)}</span>
          <span className="text-xs text-muted-foreground">{event.actor_type}</span>
        </div>
      );
    case "operation":
      return (
        <div className="flex flex-col gap-0.5">
          <span>{event.operation_display || event.operation_id}</span>
          <span className="text-xs text-muted-foreground">{event.operation_type}</span>
        </div>
      );
    case "target":
      return <span className="whitespace-nowrap text-xs">{targetLabel(event)}</span>;
    case "result":
      return <StatusBadge status={event.result} />;
    case "location":
      return <span className="text-xs text-muted-foreground">{locationLabel(event)}</span>;
    case "source":
      return <span className="text-xs">{event.source_product_area || event.service_name}</span>;
    case "sequence":
      return <span className="font-mono text-xs">{event.sequence}</span>;
    case "trace":
      return (
        <span className="font-mono text-xs text-muted-foreground">{event.trace_id ?? "—"}</span>
      );
    case "credential":
      return (
        <span className="text-xs text-muted-foreground">
          {event.credential_name ?? event.credential_id ?? "—"}
        </span>
      );
    case "decision":
      return <span className="text-xs">{event.decision}</span>;
    case "event":
      return <span className="font-mono text-xs">{event.audit_event}</span>;
  }
}

function columnLabel(id: AuditColumnId): string {
  switch (id) {
    case "time":
      return "Time";
    case "risk":
      return "Risk";
    case "actor":
      return "Actor";
    case "operation":
      return "Operation";
    case "target":
      return "Target";
    case "result":
      return "Result";
    case "location":
      return "Location";
    case "source":
      return "Source";
    case "sequence":
      return "Sequence";
    case "trace":
      return "Trace ID";
    case "credential":
      return "Credential";
    case "decision":
      return "Decision";
    case "event":
      return "Event name";
  }
}

function RiskBadge({ risk }: { risk: string }) {
  const variant = risk === "critical" || risk === "high" ? "warning" : "secondary";
  return <Badge variant={variant}>{risk}</Badge>;
}

function StatusBadge({ status }: { status: string }) {
  const variant =
    status === "completed" || status === "allowed"
      ? "success"
      : status === "failed" || status === "denied" || status === "error"
        ? "destructive"
        : "secondary";
  return <Badge variant={variant}>{status}</Badge>;
}

function actorLabel(event: GovernanceAuditEvent) {
  if (event.actor_type === "api_credential" && event.credential_name) {
    return event.credential_name;
  }
  return event.actor_display || event.actor_id;
}

function targetLabel(event: GovernanceAuditEvent) {
  const target = event.target_display || event.target_id || "organization";
  return `${event.target_kind}: ${target}`;
}

function locationLabel(event: GovernanceAuditEvent) {
  const parts = [event.geo_country, event.client_ip_version].filter(Boolean);
  return parts.length > 0 ? parts.join(" ") : "unknown";
}

function formatBytes(value: string) {
  const bytes = Number(value);
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function downloadBase64Artifact(dataBase64: string, contentType: string, fileName: string) {
  const binary = atob(dataBase64);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  const url = URL.createObjectURL(new Blob([bytes], { type: contentType }));
  const link = document.createElement("a");
  link.href = url;
  link.download = fileName;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}
