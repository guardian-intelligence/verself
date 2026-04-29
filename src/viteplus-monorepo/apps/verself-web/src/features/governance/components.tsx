import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { getRouteApi, Link, useRouter } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, Check, Clock, Columns3, Copy, ListFilter, X } from "lucide-react";
import { Badge } from "@verself/ui/components/ui/badge";
import { Button } from "@verself/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@verself/ui/components/ui/dropdown-menu";
import { Input } from "@verself/ui/components/ui/input";
import {
  PageSection,
  PageSections,
  SectionActions,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@verself/ui/components/ui/popover";
import { Select } from "@verself/ui/components/ui/select";
import { toast } from "@verself/ui/components/ui/sonner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@verself/ui/components/ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@verself/ui/components/ui/tooltip";
import { cn } from "@verself/ui/lib/utils";
import { formatDateTimeLocal, formatDateTimeUTC, formatRelative } from "~/lib/format";
import { createGovernanceDataExport, downloadGovernanceDataExport } from "~/server-fns/api";
import type { GovernanceAuditEvent, GovernanceExportJob } from "~/server-fns/api";
import {
  AUDIT_COLUMN_IDS,
  AUDIT_FILTER_DEFINITIONS,
  AUDIT_FILTER_GROUPS,
  AUDIT_FILTER_KEYS,
  AUDIT_LIMIT_CHOICES,
  DEFAULT_AUDIT_LIMIT,
  DEFAULT_AUDIT_ORDER,
  activeFilters,
  resolveView,
  resolveVisibleColumns,
  type AuditColumnId,
  type AuditFilterKey,
  type AuditLimit,
  type AuditOrder,
  type AuditSearch,
  type AuditView,
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

// Time-zone preference lives in localStorage, not the URL: sharing a link
// should show timestamps in the recipient's zone, not the sender's.
const TZ_STORAGE_KEY = "verself:governance.audit.timezone";
type TimezoneMode = "local" | "utc";

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
      toast.success("Data export ready", {
        description: "Download it from this page before it expires.",
      });
      await router.invalidate();
    },
    onError: (error) => {
      toast.error("Failed to create data export", { description: formatError(error) });
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
    onError: (error) => {
      toast.error("Failed to download data export", { description: formatError(error) });
    },
  });

  const handleCreate = () => {
    if (createExport.isPending) {
      toast("A data export is already being prepared", {
        description: "You'll see it appear below when it's ready.",
      });
      return;
    }
    createExport.mutate();
  };

  return (
    <TooltipProvider delay={200}>
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
              <Button type="button" onClick={handleCreate} data-testid="create-data-export">
                {createExport.isPending ? "Creating export…" : "Create data export"}
              </Button>
            </SectionActions>
          </SectionHeader>
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
                Chronological record of policy-evaluated operations in this organization.
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
    </TooltipProvider>
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

  const handleDownload = (job: GovernanceExportJob) => {
    if (downloadingExportID === job.export_id) {
      toast("Download in progress", { description: "Hang tight — we're still preparing bytes." });
      return;
    }
    if (!job.download_url) {
      toast.error("This export is no longer downloadable", {
        description: "Create a fresh export to continue.",
      });
      return;
    }
    onDownload(job.export_id);
  };

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Created</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Scope</TableHead>
          <TableHead className="text-right">Files</TableHead>
          <TableHead className="text-right">Size</TableHead>
          <TableHead>Expires</TableHead>
          <TableHead>Artifact</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {exports.map((job) => (
          <TableRow key={job.export_id}>
            <TableCell>
              <HydrationSafeTime value={job.created_at} mode="local" />
            </TableCell>
            <TableCell>
              <StatusBadge status={job.state} />
            </TableCell>
            <TableCell className="text-xs text-muted-foreground">{job.scopes.join(", ")}</TableCell>
            <TableCell className="text-right tabular-nums">{job.files.length}</TableCell>
            <TableCell className="text-right tabular-nums">
              {formatBytes(job.artifact_bytes)}
            </TableCell>
            <TableCell className="text-xs text-muted-foreground">
              <ExpiryCell expiresAt={job.expires_at} />
            </TableCell>
            <TableCell>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => handleDownload(job)}
                data-testid={`download-data-export-${job.export_id}`}
              >
                {downloadingExportID === job.export_id ? "Downloading…" : "Download"}
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function ExpiryCell({ expiresAt }: { expiresAt: string }) {
  const [now, setNow] = useState<Date | null>(null);
  useEffect(() => {
    setNow(new Date());
    const id = window.setInterval(() => setNow(new Date()), 60_000);
    return () => window.clearInterval(id);
  }, []);
  if (!now) return <span suppressHydrationWarning>—</span>;
  const expires = new Date(expiresAt);
  if (Number.isNaN(expires.getTime())) return <span>—</span>;
  const remainingMs = expires.getTime() - now.getTime();
  if (remainingMs <= 0) {
    return <span className="text-destructive">expired</span>;
  }
  const hoursLeft = remainingMs / (1000 * 60 * 60);
  const className = hoursLeft < 24 ? "text-warning" : undefined;
  // formatRelative for "future" dates returns negative-prefixed strings
  // (e.g. "-3h ago"); strip the leading dash and trailing " ago" so the
  // cell reads "in 3h".
  const relative = formatRelative(expires, now).replace(/^-/, "").replace(" ago", "");
  return (
    <Tooltip>
      <TooltipTrigger render={<span className={className}>{`in ${relative}`}</span>} />
      <TooltipContent>{formatDateTimeUTC(expiresAt)}</TooltipContent>
    </Tooltip>
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
  const order: AuditOrder = search.order ?? DEFAULT_AUDIT_ORDER;
  const view: AuditView = resolveView(search);
  const anyDeviation =
    activeKeys.length > 0 ||
    search.cols !== undefined ||
    (search.limit !== undefined && search.limit !== DEFAULT_AUDIT_LIMIT) ||
    (search.order !== undefined && search.order !== DEFAULT_AUDIT_ORDER) ||
    search.view !== undefined ||
    hasCursor;

  const [timezone, setTimezone] = useTimezonePreference();

  return (
    <div className="flex flex-col gap-3">
      <ViewPresetBar view={view} />

      <AuditToolbar
        search={search}
        timezone={timezone}
        onTimezoneChange={setTimezone}
        visibleColumns={visibleColumns}
        anyDeviation={anyDeviation}
        activeKeyCount={activeKeys.length}
      />

      <ActiveFilterChips search={search} activeKeys={activeKeys} />

      <div className="overflow-x-auto rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              {visibleColumns.map((id) => (
                <TableHead key={id}>
                  <ColumnHeader id={id} order={order} timezone={timezone} />
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {events.length === 0 ? (
              <EmptyRow visibleColumns={visibleColumns} hasFilters={activeKeys.length > 0} />
            ) : (
              events.map((event) => (
                <AuditRow
                  key={event.event_id}
                  event={event}
                  visibleColumns={visibleColumns}
                  timezone={timezone}
                />
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <AuditFooter limit={limit} nextCursor={nextCursor} hasCursor={hasCursor} />
    </div>
  );
}

function EmptyRow({
  visibleColumns,
  hasFilters,
}: {
  visibleColumns: ReadonlyArray<AuditColumnId>;
  hasFilters: boolean;
}) {
  return (
    <TableRow>
      <TableCell
        colSpan={visibleColumns.length}
        className="py-8 text-center text-sm text-muted-foreground"
      >
        <div className="flex flex-col items-center gap-2">
          <span>No audit events match this view.</span>
          {hasFilters ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              render={
                <Link
                  to={GOVERNANCE_ROUTE}
                  search={clearFilterSearch}
                  data-testid="audit-empty-clear"
                />
              }
            >
              Clear filters
            </Button>
          ) : null}
        </div>
      </TableCell>
    </TableRow>
  );
}

function ViewPresetBar({ view }: { view: AuditView }) {
  return (
    <div className="flex flex-wrap items-center gap-2 text-xs">
      <span className="text-muted-foreground">View</span>
      <div className="inline-flex rounded-md border border-border bg-muted/30 p-0.5">
        <PresetButton view={view} target="high-risk" label="High-risk activity" />
        <PresetButton view={view} target="all" label="All activity" />
      </div>
      {view === "high-risk" ? (
        <span className="text-muted-foreground">
          Writes, deletes, exports, denials, and errors.
        </span>
      ) : null}
    </div>
  );
}

function PresetButton({
  view,
  target,
  label,
}: {
  view: AuditView;
  target: AuditView;
  label: string;
}) {
  const active = view === target;
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      // "high-risk" is the inferred default, so navigating to it strips the
      // URL param entirely — preserves the "no state when default" invariant.
      // "all" is always explicit.
      search={(prev) => ({
        ...prev,
        view: target === "high-risk" ? undefined : target,
        cursor: undefined,
      })}
      className={cn(
        "inline-flex items-center rounded-[5px] px-2.5 py-1 text-xs font-medium transition-colors",
        active
          ? "bg-background text-foreground shadow-sm"
          : "text-muted-foreground hover:text-foreground",
      )}
      data-testid={`audit-view-${target}`}
      aria-pressed={active}
    >
      {label}
    </Link>
  );
}

function AuditToolbar({
  search,
  timezone,
  onTimezoneChange,
  visibleColumns,
  anyDeviation,
  activeKeyCount,
}: {
  search: AuditSearch;
  timezone: TimezoneMode;
  onTimezoneChange: (next: TimezoneMode) => void;
  visibleColumns: ReadonlyArray<AuditColumnId>;
  anyDeviation: boolean;
  activeKeyCount: number;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Popover>
        <PopoverTrigger
          render={
            <Button type="button" variant="outline" size="sm" data-testid="audit-add-filter">
              <ListFilter aria-hidden="true" />
              Add filter
              {activeKeyCount > 0 ? (
                <Badge variant="secondary" className="ml-1 h-4 px-1 text-[0.6rem]">
                  {activeKeyCount}
                </Badge>
              ) : null}
            </Button>
          }
        />
        <PopoverContent align="start" className="w-80 gap-3">
          <PopoverHeader>
            <PopoverTitle>Filter audit events</PopoverTitle>
          </PopoverHeader>
          <div className="flex flex-col gap-4">
            {AUDIT_FILTER_GROUPS.map((group) => {
              const groupKeys = AUDIT_FILTER_KEYS.filter(
                (key) => AUDIT_FILTER_DEFINITIONS[key].group === group.id,
              );
              if (groupKeys.length === 0) return null;
              return (
                <div key={group.id} className="flex flex-col gap-2">
                  <span className="text-[0.65rem] font-semibold tracking-wide text-muted-foreground uppercase">
                    {group.label}
                  </span>
                  <div className="flex flex-col gap-3">
                    {groupKeys.map((key) => (
                      <FilterControl
                        key={key}
                        definition={AUDIT_FILTER_DEFINITIONS[key]}
                        value={search[key]}
                      />
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        </PopoverContent>
      </Popover>

      <ColumnsPopover visibleColumns={visibleColumns} />

      <TimezoneMenu timezone={timezone} onChange={onTimezoneChange} />

      <div className="ml-auto flex items-center gap-2">
        {activeKeyCount > 0 ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            render={
              <Link
                to={GOVERNANCE_ROUTE}
                search={clearFilterSearch}
                data-testid="audit-clear-filters"
              />
            }
          >
            Clear filters
          </Button>
        ) : null}
        {anyDeviation ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            render={
              // Functional form so retainSearchParams sees prev=this and
              // returns {}, otherwise a static search={} is merged by the
              // router and the retained params survive the reset.
              <Link to={GOVERNANCE_ROUTE} search={() => ({})} data-testid="audit-reset-defaults" />
            }
          >
            Reset to defaults
          </Button>
        ) : null}
      </div>
    </div>
  );
}

// clearFilterSearch keeps column/limit/order/view preset intact but removes
// every server-facing filter key and resets the cursor. Used by both the
// toolbar's Clear filters button and the empty-state link.
function clearFilterSearch(prev: AuditSearch): AuditSearch {
  const next: AuditSearch = { ...prev, cursor: undefined };
  for (const key of AUDIT_FILTER_KEYS) {
    next[key] = undefined;
  }
  return next;
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
  // retainSearchParams-managed keys like cols/limit/view stay in place, set
  // the new filter value (or undefined to clear), and explicitly null out
  // cursor because any filter change invalidates its (recorded_at, sequence)
  // tuple against the new WHERE clause.
  const applyFilter = (next: AuditSearch[AuditFilterKey]) => {
    void navigate({
      to: GOVERNANCE_ROUTE,
      search: (prev) => ({
        ...prev,
        [definition.key]: next,
        cursor: undefined,
      }),
    }).catch(reportAuditNavigationError);
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
    void navigate({
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
    }).catch(reportAuditNavigationError);
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
        <div className="flex flex-col gap-0.5">
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
                <span
                  className={cn(
                    "flex size-4 items-center justify-center rounded-sm border",
                    checked ? "border-primary bg-primary text-primary-foreground" : "border-border",
                  )}
                  aria-hidden="true"
                >
                  {checked ? <Check className="size-3" /> : null}
                </span>
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

function TimezoneMenu({
  timezone,
  onChange,
}: {
  timezone: TimezoneMode;
  onChange: (next: TimezoneMode) => void;
}) {
  // Base UI's Menu.RadioGroup / RadioItem crashed with #31 ("RadioGroup must
  // be inside a Menu") in this pinned version when combined with our
  // Popover/Tooltip mount tree. Using plain MenuItems with a trailing
  // checkmark renders identically and stays inside supported Menu contracts.
  const items: ReadonlyArray<{ value: TimezoneMode; label: string }> = [
    { value: "local", label: "Local time" },
    { value: "utc", label: "UTC" },
  ];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button type="button" variant="outline" size="sm" data-testid="audit-timezone">
            <Clock aria-hidden="true" />
            Times: {timezone === "utc" ? "UTC" : "Local"}
          </Button>
        }
      />
      <DropdownMenuContent align="start">
        <DropdownMenuGroup>
          {/* GroupLabel/Item from Base UI require a Group ancestor — otherwise
              runtime throws #31 (MenuGroupContext missing). */}
          <DropdownMenuLabel>Display timestamps in</DropdownMenuLabel>
          {items.map((item) => (
            <DropdownMenuItem
              key={item.value}
              onClick={() => onChange(item.value)}
              data-testid={`audit-timezone-${item.value}`}
              className="justify-between"
            >
              <span>{item.label}</span>
              {timezone === item.value ? <Check aria-hidden="true" className="size-3.5" /> : null}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
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
        const label = `${definition.label}: ${String(raw)}`;
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
  limit,
  nextCursor,
  hasCursor,
}: {
  limit: number;
  nextCursor: string;
  hasCursor: boolean;
}) {
  const navigate = routeApi.useNavigate();

  const onPrevious = () => {
    if (!hasCursor) {
      toast("You're on the first page", {
        description: "Newer events appear at the top automatically.",
      });
      return;
    }
    // Forward-only cursor API; browser history is the deterministic backward
    // walk because the server doesn't know what the previous cursor was.
    window.history.back();
  };

  const onNext = () => {
    if (!nextCursor) {
      toast("No more events in this view", {
        description: "Change filters or switch to All activity to see older events.",
      });
      return;
    }
    void navigate({
      to: GOVERNANCE_ROUTE,
      search: (prev) => ({ ...prev, cursor: nextCursor }),
    }).catch(reportAuditNavigationError);
  };

  return (
    <div className="flex flex-wrap items-center justify-end gap-3 text-xs text-muted-foreground">
      <LimitSelect value={limit} />
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={onPrevious}
        data-testid="audit-prev"
      >
        Previous
      </Button>
      <Button type="button" variant="outline" size="sm" onClick={onNext} data-testid="audit-next">
        Next
      </Button>
    </div>
  );
}

function LimitSelect({ value }: { value: number }) {
  const navigate = routeApi.useNavigate();
  return (
    <label className="flex items-center gap-2 whitespace-nowrap">
      <Select
        value={String(value)}
        onChange={(event) => {
          const next = Number(event.target.value) as AuditLimit;
          void navigate({
            to: GOVERNANCE_ROUTE,
            // Page size change resets the cursor — the (recorded_at,
            // sequence) tuple's "page" meaning only holds for the old size.
            search: (prev) => ({ ...prev, limit: next, cursor: undefined }),
          }).catch(reportAuditNavigationError);
        }}
        data-testid="audit-limit"
        className="w-20"
      >
        {AUDIT_LIMIT_CHOICES.map((choice) => (
          <option key={choice} value={choice}>
            {choice}
          </option>
        ))}
      </Select>
      <span>per page</span>
    </label>
  );
}

function ColumnHeader({
  id,
  order,
  timezone,
}: {
  id: AuditColumnId;
  order: AuditOrder;
  timezone: TimezoneMode;
}) {
  if (id === "time") {
    const nextOrder: AuditOrder = order === "desc" ? "asc" : "desc";
    const tooltip =
      order === "desc" ? "Newest first — click to reverse" : "Oldest first — click to reverse";
    const label = timezone === "utc" ? "Time (UTC)" : "Time (local)";
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <Link
              to={GOVERNANCE_ROUTE}
              search={(prev) => ({
                ...prev,
                order: nextOrder === DEFAULT_AUDIT_ORDER ? undefined : nextOrder,
                cursor: undefined,
              })}
              data-testid="audit-time-sort"
              className="inline-flex items-center gap-1 font-medium hover:text-foreground"
            >
              {label}
              {order === "desc" ? (
                <ArrowDown aria-hidden="true" className="size-3" />
              ) : (
                <ArrowUp aria-hidden="true" className="size-3" />
              )}
            </Link>
          }
        />
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
    );
  }
  return <span>{columnLabel(id)}</span>;
}

function AuditRow({
  event,
  visibleColumns,
  timezone,
}: {
  event: GovernanceAuditEvent;
  visibleColumns: ReadonlyArray<AuditColumnId>;
  timezone: TimezoneMode;
}) {
  const rowAccent =
    event.result === "error"
      ? "border-l-2 border-l-destructive"
      : event.result === "denied"
        ? "border-l-2 border-l-warning"
        : undefined;
  return (
    <TableRow className={cn("group/row hover:bg-muted/30", rowAccent)}>
      {visibleColumns.map((id) => (
        <TableCell key={id} className="align-top">
          {renderCell(id, event, timezone)}
        </TableCell>
      ))}
    </TableRow>
  );
}

function renderCell(id: AuditColumnId, event: GovernanceAuditEvent, timezone: TimezoneMode) {
  switch (id) {
    case "time":
      return <TimeCell value={event.recorded_at} timezone={timezone} />;
    case "id":
      return <EventIDCell eventID={event.event_id} />;
    case "risk":
      return <RiskBadge risk={event.risk_level} />;
    case "actor":
      return <ActorCell event={event} />;
    case "operation":
      return <OperationCell event={event} />;
    case "target":
      return <TargetCell event={event} />;
    case "result":
      return <ResultCell event={event} />;
    case "area":
      return <AreaCell event={event} />;
    case "location":
      return <LocationCell event={event} />;
    case "service":
      return <span className="text-xs text-muted-foreground">{event.service_name}</span>;
    case "sequence":
      return <span className="font-mono text-xs">{event.sequence}</span>;
    case "trace":
      return <TraceCell traceID={event.trace_id ?? undefined} />;
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

function TimeCell({ value, timezone }: { value: string; timezone: TimezoneMode }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="whitespace-nowrap font-mono text-xs">
            <HydrationSafeTime value={value} mode={timezone} />
          </span>
        }
      />
      <TooltipContent>
        <div className="flex flex-col gap-0.5">
          <span>{formatDateTimeUTC(value)}</span>
          <span className="text-muted-foreground">{value}</span>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

// HydrationSafeTime renders the SSR-safe UTC string on the server and swaps
// to the requested zone on first client render. This avoids the classic
// `Date` SSR/CSR mismatch because the browser's IANA zone isn't visible to
// the server.
function HydrationSafeTime({ value, mode }: { value: string; mode: TimezoneMode }) {
  const [hydrated, setHydrated] = useState(false);
  useEffect(() => {
    setHydrated(true);
  }, []);
  if (!hydrated || mode === "utc") {
    return <span suppressHydrationWarning>{formatDateTimeUTC(value)}</span>;
  }
  return <span suppressHydrationWarning>{formatDateTimeLocal(value)}</span>;
}

function EventIDCell({ eventID }: { eventID: string }) {
  const short = eventID.slice(0, 8);
  const onCopy = () => {
    navigator.clipboard?.writeText(eventID).then(
      () => toast("Event ID copied", { description: eventID }),
      () => toast.error("Unable to copy event ID"),
    );
  };
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            onClick={onCopy}
            className="inline-flex items-center gap-1 font-mono text-xs text-muted-foreground hover:text-foreground"
            data-testid="audit-event-id"
          >
            {short}
            <Copy className="size-3 opacity-0 transition-opacity group-hover/row:opacity-100" />
          </button>
        }
      />
      <TooltipContent>
        <div className="flex flex-col gap-0.5">
          <span className="font-mono text-xs">{eventID}</span>
          <span className="text-muted-foreground">Click to copy</span>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

function ActorCell({ event }: { event: GovernanceAuditEvent }) {
  const primary = actorPrimary(event);
  const secondary = actorSecondary(event);
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      // Clicking an actor pivots the whole view to that actor. cursor resets
      // because the composed predicate is new; view=all so the actor's reads
      // aren't filtered out by the high-risk predicate.
      search={(prev) => ({
        ...prev,
        actor_id: event.actor_id,
        cursor: undefined,
        view: "all",
      })}
      className="flex flex-col gap-0.5 text-left hover:text-foreground"
      data-testid="audit-cell-actor"
    >
      <span className="text-sm">{primary}</span>
      <span className="text-xs text-muted-foreground">{secondary}</span>
    </Link>
  );
}

function actorPrimary(event: GovernanceAuditEvent): string {
  if (event.actor_type === "api_credential" && event.credential_name) {
    return event.credential_name;
  }
  if (event.actor_display) return event.actor_display;
  if (event.actor_owner_display) return event.actor_owner_display;
  return event.actor_id;
}

function actorSecondary(event: GovernanceAuditEvent): string {
  const shortID =
    event.actor_id.length > 14
      ? `${event.actor_id.slice(0, 6)}…${event.actor_id.slice(-4)}`
      : event.actor_id;
  return `${event.actor_type} · ${shortID}`;
}

function OperationCell({ event }: { event: GovernanceAuditEvent }) {
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({
        ...prev,
        operation_id: event.operation_id,
        cursor: undefined,
        view: "all",
      })}
      className="flex flex-col gap-0.5 text-left hover:text-foreground"
      data-testid="audit-cell-operation"
    >
      <span className="text-sm">{event.operation_display || event.operation_id}</span>
      <span className="text-xs text-muted-foreground">{event.operation_type}</span>
    </Link>
  );
}

function TargetCell({ event }: { event: GovernanceAuditEvent }) {
  const target = event.target_display || event.target_id || "organization";
  const label = `${event.target_kind}: ${target}`;
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({
        ...prev,
        target_kind: event.target_kind,
        cursor: undefined,
        view: "all",
      })}
      className="whitespace-nowrap text-xs hover:text-foreground"
      data-testid="audit-cell-target"
    >
      {label}
    </Link>
  );
}

function AreaCell({ event }: { event: GovernanceAuditEvent }) {
  const value = event.source_product_area || event.service_name;
  if (!value) return <span className="text-xs text-muted-foreground">—</span>;
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({
        ...prev,
        source_product_area: event.source_product_area || undefined,
        service_name: event.source_product_area ? undefined : event.service_name || undefined,
        cursor: undefined,
        view: "all",
      })}
      className="text-xs hover:text-foreground"
      data-testid="audit-cell-area"
    >
      {value}
    </Link>
  );
}

function ResultCell({ event }: { event: GovernanceAuditEvent }) {
  if (event.result === "allowed") {
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span
              aria-label="allowed"
              className="inline-block size-1.5 rounded-full bg-muted-foreground/50"
            />
          }
        />
        <TooltipContent>allowed</TooltipContent>
      </Tooltip>
    );
  }
  const variant =
    event.result === "error" ? "destructive" : event.result === "denied" ? "warning" : "secondary";
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({
        ...prev,
        result: event.result as "allowed" | "denied" | "error",
        cursor: undefined,
        view: "all",
      })}
      className="flex flex-col gap-0.5"
      data-testid="audit-cell-result"
    >
      <Badge variant={variant}>{event.result}</Badge>
      {event.denial_reason ? (
        <span className="max-w-[14rem] truncate text-[0.65rem] text-muted-foreground">
          {event.denial_reason}
        </span>
      ) : null}
    </Link>
  );
}

function LocationCell({ event }: { event: GovernanceAuditEvent }) {
  // Display "—" for loopback IPs and rows without geo enrichment — a bare
  // "ipv4" tells no one anything and is the symptom of the missing GeoIP
  // pipeline. Once the edge fills client_ip (beyond 127.0.0.1) and GeoIP
  // lookup runs, this cell lights up on its own.
  const country = event.geo_country?.trim() ?? "";
  const ip = event.client_ip?.trim() ?? "";
  const isLoopback = ip === "" || ip === "127.0.0.1" || ip === "::1";
  if (country === "" && isLoopback) {
    return <span className="text-xs text-muted-foreground">—</span>;
  }
  const parts: Array<string> = [];
  if (country !== "") parts.push(country);
  if (!isLoopback) parts.push(ip);
  return <span className="text-xs text-muted-foreground">{parts.join(" · ")}</span>;
}

function TraceCell({ traceID }: { traceID: string | undefined }) {
  if (!traceID) return <span className="text-xs text-muted-foreground">—</span>;
  return <span className="font-mono text-xs text-muted-foreground">{traceID}</span>;
}

function columnLabel(id: AuditColumnId): string {
  switch (id) {
    case "time":
      return "Time";
    case "id":
      return "ID";
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
    case "area":
      return "Area";
    case "location":
      return "Location";
    case "service":
      return "Service";
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
  const variant =
    risk === "critical" || risk === "high" ? "warning" : risk === "low" ? "outline" : "secondary";
  return <Badge variant={variant}>{risk}</Badge>;
}

function StatusBadge({ status }: { status: string }) {
  const variant =
    status === "completed"
      ? "success"
      : status === "failed"
        ? "destructive"
        : status === "running"
          ? "info"
          : "secondary";
  return <Badge variant={variant}>{status}</Badge>;
}

function useTimezonePreference(): [TimezoneMode, (next: TimezoneMode) => void] {
  // SSR starts in UTC (the only zone the server knows) — after hydration we
  // read the persisted preference or fall back to the browser's local zone.
  const [timezone, setTimezone] = useState<TimezoneMode>("utc");
  useEffect(() => {
    try {
      const stored = window.localStorage.getItem(TZ_STORAGE_KEY);
      if (stored === "local" || stored === "utc") {
        setTimezone(stored);
        return;
      }
    } catch {
      // localStorage can throw in private-mode browsers; fall through to
      // local as the default without persisting.
    }
    setTimezone("local");
  }, []);
  const update = (next: TimezoneMode) => {
    setTimezone(next);
    try {
      window.localStorage.setItem(TZ_STORAGE_KEY, next);
    } catch {
      // Nothing to do — the preference lives only for this tab.
    }
  };
  return [timezone, update];
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

function reportAuditNavigationError(error: unknown) {
  toast.error("Failed to update audit view", { description: formatError(error) });
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
