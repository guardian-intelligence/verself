import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useForm } from "@tanstack/react-form";
import { importRepo } from "~/lib/api";
import { keys } from "~/lib/query-keys";
import { useState } from "react";

export const Route = createFileRoute("/repos/new")({
  component: NewRepoPage,
});

function NewRepoPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [submitError, setSubmitError] = useState<string | null>(null);

  const form = useForm({
    defaultValues: {
      cloneUrl: "",
      defaultBranch: "main",
    },
    onSubmit: async ({ value }) => {
      setSubmitError(null);
      try {
        const repo = await importRepo({
          clone_url: value.cloneUrl,
          default_branch: value.defaultBranch,
        });
        void queryClient.invalidateQueries({ queryKey: keys.repos() });
        void navigate({ to: "/repos/$repoId", params: { repoId: repo.repo_id } });
      } catch (err) {
        setSubmitError(err instanceof Error ? err.message : "Failed to import repo");
      }
    },
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/repos" className="text-muted-foreground hover:text-foreground text-sm">
          &larr; Back
        </Link>
        <h1 className="text-2xl font-bold">Import Repo</h1>
      </div>

      <div className="max-w-2xl text-sm text-muted-foreground">
        Import a repository that already uses
        <code className="mx-1">runs-on: forge-metal</code>
        in its workflow YAML. The service will scan the default branch, record any unsupported
        labels, and queue the first golden bootstrap when the repo is compatible.
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          void form.handleSubmit();
        }}
        className="max-w-lg space-y-4"
      >
        <form.Field
          name="cloneUrl"
          validators={{
            onChange: ({ value }) => {
              if (!value) return "Clone URL is required";
              return undefined;
            },
          }}
        >
          {(field) => (
            <div>
              <label htmlFor={field.name} className="text-sm font-medium">
                Clone URL
              </label>
              <input
                id={field.name}
                type="text"
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="https://git.example.com/acme/repo.git"
                className="mt-1 w-full px-3 py-2 rounded-md border border-input bg-background text-sm"
              />
              {field.state.meta.isTouched && field.state.meta.errors.length > 0 && (
                <p className="mt-1 text-sm text-destructive">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Field name="defaultBranch">
          {(field) => (
            <div>
              <label htmlFor={field.name} className="text-sm font-medium">
                Default branch
              </label>
              <input
                id={field.name}
                type="text"
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="main"
                className="mt-1 w-full px-3 py-2 rounded-md border border-input bg-background text-sm font-mono"
              />
            </div>
          )}
        </form.Field>

        {submitError && <p className="text-sm text-destructive">{submitError}</p>}

        <div className="rounded-md border border-border bg-muted/20 p-4 text-sm text-muted-foreground">
          v1 supports one runner label and one runner profile:
          <code className="mx-1">forge-metal</code>. If the default branch still uses
          <code className="mx-1">ubuntu-latest</code>
          or any other label, the repo will land in action required until the workflows are fixed.
        </div>

        <form.Subscribe selector={(s) => [s.canSubmit, s.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <button
              type="submit"
              disabled={!canSubmit || isSubmitting}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm disabled:opacity-50"
            >
              {isSubmitting ? "Importing..." : "Import Repo"}
            </button>
          )}
        </form.Subscribe>
      </form>
    </div>
  );
}
