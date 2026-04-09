import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useForm } from "@tanstack/react-form";
import { submitRepoExecution } from "~/server-fns/api";
import { keys } from "~/lib/query-keys";
import { useState } from "react";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/jobs/new")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  component: NewJobPage,
});

function NewJobPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [submitError, setSubmitError] = useState<string | null>(null);

  const form = useForm({
    defaultValues: {
      repoUrl: "",
      ref: "refs/heads/main",
    },
    onSubmit: async ({ value }) => {
      setSubmitError(null);
      try {
        const data = await submitRepoExecution({
          data: {
            repo_url: value.repoUrl,
            ref: value.ref,
          },
        });
        void queryClient.invalidateQueries({ queryKey: keys.jobs() });
        void queryClient.invalidateQueries({ queryKey: keys.balance() });
        void navigate({ to: "/jobs/$jobId", params: { jobId: data.execution_id } });
      } catch (err) {
        setSubmitError(err instanceof Error ? err.message : "Failed to create sandbox");
      }
    },
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link to="/jobs" className="text-muted-foreground hover:text-foreground text-sm">
          &larr; Back
        </Link>
        <h1 className="text-2xl font-bold">Manual Execution</h1>
      </div>

      <div className="max-w-xl text-sm text-muted-foreground">
        This is the low-level repo execution escape hatch. Imported repos should
        normally be prepared and run from the <Link to="/repos" className="text-primary hover:underline">Repos</Link>{" "}
        flow so they execute against an active golden image.
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          void form.handleSubmit();
        }}
        className="max-w-md space-y-4"
      >
        <form.Field
          name="repoUrl"
          validators={{
            onChange: ({ value }) => {
              if (!value) return "Repository URL is required";
              return undefined;
            },
          }}
        >
          {(field) => (
            <div>
              <label htmlFor={field.name} className="text-sm font-medium">
                Repository URL
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

        <form.Field
          name="ref"
          validators={{
            onChange: ({ value }) => {
              if (!value) return "Ref is required";
              if (!value.startsWith("refs/")) return "Ref must look like refs/heads/main";
              return undefined;
            },
          }}
        >
          {(field) => (
            <div>
              <label htmlFor={field.name} className="text-sm font-medium">
                Ref
              </label>
              <input
                id={field.name}
                type="text"
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="refs/heads/main"
                className="mt-1 w-full px-3 py-2 rounded-md border border-input bg-background text-sm font-mono"
              />
              {field.state.meta.isTouched && field.state.meta.errors.length > 0 && (
                <p className="mt-1 text-sm text-destructive">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        {submitError && <p className="text-sm text-destructive">{submitError}</p>}

        <form.Subscribe selector={(s) => [s.canSubmit, s.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <button
              type="submit"
              disabled={!canSubmit || isSubmitting}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm disabled:opacity-50"
            >
              {isSubmitting ? "Submitting..." : "Submit Execution"}
            </button>
          )}
        </form.Subscribe>
      </form>
    </div>
  );
}
