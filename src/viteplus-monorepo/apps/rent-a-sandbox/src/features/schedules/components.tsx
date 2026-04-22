import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Field, FieldError, FieldLabel } from "@forge-metal/ui/components/ui/field";
import { Input } from "@forge-metal/ui/components/ui/input";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import { Textarea } from "@forge-metal/ui/components/ui/textarea";
import { EmptyState } from "~/components/empty-state";
import { ErrorCallout } from "~/components/error-callout";
import { formatDateTimeUTC } from "~/lib/format";
import { executionScheduleQuery, executionSchedulesQuery } from "./queries";
import {
  useCreateExecutionScheduleMutation,
  usePauseExecutionScheduleMutation,
  useResumeExecutionScheduleMutation,
} from "./mutations";
import {
  DEFAULT_INTERVAL_SECONDS,
  validateIntervalSeconds,
  validateScheduleCommand,
} from "./validation";

export function ExecutionSchedulesPanel() {
  const auth = useSignedInAuth();
  const schedules = useSuspenseQuery(executionSchedulesQuery(auth)).data;

  if (schedules.length === 0) {
    return (
      <EmptyState
        title="No schedules yet"
        body="Recurring schedules trigger the same direct VM execution flow on an interval."
        action={<Button render={<Link to="/schedules/new" />}>New schedule</Button>}
      />
    );
  }

  return (
    <div className="overflow-hidden rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Every</TableHead>
            <TableHead>State</TableHead>
            <TableHead>Recent runs</TableHead>
            <TableHead>Updated</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {schedules.map((schedule) => (
            <TableRow key={schedule.schedule_id}>
              <TableCell>
                <Link
                  to="/schedules/$scheduleId"
                  params={{ scheduleId: schedule.schedule_id }}
                  className="font-medium hover:underline"
                >
                  {schedule.display_name || schedule.schedule_id.slice(0, 8)}
                </Link>
              </TableCell>
              <TableCell>{schedule.interval_seconds}s</TableCell>
              <TableCell className="capitalize">{schedule.state}</TableCell>
              <TableCell>{schedule.dispatches.length}</TableCell>
              <TableCell>{formatDateTimeUTC(schedule.updated_at)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

export function ExecutionScheduleDetailPanel({ scheduleId }: { scheduleId: string }) {
  const auth = useSignedInAuth();
  const schedule = useSuspenseQuery(executionScheduleQuery(auth, scheduleId)).data;
  const pauseMutation = usePauseExecutionScheduleMutation(scheduleId);
  const resumeMutation = useResumeExecutionScheduleMutation(scheduleId);
  const isPaused = schedule.state === "paused";

  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/schedules" className="hover:text-foreground">
              ← Schedules
            </Link>
          </PageEyebrow>
          <div className="flex flex-wrap items-center gap-3">
            <PageTitle>{schedule.display_name || schedule.schedule_id.slice(0, 8)}</PageTitle>
            <span className="rounded-full border px-3 py-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {schedule.state}
            </span>
          </div>
        </PageHeaderContent>
        <div className="flex items-center gap-2">
          {isPaused ? (
            <Button
              type="button"
              onClick={() => resumeMutation.mutate()}
              disabled={resumeMutation.isPending}
            >
              {resumeMutation.isPending ? "Resuming…" : "Resume"}
            </Button>
          ) : (
            <Button
              type="button"
              variant="outline"
              onClick={() => pauseMutation.mutate()}
              disabled={pauseMutation.isPending}
            >
              {pauseMutation.isPending ? "Pausing…" : "Pause"}
            </Button>
          )}
        </div>
      </PageHeader>

      <PageSections>
        {pauseMutation.error || resumeMutation.error ? (
          <PageSection>
            <ErrorCallout
              title={isPaused ? "Resume failed" : "Pause failed"}
              error={pauseMutation.error ?? resumeMutation.error}
            />
          </PageSection>
        ) : null}

        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Configuration</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <dl className="grid gap-4 rounded-md border p-4 sm:grid-cols-2">
            <div>
              <dt className="text-sm text-muted-foreground">Every</dt>
              <dd className="font-medium">{schedule.interval_seconds} seconds</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Task queue</dt>
              <dd className="font-mono text-sm">{schedule.task_queue}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Temporal schedule</dt>
              <dd className="font-mono text-sm">{schedule.temporal_schedule_id}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Updated</dt>
              <dd>{formatDateTimeUTC(schedule.updated_at)}</dd>
            </div>
            <div className="sm:col-span-2">
              <dt className="text-sm text-muted-foreground">Run command</dt>
              <dd className="mt-1 rounded-md bg-muted p-3 font-mono text-sm whitespace-pre-wrap">
                {schedule.run_command}
              </dd>
            </div>
          </dl>
        </PageSection>

        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Recent runs</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          {schedule.dispatches.length === 0 ? (
            <EmptyState
              title="No runs yet"
              body="The worker will create an execution row the first time this schedule fires."
            />
          ) : (
            <div className="overflow-hidden rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Dispatch</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead>Scheduled</TableHead>
                    <TableHead>Execution</TableHead>
                    <TableHead>Failure</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {schedule.dispatches.map((dispatch) => (
                    <TableRow key={dispatch.dispatch_id}>
                      <TableCell className="font-mono text-sm">
                        {dispatch.dispatch_id.slice(0, 8)}
                      </TableCell>
                      <TableCell className="capitalize">{dispatch.state}</TableCell>
                      <TableCell>{formatDateTimeUTC(dispatch.scheduled_at)}</TableCell>
                      <TableCell>
                        {dispatch.execution_id ? (
                          <Link
                            to="/executions/$executionId"
                            params={{ executionId: dispatch.execution_id }}
                            className="font-mono text-sm hover:underline"
                          >
                            {dispatch.execution_id.slice(0, 8)}
                          </Link>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                      <TableCell>{dispatch.failure_reason ?? "—"}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </PageSection>
      </PageSections>
    </Page>
  );
}

export function ExecutionScheduleForm() {
  const navigate = useNavigate();
  const mutation = useCreateExecutionScheduleMutation({
    onSuccess: async (schedule) => {
      await navigate({
        to: "/schedules/$scheduleId",
        params: { scheduleId: schedule.schedule_id },
      });
    },
  });

  const form = useForm({
    defaultValues: {
      displayName: "",
      intervalSeconds: String(DEFAULT_INTERVAL_SECONDS),
      runCommand: "printf 'scheduled execution\\n'",
    },
    onSubmit: async ({ value }) => {
      mutation.reset();
      await mutation.mutateAsync({
        display_name: value.displayName || undefined,
        interval_seconds: Number(value.intervalSeconds),
        run_command: value.runCommand,
      });
    },
  });

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void form.handleSubmit();
      }}
      className="space-y-4"
    >
      <form.Field name="displayName">
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Display name</FieldLabel>
            <Input
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
              placeholder="Nightly smoke"
            />
          </Field>
        )}
      </form.Field>

      <form.Field
        name="intervalSeconds"
        validators={{
          onChange: ({ value }) => validateIntervalSeconds(value),
        }}
      >
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Interval seconds</FieldLabel>
            <Input
              id={field.name}
              inputMode="numeric"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
              <FieldError>{field.state.meta.errors[0]}</FieldError>
            ) : null}
          </Field>
        )}
      </form.Field>

      <form.Field
        name="runCommand"
        validators={{
          onChange: ({ value }) => validateScheduleCommand(value),
        }}
      >
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Run command</FieldLabel>
            <Textarea
              id={field.name}
              rows={5}
              className="font-mono"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
              <FieldError>{field.state.meta.errors[0]}</FieldError>
            ) : null}
          </Field>
        )}
      </form.Field>

      {mutation.error ? (
        <ErrorCallout title="Schedule creation failed" error={mutation.error} />
      ) : null}

      <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
        {([canSubmit, isSubmitting]) => (
          <Button type="submit" disabled={!canSubmit || isSubmitting || mutation.isPending}>
            {mutation.isPending || isSubmitting ? "Creating…" : "Create schedule"}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}
