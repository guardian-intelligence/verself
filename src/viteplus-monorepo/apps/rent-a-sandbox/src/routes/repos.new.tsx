import { useForm } from "@tanstack/react-form";
import { createFileRoute, Link } from "@tanstack/react-router";
import { ErrorCallout } from "~/components/error-callout";
import { useImportRepoMutation } from "~/features/repos/mutations";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/repos/new")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  component: NewRepoPage,
});

function NewRepoPage() {
  const importMutation = useImportRepoMutation();

  const form = useForm({
    defaultValues: {
      cloneUrl: "",
      defaultBranch: "main",
    },
    onSubmit: async ({ value }) => {
      await importMutation.mutateAsync({
        data: {
          clone_url: value.cloneUrl,
          default_branch: value.defaultBranch,
        },
      });
    },
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/repos" className="text-sm text-muted-foreground hover:text-foreground">
          &larr; Back
        </Link>
        <h1 className="text-2xl font-bold">Import Repo</h1>
      </div>

      <div className="max-w-2xl text-sm text-muted-foreground">
        Import a repository that already uses <code className="mx-1">runs-on: forge-metal</code> in
        its workflow YAML. The service will scan the default branch, record any unsupported labels,
        and queue the first golden bootstrap when the repo is compatible.
      </div>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
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
                onChange={(event) => field.handleChange(event.target.value)}
                placeholder="https://git.example.com/acme/repo.git"
                className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              />
              {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
                <p className="mt-1 text-sm text-destructive">{field.state.meta.errors[0]}</p>
              ) : null}
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
                onChange={(event) => field.handleChange(event.target.value)}
                placeholder="main"
                className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
              />
            </div>
          )}
        </form.Field>

        {importMutation.error ? <ErrorCallout error={importMutation.error} title="Import failed" /> : null}

        <div className="rounded-md border border-border bg-muted/20 p-4 text-sm text-muted-foreground">
          v1 supports one runner label and one runner profile:
          <code className="mx-1">forge-metal</code>. If the default branch still uses
          <code className="mx-1">ubuntu-latest</code>
          or any other label, the repo will land in action required until the workflows are fixed.
        </div>

        <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <button
              type="submit"
              disabled={!canSubmit || isSubmitting || importMutation.isPending}
              className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90 disabled:opacity-50"
            >
              {isSubmitting || importMutation.isPending ? "Importing..." : "Import Repo"}
            </button>
          )}
        </form.Subscribe>
      </form>
    </div>
  );
}
