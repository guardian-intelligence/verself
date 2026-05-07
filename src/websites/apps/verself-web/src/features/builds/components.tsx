import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useSignedInAuth } from "@verself/auth-web/react";
import { Badge } from "@verself/ui/components/ui/badge";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@verself/ui/components/ui/popover";
import { Skeleton } from "@verself/ui/components/ui/skeleton";
import { Table, TableBody, TableCell, TableRow } from "@verself/ui/components/ui/table";
import { cn } from "@verself/ui/lib/utils";
import {
  CalendarDays,
  ChevronDown,
  Circle,
  GitBranch,
  GitCommit,
  MoreHorizontal,
  Search,
  Triangle,
} from "lucide-react";
import { useOrgScopedPath, type OrgScopedPath } from "~/features/shell/org-routing";
import { buildRunsQuery } from "./queries";
import { toBuildsDashboardView, type BuildRunRow, type BuildRunStatusTone } from "./model";
import { dateFromDateOnly, type BuildsSearch } from "./search";

const statusToneClass: Record<BuildRunStatusTone, string> = {
  canceled: "bg-zinc-500",
  error: "bg-red-500",
  muted: "bg-muted-foreground",
  queued: "bg-amber-400",
  ready: "bg-emerald-400",
  running: "bg-blue-400",
};

const dateTitleFormatter = new Intl.DateTimeFormat("en-US", {
  month: "long",
  timeZone: "UTC",
  year: "numeric",
});

const dateInputFormatter = new Intl.DateTimeFormat("en-US", {
  day: "numeric",
  month: "long",
  timeZone: "UTC",
  year: "numeric",
});

export function BuildsDashboard({ search }: { readonly search: BuildsSearch }) {
  const auth = useSignedInAuth();
  const page = useSuspenseQuery(buildRunsQuery(auth)).data;
  const dashboard = toBuildsDashboardView(page);

  return (
    <div className="flex flex-col gap-3">
      <BuildsToolbar search={search} />
      {dashboard.rows.length > 0 ? <BuildRunsTable rows={dashboard.rows} /> : <BuildRunsEmpty />}
    </div>
  );
}

export function BuildsDashboardSkeleton() {
  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-2 lg:grid-cols-[1.3fr_1fr_1fr_1fr_1fr_auto]">
        <Skeleton className="h-10" />
        <Skeleton className="h-10" />
        <Skeleton className="h-10" />
        <Skeleton className="h-10" />
        <Skeleton className="h-10" />
        <Skeleton className="h-10 w-36" />
      </div>
      <div className="overflow-hidden rounded-md border border-border/70">
        {Array.from({ length: 10 }, (_, index) => (
          <div
            key={index}
            className="grid h-[4.5rem] grid-cols-[1.25fr_0.85fr_1.25fr_2fr_1.25fr_auto] items-center gap-4 border-b px-4 last:border-b-0"
          >
            <Skeleton className="h-9" />
            <Skeleton className="h-8" />
            <Skeleton className="h-8" />
            <Skeleton className="h-8" />
            <Skeleton className="h-8" />
            <Skeleton className="size-8" />
          </div>
        ))}
      </div>
    </div>
  );
}

function BuildsToolbar({ search }: { readonly search: BuildsSearch }) {
  return (
    <div className="grid gap-2 lg:grid-cols-[1.3fr_1fr_1fr_1fr_1fr_auto]">
      <DateRangeFilter search={search} />
      <FilterButton icon={<Search aria-hidden="true" />} label="All Authors..." />
      <FilterButton label="All Environments" />
      <FilterButton icon={<Search aria-hidden="true" />} label="All Repositories" />
      <FilterButton icon={<Search aria-hidden="true" />} label="All Branches..." />
      <StatusFilterButton />
    </div>
  );
}

function DateRangeFilter({ search }: { readonly search: BuildsSearch }) {
  const calendar = monthGrid(search.from, search.to);
  const startDate = dateInputFormatter.format(dateFromDateOnly(search.from));
  const endDate = dateInputFormatter.format(dateFromDateOnly(search.to));
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            className="h-10 justify-start gap-2 bg-input/20 px-3 text-left font-normal"
          >
            <CalendarDays className="size-4 text-muted-foreground" aria-hidden="true" />
            <span className="truncate">Select Date Range</span>
          </Button>
        }
      />
      <PopoverContent align="start" sideOffset={8} className="w-72 gap-0 overflow-hidden p-0">
        <div className="flex items-center justify-between border-b px-3 py-3">
          <div className="text-sm font-medium">
            {dateTitleFormatter.format(calendar.anchorDate)}
          </div>
          <div className="flex items-center gap-1 text-muted-foreground">
            <Button type="button" variant="ghost" size="icon-xs" aria-label="Previous month">
              <ChevronDown className="size-3 rotate-90" />
            </Button>
            <Button type="button" variant="ghost" size="icon-xs" aria-label="Next month">
              <ChevronDown className="size-3 -rotate-90" />
            </Button>
          </div>
        </div>
        <div className="grid grid-cols-7 gap-1 px-3 pt-3 text-center text-xs text-muted-foreground">
          {["S", "M", "T", "W", "T", "F", "S"].map((day, index) => (
            <div key={`${day}-${index}`} className="h-6 leading-6">
              {day}
            </div>
          ))}
          {calendar.days.map((day) => (
            <button
              key={day.key}
              type="button"
              className={cn(
                "h-8 rounded-md text-xs text-foreground transition-colors hover:bg-muted",
                !day.currentMonth && "text-muted-foreground/60",
                day.inRange && !day.selected && "bg-primary/10 text-primary",
                day.selected && "bg-primary text-primary-foreground hover:bg-primary",
              )}
            >
              {day.label}
            </button>
          ))}
        </div>
        <div className="mt-3 flex flex-col gap-2 border-t p-3">
          <DateRangeInput label="Start" date={startDate} time="12:00 AM" />
          <DateRangeInput label="End" date={endDate} time="11:59 PM" />
          <Button type="button" variant="outline" className="w-full">
            Apply
          </Button>
          <button
            type="button"
            className="truncate text-center text-xs text-muted-foreground hover:text-foreground"
          >
            Timezone ({search.tz})
          </button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function DateRangeInput({
  date,
  label,
  time,
}: {
  readonly date: string;
  readonly label: string;
  readonly time: string;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-muted-foreground">
      <span>{label}</span>
      <span className="grid grid-cols-[1fr_6.125rem] gap-2">
        <Input readOnly value={date} className="h-9 text-sm text-foreground" />
        <Input readOnly value={time} className="h-9 text-sm text-foreground" />
      </span>
    </label>
  );
}

function FilterButton({ icon, label }: { readonly icon?: ReactNode; readonly label: string }) {
  return (
    <Button
      type="button"
      variant="outline"
      className="h-10 justify-between gap-2 bg-input/20 px-3 text-left font-normal text-muted-foreground"
    >
      <span className="flex min-w-0 items-center gap-2">
        {icon ? <span className="text-muted-foreground [&_svg]:size-4">{icon}</span> : null}
        <span className="truncate">{label}</span>
      </span>
      <ChevronDown className="size-4 text-muted-foreground" aria-hidden="true" />
    </Button>
  );
}

function StatusFilterButton() {
  return (
    <Button
      type="button"
      variant="outline"
      className="h-10 justify-between gap-2 bg-input/20 px-3 text-left font-normal"
    >
      <span className="flex min-w-0 items-center gap-2">
        <span className="flex -space-x-1">
          <span className="size-2.5 rounded-full bg-emerald-400 ring-2 ring-background" />
          <span className="size-2.5 rounded-full bg-blue-400 ring-2 ring-background" />
          <span className="size-2.5 rounded-full bg-amber-400 ring-2 ring-background" />
          <span className="size-2.5 rounded-full bg-red-500 ring-2 ring-background" />
        </span>
        <span>Status</span>
        <Badge variant="outline" className="h-5 px-1.5 text-[0.65rem]">
          5/6
        </Badge>
      </span>
      <ChevronDown className="size-4 text-muted-foreground" aria-hidden="true" />
    </Button>
  );
}

function BuildRunsTable({ rows }: { readonly rows: readonly BuildRunRow[] }) {
  return (
    <div className="overflow-hidden rounded-md border border-border/70">
      <Table className="min-w-[64rem] table-fixed">
        <TableBody>
          {rows.map((row) => (
            <BuildRunTableRow key={row.executionId} row={row} />
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function BuildRunTableRow({ row }: { readonly row: BuildRunRow }) {
  const href = useOrgScopedPath(`/executions/${row.executionId}` as OrgScopedPath);
  return (
    <TableRow className="h-[4.5rem] hover:bg-muted/35" data-testid="build-run-row">
      <TableCell className="w-[18%] px-4">
        <div className="flex min-w-0 flex-col gap-0.5">
          <Link
            to={href}
            className="truncate font-mono text-sm font-semibold hover:underline"
            data-testid="build-run-link"
          >
            {row.shortId}
          </Link>
          <div className="flex min-w-0 items-center gap-1.5 text-sm text-muted-foreground">
            <span className="truncate">{row.environment}</span>
            {row.showCurrent ? (
              <Badge variant="info" className="h-4 px-1.5 text-[0.625rem]">
                Current
              </Badge>
            ) : null}
          </div>
        </div>
      </TableCell>
      <TableCell className="w-[13%]">
        <div className="flex min-w-0 items-start gap-2">
          <span
            className={cn("mt-1.5 size-2.5 shrink-0 rounded-full", statusToneClass[row.statusTone])}
          />
          <div className="min-w-0">
            <div className="truncate text-sm text-foreground" data-testid="build-run-status">
              {row.statusLabel}
            </div>
            <div className="truncate text-sm text-muted-foreground">{row.durationLabel}</div>
          </div>
        </div>
      </TableCell>
      <TableCell className="w-[18%]">
        <div className="flex min-w-0 items-center gap-3">
          <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-black text-white ring-1 ring-white/10">
            <Triangle className="size-3 fill-current" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <div className="truncate text-sm text-foreground">{row.projectLabel}</div>
            <div className="truncate text-xs text-muted-foreground">{row.repositoryFullName}</div>
          </div>
        </div>
      </TableCell>
      <TableCell className="w-[30%]">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex min-w-0 items-center gap-2 text-sm text-foreground">
            <GitBranch className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span className="truncate">{row.branch}</span>
          </div>
          <div className="flex min-w-0 items-center gap-2 text-sm text-foreground">
            <GitCommit className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span className="shrink-0 font-mono text-xs">{row.commitSha}</span>
            <span className="truncate">{row.commitLabel}</span>
          </div>
        </div>
      </TableCell>
      <TableCell className="w-[18%] text-right">
        <div className="flex min-w-0 items-center justify-end gap-3">
          <div className="min-w-0 text-right">
            <div className="truncate text-sm text-muted-foreground">
              {row.runDateLabel} by {row.actorLabel}
            </div>
            <div className="truncate text-xs text-muted-foreground/80">
              {row.providerLabel} · {row.sourceLabel}
            </div>
          </div>
          <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/20 text-xs font-semibold text-primary">
            {row.actorInitial}
          </span>
        </div>
      </TableCell>
      <TableCell className="w-10 pr-4 text-right">
        <Button type="button" variant="ghost" size="icon-sm" aria-label="Build actions">
          <MoreHorizontal className="size-4" />
        </Button>
      </TableCell>
    </TableRow>
  );
}

function BuildRunsEmpty() {
  return (
    <div className="flex min-h-80 flex-col items-center justify-center rounded-md border border-border/70 text-center">
      <Circle className="mb-3 size-8 text-muted-foreground" aria-hidden="true" />
      <div className="text-sm font-medium">No deployments found</div>
      <div className="mt-1 max-w-sm text-sm text-muted-foreground">
        New GitHub runner and scheduled sandbox runs will appear here.
      </div>
    </div>
  );
}

type CalendarDay = {
  readonly currentMonth: boolean;
  readonly inRange: boolean;
  readonly key: string;
  readonly label: number;
  readonly selected: boolean;
};

function monthGrid(
  from: string,
  to: string,
): { readonly anchorDate: Date; readonly days: readonly CalendarDay[] } {
  const anchorDate = dateFromDateOnly(from);
  const firstOfMonth = new Date(Date.UTC(anchorDate.getUTCFullYear(), anchorDate.getUTCMonth(), 1));
  const startOffset = firstOfMonth.getUTCDay();
  const start = new Date(firstOfMonth);
  start.setUTCDate(firstOfMonth.getUTCDate() - startOffset);
  const selectedFrom = from <= to ? from : to;
  const selectedTo = from <= to ? to : from;
  const days = Array.from({ length: 42 }, (_, index) => {
    const date = new Date(start);
    date.setUTCDate(start.getUTCDate() + index);
    const key = date.toISOString().slice(0, 10);
    return {
      currentMonth: date.getUTCMonth() === anchorDate.getUTCMonth(),
      inRange: key >= selectedFrom && key <= selectedTo,
      key,
      label: date.getUTCDate(),
      selected: key === selectedFrom || key === selectedTo,
    };
  });
  return { anchorDate, days };
}
