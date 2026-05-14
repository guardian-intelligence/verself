import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { getRouteApi, Link, useRouter } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, Copy, Download, FilterX, Plus } from "lucide-react";
import { Badge } from "@verself/ui/components/ui/badge";
import { Button } from "@verself/ui/components/ui/button";
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
import type { GovernanceAPIActivity, GovernanceExportJob } from "~/server-fns/api";
import {
  AUDIT_FILTER_DEFINITIONS,
  AUDIT_FILTER_KEYS,
  AUDIT_LIMIT_CHOICES,
  DEFAULT_AUDIT_LIMIT,
  DEFAULT_AUDIT_ORDER,
  activeFilters,
  withoutAuditFilters,
  type AuditFilterKey,
  type AuditLimit,
  type AuditOrder,
  type AuditSearch,
  type FilterDefinition,
} from "./audit-search";

const GOVERNANCE_ROUTE = "." as const;
const routeApi = getRouteApi("/_shell/_authenticated/$orgSlug/settings/governance");
const TZ_STORAGE_KEY = "verself:governance.audit.timezone";
type TimezoneMode = "local" | "utc";

interface GovernanceSettingsProps {
  apiActivities: Array<GovernanceAPIActivity>;
  activityLimit: number;
  activityNextCursor: string;
  exports: Array<GovernanceExportJob>;
  search: AuditSearch;
}

export function GovernanceSettings({
  apiActivities,
  activityLimit,
  activityNextCursor,
  exports,
  search,
}: GovernanceSettingsProps) {
  const router = useRouter();
  const createExport = useMutation({
    mutationFn: () =>
      createGovernanceDataExport({
        data: {
          include_logs: false,
          scopes: ["identity", "billing", "sandbox", "api_activity"],
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
      toast("A data export is already being prepared");
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
                <Plus aria-hidden="true" />
                {createExport.isPending ? "Creating export" : "Create data export"}
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
              <SectionTitle>API activity</SectionTitle>
              <SectionDescription>
                Tamper-evident OCSF API Activity records for this organization.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <APIActivityTrail
            activities={apiActivities}
            limit={activityLimit}
            nextCursor={activityNextCursor}
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
      toast("Download in progress");
      return;
    }
    if (!job.download_url) {
      toast.error("This export is no longer downloadable");
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
                <Download aria-hidden="true" />
                {downloadingExportID === job.export_id ? "Downloading" : "Download"}
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
  if (!now) return <span suppressHydrationWarning>-</span>;
  const expires = new Date(expiresAt);
  if (Number.isNaN(expires.getTime())) return <span>-</span>;
  const remainingMs = expires.getTime() - now.getTime();
  if (remainingMs <= 0) {
    return <span className="text-destructive">expired</span>;
  }
  const relative = formatRelative(expires, now).replace(/^-/, "").replace(" ago", "");
  return (
    <Tooltip>
      <TooltipTrigger render={<span>{`in ${relative}`}</span>} />
      <TooltipContent>{formatDateTimeUTC(expiresAt)}</TooltipContent>
    </Tooltip>
  );
}

function APIActivityTrail({
  activities,
  limit,
  nextCursor,
  search,
}: {
  activities: Array<GovernanceAPIActivity>;
  limit: number;
  nextCursor: string;
  search: AuditSearch;
}) {
  const [timezone, setTimezone] = useTimezonePreference();
  const activeKeys = activeFilters(search);
  const hasCursor = search.cursor !== undefined && search.cursor !== "";
  const order: AuditOrder = search.order ?? DEFAULT_AUDIT_ORDER;

  return (
    <div className="flex flex-col gap-3">
      <AuditToolbar
        search={search}
        timezone={timezone}
        onTimezoneChange={setTimezone}
        activeKeyCount={activeKeys.length}
      />
      <ActiveFilterChips search={search} activeKeys={activeKeys} />
      <div className="overflow-x-auto rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>
                <TimeHeader order={order} timezone={timezone} />
              </TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Actor</TableHead>
              <TableHead>Operation</TableHead>
              <TableHead>Resource</TableHead>
              <TableHead>Service</TableHead>
              <TableHead>Permission</TableHead>
              <TableHead>Trace</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {activities.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="py-8 text-center text-sm text-muted-foreground">
                  No API activity records match this query.
                </TableCell>
              </TableRow>
            ) : (
              activities.map((event) => (
                <APIActivityRow key={event.metadata_uid} event={event} timezone={timezone} />
              ))
            )}
          </TableBody>
        </Table>
      </div>
      <AuditFooter limit={limit} nextCursor={nextCursor} hasCursor={hasCursor} />
    </div>
  );
}

function AuditToolbar({
  search,
  timezone,
  onTimezoneChange,
  activeKeyCount,
}: {
  search: AuditSearch;
  timezone: TimezoneMode;
  onTimezoneChange: (next: TimezoneMode) => void;
  activeKeyCount: number;
}) {
  return (
    <div className="flex flex-wrap items-end gap-2">
      {AUDIT_FILTER_KEYS.map((key) => (
        <FilterControl key={key} definition={AUDIT_FILTER_DEFINITIONS[key]} value={search[key]} />
      ))}
      <TimezoneSelect value={timezone} onChange={onTimezoneChange} />
      {activeKeyCount > 0 ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          render={
            <Link
              to={GOVERNANCE_ROUTE}
              search={(prev) => withoutAuditFilters(prev)}
              data-testid="audit-clear-filters"
            />
          }
        >
          <FilterX aria-hidden="true" />
          Clear
        </Button>
      ) : null}
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
  const [draft, setDraft] = useState(typeof value === "string" ? value : "");

  useEffect(() => {
    setDraft(typeof value === "string" ? value : "");
  }, [value]);

  const applyFilter = (next: string | undefined) => {
    void navigate({
      to: GOVERNANCE_ROUTE,
      search: (prev) => ({ ...prev, [definition.key]: next, cursor: undefined }),
    }).catch(reportAuditNavigationError);
  };

  if (definition.kind === "enum" && definition.options) {
    return (
      <label className="flex min-w-36 flex-col gap-1 text-xs">
        <span className="font-medium text-foreground">{definition.label}</span>
        <Select
          value={typeof value === "string" ? value : ""}
          onChange={(event) => {
            const next = event.target.value.trim();
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
      </label>
    );
  }

  const commit = () => {
    const trimmed = draft.trim();
    const next = trimmed === "" ? undefined : trimmed;
    if (next !== value) applyFilter(next);
  };

  return (
    <label className="flex min-w-40 flex-col gap-1 text-xs">
      <span className="font-medium text-foreground">{definition.label}</span>
      <Input
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            commit();
          }
        }}
        placeholder={definition.help}
        data-testid={`audit-filter-${definition.key}`}
      />
    </label>
  );
}

function TimezoneSelect({
  value,
  onChange,
}: {
  value: TimezoneMode;
  onChange: (next: TimezoneMode) => void;
}) {
  return (
    <label className="flex min-w-32 flex-col gap-1 text-xs">
      <span className="font-medium text-foreground">Time</span>
      <Select
        value={value}
        onChange={(event) => onChange(event.target.value as TimezoneMode)}
        data-testid="audit-timezone"
      >
        <option value="local">Local</option>
        <option value="utc">UTC</option>
      </Select>
    </label>
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
        const value = search[key];
        return (
          <Link
            key={key}
            to={GOVERNANCE_ROUTE}
            search={(prev) => ({ ...prev, [key]: undefined, cursor: undefined })}
            className="inline-flex items-center gap-1 rounded-full border border-border bg-muted/40 px-2 py-0.5 text-xs text-foreground transition-colors hover:border-destructive hover:bg-destructive/10 hover:text-destructive"
            data-testid={`audit-chip-${key}`}
          >
            <span>{`${definition.label}: ${String(value)}`}</span>
          </Link>
        );
      })}
    </div>
  );
}

function TimeHeader({ order, timezone }: { order: AuditOrder; timezone: TimezoneMode }) {
  const nextOrder: AuditOrder = order === "desc" ? "asc" : "desc";
  return (
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
      {timezone === "utc" ? "Time (UTC)" : "Time"}
      {order === "desc" ? (
        <ArrowDown aria-hidden="true" className="size-3" />
      ) : (
        <ArrowUp aria-hidden="true" className="size-3" />
      )}
    </Link>
  );
}

function APIActivityRow({
  event,
  timezone,
}: {
  event: GovernanceAPIActivity;
  timezone: TimezoneMode;
}) {
  const rowAccent =
    event.status_id === 2
      ? "border-l-2 border-l-destructive"
      : event.action === "Denied"
        ? "border-l-2 border-l-warning"
        : undefined;
  return (
    <TableRow className={cn("group/row hover:bg-muted/30", rowAccent)}>
      <TableCell className="align-top">
        <TimeCell value={event.time} timezone={timezone} sequence={event.sequence} />
      </TableCell>
      <TableCell className="align-top">
        <ActivityStatusBadge status={event.status} statusID={event.status_id} />
      </TableCell>
      <TableCell className="align-top">
        <ActorCell event={event} />
      </TableCell>
      <TableCell className="align-top">
        <OperationCell event={event} />
      </TableCell>
      <TableCell className="align-top">
        <ResourceCell event={event} />
      </TableCell>
      <TableCell className="align-top">
        <ServiceCell event={event} />
      </TableCell>
      <TableCell className="max-w-[18rem] align-top">
        <span className="font-mono text-xs text-muted-foreground">{event.permission}</span>
      </TableCell>
      <TableCell className="align-top">
        <TraceCell traceID={event.trace_uid} metadataUID={event.metadata_uid} />
      </TableCell>
    </TableRow>
  );
}

function TimeCell({
  value,
  timezone,
  sequence,
}: {
  value: string;
  timezone: TimezoneMode;
  sequence: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="flex flex-col gap-0.5 whitespace-nowrap">
            <span className="font-mono text-xs">
              <HydrationSafeTime value={value} mode={timezone} />
            </span>
            <span className="font-mono text-[0.65rem] text-muted-foreground">{sequence}</span>
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

function ActivityStatusBadge({ status, statusID }: { status: string; statusID: number }) {
  const variant = statusID === 1 ? "success" : "destructive";
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({
        ...prev,
        status_id: String(statusID) as AuditSearch["status_id"],
        cursor: undefined,
      })}
      data-testid="audit-cell-status"
    >
      <Badge variant={variant}>{status}</Badge>
    </Link>
  );
}

function ActorCell({ event }: { event: GovernanceAPIActivity }) {
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({ ...prev, actor_uid: event.actor_uid, cursor: undefined })}
      className="flex flex-col gap-0.5 text-left hover:text-foreground"
      data-testid="audit-cell-actor"
    >
      <span className="max-w-[16rem] truncate text-sm">{event.actor_uid}</span>
      <span className="text-xs text-muted-foreground">{event.actor_type}</span>
    </Link>
  );
}

function OperationCell({ event }: { event: GovernanceAPIActivity }) {
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({ ...prev, api_operation: event.api_operation, cursor: undefined })}
      className="flex flex-col gap-0.5 text-left hover:text-foreground"
      data-testid="audit-cell-event"
    >
      <span className="text-sm">{event.api_operation}</span>
      <span className="font-mono text-xs text-muted-foreground">{event.activity_name}</span>
    </Link>
  );
}

function ResourceCell({ event }: { event: GovernanceAPIActivity }) {
  const resourceUID = event.primary_resource_uid || event.org_id;
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({
        ...prev,
        resource_type: event.primary_resource_type,
        cursor: undefined,
      })}
      className="flex flex-col gap-0.5 text-left hover:text-foreground"
      data-testid="audit-cell-target"
    >
      <span className="text-sm">{event.primary_resource_type}</span>
      <span className="max-w-[14rem] truncate font-mono text-xs text-muted-foreground">
        {resourceUID}
      </span>
    </Link>
  );
}

function ServiceCell({ event }: { event: GovernanceAPIActivity }) {
  return (
    <Link
      to={GOVERNANCE_ROUTE}
      search={(prev) => ({ ...prev, api_service: event.api_service, cursor: undefined })}
      className="text-xs text-muted-foreground hover:text-foreground"
      data-testid="audit-cell-source"
    >
      {event.api_service}
    </Link>
  );
}

function TraceCell({ traceID, metadataUID }: { traceID: string | undefined; metadataUID: string }) {
  const value = traceID || metadataUID;
  const label = traceID ? traceID.slice(0, 12) : metadataUID.slice(0, 8);
  const onCopy = () => {
    navigator.clipboard?.writeText(value).then(
      () => toast("Copied", { description: value }),
      () => toast.error("Unable to copy value"),
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
            data-testid="audit-copy-trace"
          >
            {label}
            <Copy className="size-3 opacity-0 transition-opacity group-hover/row:opacity-100" />
          </button>
        }
      />
      <TooltipContent>
        <div className="flex flex-col gap-0.5">
          <span className="font-mono text-xs">{value}</span>
          <span className="text-muted-foreground">{traceID ? "Trace ID" : "Event ID"}</span>
        </div>
      </TooltipContent>
    </Tooltip>
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
      toast("Already on the first page");
      return;
    }
    window.history.back();
  };

  const onNext = () => {
    if (!nextCursor) {
      toast("No more events in this query");
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
            search: (prev) => ({
              ...prev,
              limit: next === DEFAULT_AUDIT_LIMIT ? undefined : next,
              cursor: undefined,
            }),
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

function StatusBadge({ status }: { status: string }) {
  const variant =
    status === "ready" || status === "completed"
      ? "success"
      : status === "failed"
        ? "destructive"
        : status === "running"
          ? "info"
          : "secondary";
  return <Badge variant={variant}>{status}</Badge>;
}

function useTimezonePreference(): [TimezoneMode, (next: TimezoneMode) => void] {
  const [timezone, setTimezone] = useState<TimezoneMode>("utc");
  useEffect(() => {
    try {
      const stored = window.localStorage.getItem(TZ_STORAGE_KEY);
      if (stored === "local" || stored === "utc") {
        setTimezone(stored);
        return;
      }
    } catch {
      setTimezone("local");
      return;
    }
    setTimezone("local");
  }, []);
  const update = (next: TimezoneMode) => {
    setTimezone(next);
    try {
      window.localStorage.setItem(TZ_STORAGE_KEY, next);
    } catch {
      return;
    }
  };
  return [timezone, update];
}

function formatBytes(value: number | string) {
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
  toast.error("Failed to update audit query", { description: formatError(error) });
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
