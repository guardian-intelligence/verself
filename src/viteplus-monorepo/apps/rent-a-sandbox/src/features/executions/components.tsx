import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { ClientOnly, Link } from "@tanstack/react-router";
import { useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import { useStickToBottom } from "use-stick-to-bottom";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Field, FieldError, FieldLabel } from "@forge-metal/ui/components/ui/field";
import {
  Page,
  PageEyebrow,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import { Textarea } from "@forge-metal/ui/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "@forge-metal/ui/components/ui/pagination";
import { Callout } from "~/components/callout";
import { EmptyState } from "~/components/empty-state";
import { ErrorCallout } from "~/components/error-callout";
import { type ElectricExecution } from "~/lib/collections";
import { formatDateTimeUTC } from "~/lib/format";
import { useExecutionLogs, useExecutionRows } from "./live";
import { executionQuery } from "./queries";
import { ExecutionStatusBadge, isExecutionActiveStatus } from "./status";
import { DEFAULT_RUN_COMMAND, validateRunCommand } from "./validation";
import { useCreateExecutionMutation, type CreateExecutionResult } from "./mutations";

export function ExecutionListPanel({ orgId }: { orgId: string }) {
  return (
    <ClientOnly fallback={<ExecutionListLoading />}>
      <ExecutionListPanelContent orgId={orgId} />
    </ClientOnly>
  );
}

function ExecutionListPanelContent({ orgId }: { orgId: string }) {
  const rows = useExecutionRows(orgId);

  if (rows.isLoading || rows.isIdle) {
    return <ExecutionListLoading />;
  }

  if (rows.isError) {
    return <ExecutionListError status={rows.status} />;
  }

  if (rows.isEmpty) {
    return <ExecutionListEmpty />;
  }

  return <ExecutionTable executions={rows.executions} />;
}

export function ExecutionDetailPanel({ executionId }: { executionId: string }) {
  return (
    <ClientOnly fallback={<ExecutionDetailLoading executionId={executionId} />}>
      <ExecutionDetailPanelContent executionId={executionId} />
    </ClientOnly>
  );
}

function ExecutionDetailPanelContent({ executionId }: { executionId: string }) {
  const auth = useSignedInAuth();
  const execution = useSuspenseQuery(executionQuery(auth, executionId)).data;

  const attempt = execution.latest_attempt;
  const isRunning = isExecutionActiveStatus(execution.status);

  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/executions" className="hover:text-foreground">
              ← Executions
            </Link>
          </PageEyebrow>
          <div className="flex flex-wrap items-center gap-3">
            <PageTitle className="font-mono">{execution.execution_id.slice(0, 8)}</PageTitle>
            <ExecutionStatusBadge status={execution.status} />
          </div>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection>
          <ExecutionTimingSummary execution={execution} isRunning={isRunning} />
        </PageSection>

        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Output</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <ClientOnly fallback={<ExecutionLogsLoading isRunning={isRunning} />}>
            <ExecutionLogsPanel attemptId={attempt.attempt_id} isRunning={isRunning} />
          </ClientOnly>
        </PageSection>
      </PageSections>
    </Page>
  );
}

function ExecutionDetailLoading({ executionId }: { executionId: string }) {
  const executionPrefix = executionId.slice(0, 8);
  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/executions" className="hover:text-foreground">
              ← Executions
            </Link>
          </PageEyebrow>
          <PageTitle className="font-mono">{executionPrefix}</PageTitle>
        </PageHeaderContent>
      </PageHeader>
      <PageSections>
        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Output</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <ExecutionLogsLoading isRunning />
        </PageSection>
      </PageSections>
    </Page>
  );
}

export function ExecutionSubmissionForm({
  onSuccess,
}: {
  onSuccess: (execution: CreateExecutionResult) => void | Promise<void>;
}) {
  const mutation = useCreateExecutionMutation({ onSuccess });
  const form = useForm({
    defaultValues: {
      runCommand: DEFAULT_RUN_COMMAND,
    },
    onSubmit: async ({ value }) => {
      mutation.reset();
      await mutation.mutateAsync({
        run_command: value.runCommand,
      });
    },
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        e.stopPropagation();
        void form.handleSubmit();
      }}
      className="space-y-4"
    >
      <form.Field
        name="runCommand"
        validators={{
          onChange: ({ value }) => validateRunCommand(value),
        }}
      >
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Run command</FieldLabel>
            <Textarea
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              rows={4}
              className="font-mono"
            />
            {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
              <FieldError>{field.state.meta.errors[0]}</FieldError>
            ) : null}
          </Field>
        )}
      </form.Field>

      {mutation.error ? (
        <ErrorCallout title="Execution submission failed" error={mutation.error} />
      ) : null}

      <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
        {([canSubmit, isSubmitting]) => (
          <Button type="submit" disabled={!canSubmit || isSubmitting || mutation.isPending}>
            {mutation.isPending || isSubmitting ? "Submitting…" : "Submit execution"}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}

function ExecutionLogsPanel({ attemptId, isRunning }: { attemptId: string; isRunning: boolean }) {
  const logs = useExecutionLogs(attemptId);

  if (logs.isLoading || logs.isIdle) {
    return <ExecutionLogsLoading isRunning={isRunning} />;
  }

  if (logs.isError) {
    return <ExecutionLogsError status={logs.status} isRunning={isRunning} />;
  }

  if (logs.isEmpty) {
    return <ExecutionLogsEmpty isRunning={isRunning} />;
  }

  return <ExecutionLogsBody logText={logs.logText} />;
}

const EXECUTIONS_PAGE_SIZE = 25;

function ExecutionTable({ executions }: { executions: ElectricExecution[] }) {
  const [page, setPage] = useState(1);
  const pageCount = Math.max(1, Math.ceil(executions.length / EXECUTIONS_PAGE_SIZE));
  const currentPage = Math.min(page, pageCount);
  const pageRows = useMemo(
    () =>
      executions.slice(
        (currentPage - 1) * EXECUTIONS_PAGE_SIZE,
        currentPage * EXECUTIONS_PAGE_SIZE,
      ),
    [executions, currentPage],
  );

  const goTo = (next: number) => setPage(Math.min(Math.max(1, next), pageCount));

  return (
    <div className="space-y-4">
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Created</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pageRows.map((execution) => (
              <TableRow key={execution.execution_id}>
                <TableCell>
                  <Link
                    to="/executions/$executionId"
                    params={{ executionId: execution.execution_id }}
                    className="font-mono text-primary hover:underline"
                  >
                    {execution.execution_id.slice(0, 8)}
                  </Link>
                </TableCell>
                <TableCell>
                  <ExecutionStatusBadge status={execution.status} />
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {formatDateTimeUTC(execution.created_at)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {pageCount > 1 ? (
        <ExecutionTablePagination
          currentPage={currentPage}
          pageCount={pageCount}
          totalCount={executions.length}
          onChange={goTo}
        />
      ) : null}
    </div>
  );
}

function ExecutionTablePagination({
  currentPage,
  pageCount,
  totalCount,
  onChange,
}: {
  currentPage: number;
  pageCount: number;
  totalCount: number;
  onChange: (page: number) => void;
}) {
  const pages = buildPageList(currentPage, pageCount);
  const start = (currentPage - 1) * EXECUTIONS_PAGE_SIZE + 1;
  const end = Math.min(currentPage * EXECUTIONS_PAGE_SIZE, totalCount);

  const handle =
    (next: number): React.MouseEventHandler<HTMLAnchorElement> =>
    (event) => {
      event.preventDefault();
      onChange(next);
    };

  return (
    <div className="flex flex-col items-center gap-2 sm:flex-row sm:justify-between">
      <p className="text-xs text-muted-foreground">
        Showing {start.toLocaleString()}–{end.toLocaleString()} of {totalCount.toLocaleString()}
      </p>
      <Pagination className="mx-0 w-auto justify-end">
        <PaginationContent>
          <PaginationItem>
            <PaginationPrevious
              href="#"
              aria-disabled={currentPage === 1}
              className={currentPage === 1 ? "pointer-events-none opacity-50" : undefined}
              onClick={handle(currentPage - 1)}
            />
          </PaginationItem>
          {pages.map((entry, index) =>
            entry === "ellipsis" ? (
              <PaginationItem key={`ellipsis-${index}`}>
                <PaginationEllipsis />
              </PaginationItem>
            ) : (
              <PaginationItem key={entry}>
                <PaginationLink href="#" isActive={entry === currentPage} onClick={handle(entry)}>
                  {entry}
                </PaginationLink>
              </PaginationItem>
            ),
          )}
          <PaginationItem>
            <PaginationNext
              href="#"
              aria-disabled={currentPage === pageCount}
              className={currentPage === pageCount ? "pointer-events-none opacity-50" : undefined}
              onClick={handle(currentPage + 1)}
            />
          </PaginationItem>
        </PaginationContent>
      </Pagination>
    </div>
  );
}

function buildPageList(current: number, total: number): Array<number | "ellipsis"> {
  if (total <= 7) {
    return Array.from({ length: total }, (_, i) => i + 1);
  }
  const pages: Array<number | "ellipsis"> = [1];
  const left = Math.max(2, current - 1);
  const right = Math.min(total - 1, current + 1);
  if (left > 2) pages.push("ellipsis");
  for (let i = left; i <= right; i++) pages.push(i);
  if (right < total - 1) pages.push("ellipsis");
  pages.push(total);
  return pages;
}

function ExecutionListLoading() {
  return <EmptyState title="Loading executions" body="Synchronizing the latest execution state." />;
}

function ExecutionListError({ status }: { status: string }) {
  return (
    <ErrorCallout
      title="Could not load executions"
      error={`Execution sync failed (${status}).`}
      action={
        <Button variant="outline" render={<Link to="/executions" />}>
          Retry
        </Button>
      }
    />
  );
}

function ExecutionListEmpty() {
  return (
    <EmptyState
      title="No executions yet"
      body="Launch a manual execution to get started."
      action={
        <Button variant="default" render={<Link to="/executions/new" />}>
          New execution
        </Button>
      }
    />
  );
}

function ExecutionLogsLoading({ isRunning }: { isRunning: boolean }) {
  return (
    <Callout title="Loading output">
      {isRunning ? "Waiting for stdout or stderr…" : "No stdout or stderr yet."}
    </Callout>
  );
}

function ExecutionLogsError({ status, isRunning }: { status: string; isRunning: boolean }) {
  return (
    <ErrorCallout
      title="Output unavailable"
      error={`Output stream unavailable (${status}).`}
      action={
        <div className="text-xs text-muted-foreground">
          {isRunning
            ? "The command is still running, but output has not synced yet."
            : "No stdout or stderr is available."}
        </div>
      }
    />
  );
}

function ExecutionLogsEmpty({ isRunning }: { isRunning: boolean }) {
  return (
    <Callout title="Output">
      {isRunning ? "Waiting for stdout or stderr…" : "No stdout or stderr."}
    </Callout>
  );
}

function ExecutionLogsBody({ logText }: { logText: string }) {
  const { scrollRef, contentRef, isAtBottom, scrollToBottom } = useStickToBottom();

  return (
    <div className="relative">
      <pre
        ref={scrollRef}
        className="max-h-[600px] overflow-x-auto overflow-y-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-4 font-mono text-xs"
      >
        <code ref={contentRef}>{logText}</code>
      </pre>
      {!isAtBottom && logText ? (
        <Button
          type="button"
          onClick={() => scrollToBottom()}
          className="absolute bottom-3 right-3 shadow-md"
          size="sm"
        >
          Scroll to bottom
        </Button>
      ) : null}
    </div>
  );
}

function ExecutionTimingSummary({
  execution,
  isRunning,
}: {
  execution: {
    created_at: string;
    latest_attempt: {
      completed_at?: string | null | undefined;
      duration_ms?: number | null | undefined;
      exit_code?: number | null | undefined;
      started_at?: string | null | undefined;
      zfs_written?: number | null | undefined;
    };
  };
  isRunning: boolean;
}) {
  const attempt = execution.latest_attempt;
  const exitCode = formatExitCode(attempt);

  return (
    <dl className="grid gap-x-8 gap-y-4 sm:grid-cols-2 lg:grid-cols-4">
      <ExecutionMetric
        label="Runtime"
        value={<RuntimeValue attempt={attempt} isRunning={isRunning} />}
      />
      <ExecutionMetric
        label="Started after"
        value={
          <StartupValue
            createdAt={execution.created_at}
            startedAt={attempt.started_at}
            isRunning={isRunning}
          />
        }
      />
      <ExecutionMetric label="Exit code" value={exitCode} />
      <ExecutionMetric
        label="ZFS written"
        value={attempt.zfs_written ? formatBytes(attempt.zfs_written) : "--"}
      />
    </dl>
  );
}

function ExecutionMetric({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
      <dd className="mt-1 truncate font-mono text-sm font-semibold tabular-nums">{value}</dd>
    </div>
  );
}

function RuntimeValue({
  attempt,
  isRunning,
}: {
  attempt: {
    duration_ms?: number | null | undefined;
    started_at?: string | null | undefined;
  };
  isRunning: boolean;
}) {
  if (typeof attempt.duration_ms === "number" && attempt.duration_ms > 0) {
    return formatDurationMs(attempt.duration_ms);
  }
  if (isRunning && attempt.started_at) {
    return <LiveElapsed startAt={attempt.started_at} />;
  }
  return "--";
}

function StartupValue({
  createdAt,
  startedAt,
}: {
  createdAt: string;
  startedAt?: string | null | undefined;
  isRunning: boolean;
}) {
  if (startedAt) {
    return formatDurationBetween(createdAt, startedAt);
  }
  return "Starting VM...";
}

function formatExitCode(attempt: { exit_code?: number | null | undefined }): string {
  if (typeof attempt.exit_code === "number") {
    return String(attempt.exit_code);
  }
  return "--";
}

function LiveElapsed({ startAt }: { startAt: string }) {
  const nowMs = useAnimationFrameNowMs();
  const startMs = Date.parse(startAt);

  if (!Number.isFinite(startMs)) {
    return "--";
  }

  return formatDurationMs(nowMs - startMs);
}

const animationFrameSubscribers = new Set<() => void>();
let animationFrameID: number | null = null;

function subscribeAnimationFrame(callback: () => void): () => void {
  if (typeof window === "undefined") {
    return () => {};
  }

  animationFrameSubscribers.add(callback);
  if (animationFrameID === null) {
    animationFrameID = window.requestAnimationFrame(tickAnimationFrameSubscribers);
  }

  return () => {
    animationFrameSubscribers.delete(callback);
    if (animationFrameSubscribers.size === 0 && animationFrameID !== null) {
      window.cancelAnimationFrame(animationFrameID);
      animationFrameID = null;
    }
  };
}

function tickAnimationFrameSubscribers() {
  for (const callback of animationFrameSubscribers) {
    callback();
  }

  if (animationFrameSubscribers.size > 0) {
    animationFrameID = window.requestAnimationFrame(tickAnimationFrameSubscribers);
  } else {
    animationFrameID = null;
  }
}

function useAnimationFrameNowMs(): number {
  return useSyncExternalStore(subscribeAnimationFrame, Date.now, () => 0);
}

function formatDurationBetween(startAt: string, endAt: string): string {
  const startMs = Date.parse(startAt);
  const endMs = Date.parse(endAt);

  if (!Number.isFinite(startMs) || !Number.isFinite(endMs)) {
    return "--";
  }

  return formatDurationMs(endMs - startMs);
}

function formatDurationMs(durationMs: number): string {
  const ms = Math.max(0, Math.floor(durationMs));

  if (ms < 1_000) {
    return `${ms}ms`;
  }

  const totalSeconds = Math.floor(ms / 1_000);
  const milliseconds = ms % 1_000;

  if (totalSeconds < 60) {
    return `${totalSeconds}s ${milliseconds.toString().padStart(3, "0")}ms`;
  }

  const totalMinutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;

  if (totalMinutes < 60) {
    return `${totalMinutes}m ${seconds}s`;
  }

  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;

  return `${hours}h ${minutes}m`;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
