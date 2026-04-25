import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Link, useHydrated, useNavigate } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Field, FieldError, FieldLabel } from "@forge-metal/ui/components/ui/field";
import { Input } from "@forge-metal/ui/components/ui/input";
import {
  Page,
  PageDescription,
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
import { useCreateSourceRepositoryMutation } from "./mutations";
import {
  sourceRefsQuery,
  sourceRepositoriesQuery,
  sourceRepositoryQuery,
  sourceTreeQuery,
} from "./queries";

export function SourceRepositoriesPanel() {
  const auth = useSignedInAuth();
  const { repositories } = useSuspenseQuery(sourceRepositoriesQuery(auth)).data;

  if (repositories.length === 0) {
    return (
      <EmptyState
        title="No repositories"
        body="Create a private repository managed through Forge Metal."
      />
    );
  }

  return (
    <div className="overflow-hidden rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Default branch</TableHead>
            <TableHead>Visibility</TableHead>
            <TableHead>Updated</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {repositories.map((repo) => (
            <TableRow key={repo.repo_id}>
              <TableCell>
                <Link
                  to="/source/$repoId"
                  params={{ repoId: repo.repo_id }}
                  className="font-medium hover:underline"
                >
                  {repo.name}
                </Link>
                <div className="text-xs text-muted-foreground">{repo.slug}</div>
              </TableCell>
              <TableCell className="font-mono text-sm">{repo.default_branch}</TableCell>
              <TableCell className="capitalize">{repo.visibility}</TableCell>
              <TableCell>{formatDateTimeUTC(repo.updated_at)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

export function SourceRepositoryForm() {
  const hydrated = useHydrated();
  const navigate = useNavigate();
  const mutation = useCreateSourceRepositoryMutation({
    onSuccess: async (repo) => {
      await navigate({ to: "/source/$repoId", params: { repoId: repo.repo_id } });
    },
  });
  const form = useForm({
    defaultValues: {
      name: "",
      description: "",
      defaultBranch: "main",
    },
    onSubmit: async ({ value }) => {
      mutation.reset();
      await mutation.mutateAsync({
        name: value.name,
        description: value.description || undefined,
        default_branch: value.defaultBranch || undefined,
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
      className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]"
    >
      <form.Field
        name="name"
        validators={{
          onChange: ({ value }) => (value.trim() ? undefined : "Repository name is required."),
        }}
      >
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Name</FieldLabel>
            <Input
              id={field.name}
              disabled={!hydrated || mutation.isPending}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
              placeholder="product-api"
            />
            {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
              <FieldError>{field.state.meta.errors[0]}</FieldError>
            ) : null}
          </Field>
        )}
      </form.Field>

      <form.Field name="defaultBranch">
        {(field) => (
          <Field>
            <FieldLabel htmlFor={field.name}>Default branch</FieldLabel>
            <Input
              id={field.name}
              disabled={!hydrated || mutation.isPending}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
            />
          </Field>
        )}
      </form.Field>

      <div className="flex items-end">
        <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <Button
              type="submit"
              disabled={!hydrated || !canSubmit || isSubmitting || mutation.isPending}
            >
              {mutation.isPending || isSubmitting ? "Creating..." : "Create"}
            </Button>
          )}
        </form.Subscribe>
      </div>

      <form.Field name="description">
        {(field) => (
          <Field className="lg:col-span-3">
            <FieldLabel htmlFor={field.name}>Description</FieldLabel>
            <Textarea
              id={field.name}
              disabled={!hydrated || mutation.isPending}
              rows={3}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
            />
          </Field>
        )}
      </form.Field>

      {mutation.error ? (
        <div className="lg:col-span-3">
          <ErrorCallout title="Repository creation failed" error={mutation.error} />
        </div>
      ) : null}
    </form>
  );
}

export function SourceRepositoryDetail({ repoId }: { repoId: string }) {
  const auth = useSignedInAuth();
  const repo = useSuspenseQuery(sourceRepositoryQuery(auth, repoId)).data;
  const refs = useSuspenseQuery(sourceRefsQuery(auth, repoId)).data.refs;
  const [path, setPath] = useState("");
  const treeInput = {
    ref: repo.default_branch,
    ...(path ? { path } : {}),
  };
  const tree = useSuspenseQuery(sourceTreeQuery(auth, repoId, treeInput)).data;

  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/source" className="hover:text-foreground">
              Source
            </Link>
          </PageEyebrow>
          <PageTitle>{repo.name}</PageTitle>
          <PageDescription>{repo.description || repo.slug}</PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Repository</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <dl className="grid gap-4 rounded-md border p-4 sm:grid-cols-3">
            <div>
              <dt className="text-sm text-muted-foreground">Default branch</dt>
              <dd className="font-mono text-sm">{repo.default_branch}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Visibility</dt>
              <dd className="capitalize">{repo.visibility}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Updated</dt>
              <dd>{formatDateTimeUTC(repo.updated_at)}</dd>
            </div>
          </dl>
        </PageSection>

        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Refs</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          {refs.length === 0 ? (
            <EmptyState title="No refs" body="Forgejo has not reported any branch refs yet." />
          ) : (
            <div className="overflow-hidden rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Commit</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {refs.map((ref) => (
                    <TableRow key={ref.name}>
                      <TableCell className="font-mono text-sm">{ref.name}</TableCell>
                      <TableCell className="font-mono text-sm">{ref.commit.slice(0, 12)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </PageSection>

        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Tree</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <div className="mb-3 max-w-md">
            <Input
              value={path}
              onChange={(event) => setPath(event.target.value)}
              placeholder="src"
              aria-label="Tree path"
            />
          </div>
          {tree.entries.length === 0 ? (
            <EmptyState title="Empty path" body="No tree entries were returned for this path." />
          ) : (
            <div className="overflow-hidden rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Path</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Size</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tree.entries.map((entry) => (
                    <TableRow key={entry.path}>
                      <TableCell className="font-mono text-sm">{entry.path}</TableCell>
                      <TableCell>{entry.type}</TableCell>
                      <TableCell>{entry.size}</TableCell>
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
