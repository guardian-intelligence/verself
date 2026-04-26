import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useSignedInAuth } from "@verself/auth-web/react";
import { Button } from "@verself/ui/components/ui/button";
import { Field, FieldError, FieldLabel } from "@verself/ui/components/ui/field";
import { Input } from "@verself/ui/components/ui/input";
import { Select } from "@verself/ui/components/ui/select";
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
} from "@verself/ui/components/ui/page";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@verself/ui/components/ui/table";
import { Textarea } from "@verself/ui/components/ui/textarea";
import { EmptyState } from "~/components/empty-state";
import { ErrorCallout } from "~/components/error-callout";
import { formatDateTimeUTC } from "~/lib/format";
import { sourceRepositoriesQuery } from "~/features/source/queries";
import { executionScheduleQuery, executionSchedulesQuery } from "./queries";
import {
  useCreateExecutionScheduleMutation,
  usePauseExecutionScheduleMutation,
  useResumeExecutionScheduleMutation,
} from "./mutations";
import {
  DEFAULT_INTERVAL_SECONDS,
  parseScheduleInputs,
  validateIntervalSeconds,
  validateRef,
  validateScheduleInputs,
  validateSourceRepositoryID,
  validateWorkflowPath,
} from "./validation";

export function ExecutionSchedulesPanel() {
  const auth = useSignedInAuth();
  const schedules = useSuspenseQuery(executionSchedulesQuery(auth)).data;

  if (schedules.length === 0) {
    return (
      <EmptyState
        title="No schedules yet"
        body="Recurring schedules dispatch source-hosted workflows on an interval."
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
            <TableHead>Workflow</TableHead>
            <TableHead>Every</TableHead>
            <TableHead>State</TableHead>
            <TableHead>Recent dispatches</TableHead>
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
              <TableCell>
                <div className="min-w-0">
                  <div className="font-mono text-sm break-all">{schedule.workflow_path}</div>
                  <div className="font-mono text-xs text-muted-foreground">
                    {schedule.project_id.slice(0, 8)} / {schedule.source_repository_id.slice(0, 8)}
                    {schedule.ref ? ` @ ${schedule.ref}` : ""}
                  </div>
                </div>
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
              <dt className="text-sm text-muted-foreground">Project</dt>
              <dd className="font-mono text-sm break-all">{schedule.project_id}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Repository</dt>
              <dd className="font-mono text-sm break-all">{schedule.source_repository_id}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Workflow</dt>
              <dd className="font-mono text-sm break-all">{schedule.workflow_path}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Ref</dt>
              <dd className="font-mono text-sm">{schedule.ref || "default"}</dd>
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
              <dt className="text-sm text-muted-foreground">Inputs</dt>
              <dd className="mt-1 rounded-md bg-muted p-3 font-mono text-sm whitespace-pre-wrap">
                {JSON.stringify(schedule.inputs, null, 2)}
              </dd>
            </div>
          </dl>
        </PageSection>

        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Recent dispatches</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          {schedule.dispatches.length === 0 ? (
            <EmptyState
              title="No dispatches yet"
              body="The worker will dispatch the workflow the first time this schedule fires."
            />
          ) : (
            <div className="overflow-hidden rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Dispatch</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead>Scheduled</TableHead>
                    <TableHead>Workflow run</TableHead>
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
                      <TableCell className="font-mono text-sm">
                        {dispatch.source_workflow_run_id
                          ? `${dispatch.source_workflow_run_id.slice(0, 8)} (${dispatch.workflow_state ?? "unknown"})`
                          : "—"}
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
  const auth = useSignedInAuth();
  const repositories = useSuspenseQuery(sourceRepositoriesQuery(auth)).data.repositories;
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
      inputsJSON: "{}",
      ref: "main",
      sourceRepositoryId: repositories[0]?.repo_id ?? "",
      workflowPath: ".forgejo/workflows/smoke.yml",
    },
    onSubmit: async ({ value }) => {
      mutation.reset();
      const repository = repositories.find((repo) => repo.repo_id === value.sourceRepositoryId);
      if (!repository) {
        throw new Error("Selected repository is no longer available.");
      }
      await mutation.mutateAsync({
        display_name: value.displayName || undefined,
        inputs: parseScheduleInputs(value.inputsJSON),
        interval_seconds: Number(value.intervalSeconds),
        project_id: repository.project_id,
        ref: value.ref || undefined,
        source_repository_id: value.sourceRepositoryId,
        workflow_path: value.workflowPath,
      });
    },
  });

  if (repositories.length === 0) {
    return (
      <EmptyState
        title="No repositories"
        body="Create a source repository before scheduling workflow dispatches."
        action={<Button render={<Link to="/builds" />}>Open builds</Button>}
      />
    );
  }

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
        name="sourceRepositoryId"
        validators={{
          onChange: ({ value }) => validateSourceRepositoryID(value),
        }}
      >
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Repository</FieldLabel>
            <Select
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
            >
              {repositories.map((repo) => (
                <option key={repo.repo_id} value={repo.repo_id}>
                  {repo.name} ({repo.project_id.slice(0, 8)})
                </option>
              ))}
            </Select>
            {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
              <FieldError>{field.state.meta.errors[0]}</FieldError>
            ) : null}
          </Field>
        )}
      </form.Field>

      <form.Field
        name="workflowPath"
        validators={{
          onChange: ({ value }) => validateWorkflowPath(value),
        }}
      >
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Workflow path</FieldLabel>
            <Input
              id={field.name}
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
        name="ref"
        validators={{
          onChange: ({ value }) => validateRef(value),
        }}
      >
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Ref</FieldLabel>
            <Input
              id={field.name}
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
        name="inputsJSON"
        validators={{
          onChange: ({ value }) => validateScheduleInputs(value),
        }}
      >
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Inputs</FieldLabel>
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
