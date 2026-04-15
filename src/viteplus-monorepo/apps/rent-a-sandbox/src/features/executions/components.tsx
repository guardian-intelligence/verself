import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { ClientOnly, Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useStickToBottom } from "use-stick-to-bottom";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@forge-metal/ui/components/ui/card";
import { Field, FieldError, FieldLabel } from "@forge-metal/ui/components/ui/field";
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
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/executions" className="text-sm text-muted-foreground hover:text-foreground">
          Executions
        </Link>
        <span className="text-muted-foreground">/</span>
        <h1 className="font-mono text-xl font-semibold">{execution.execution_id.slice(0, 8)}</h1>
        <ExecutionStatusBadge status={execution.status} />
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <InfoCard
          label="Source"
          value={execution.source_ref || execution.source_kind || "--"}
        />
        <InfoCard label="Kind" value={execution.kind} />
        <InfoCard
          label="Duration"
          value={attempt.duration_ms ? `${(attempt.duration_ms / 1000).toFixed(1)}s` : "--"}
        />
        <InfoCard label="Exit code" value={String(attempt.exit_code ?? "--")} />
        <InfoCard
          label="ZFS written"
          value={attempt.zfs_written ? formatBytes(attempt.zfs_written) : "--"}
        />
        <InfoCard label="Attempt" value={attempt.attempt_id.slice(0, 8)} />
      </div>

      <ClientOnly fallback={<ExecutionLogsLoading isRunning={isRunning} />}>
        <ExecutionLogsPanel attemptId={attempt.attempt_id} isRunning={isRunning} />
      </ClientOnly>
    </div>
  );
}

function ExecutionDetailLoading({ executionId }: { executionId: string }) {
  const executionPrefix = executionId.slice(0, 8);
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Link to="/executions" className="text-sm text-muted-foreground hover:text-foreground">
          Executions
        </Link>
        <span className="text-muted-foreground">/</span>
        <h1 className="font-mono text-xl font-semibold">{executionPrefix}</h1>
      </div>
      <ExecutionLogsLoading isRunning />
    </div>
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
      className="max-w-xl space-y-4"
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
    <div>
      <h2 className="mb-2 text-sm font-semibold">Logs</h2>
      <Callout title="Loading logs">
        {isRunning ? "Waiting for output…" : "No log output yet."}
      </Callout>
    </div>
  );
}

function ExecutionLogsError({ status, isRunning }: { status: string; isRunning: boolean }) {
  return (
    <div>
      <h2 className="mb-2 text-sm font-semibold">Logs</h2>
      <ErrorCallout
        title="Log stream unavailable"
        error={`Log stream unavailable (${status}).`}
        action={
          <div className="text-xs text-muted-foreground">
            {isRunning
              ? "The attempt is still running, but logs have not synced yet."
              : "No log output is available."}
          </div>
        }
      />
    </div>
  );
}

function ExecutionLogsEmpty({ isRunning }: { isRunning: boolean }) {
  return (
    <div>
      <h2 className="mb-2 text-sm font-semibold">Logs</h2>
      <Callout title="Logs">{isRunning ? "Waiting for output…" : "No log output."}</Callout>
    </div>
  );
}

function ExecutionLogsBody({ logText }: { logText: string }) {
  const { scrollRef, contentRef, isAtBottom, scrollToBottom } = useStickToBottom();

  return (
    <div>
      <h2 className="mb-2 text-sm font-semibold">Logs</h2>
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
    </div>
  );
}

function InfoCard({ label, value }: { label: string; value: string }) {
  return (
    <Card className="gap-1 p-3">
      <CardHeader className="p-0">
        <CardTitle className="text-xs font-medium text-muted-foreground">{label}</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <div className="truncate text-sm font-medium">{value}</div>
      </CardContent>
    </Card>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
