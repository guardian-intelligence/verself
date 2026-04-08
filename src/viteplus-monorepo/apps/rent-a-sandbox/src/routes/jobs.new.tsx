import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useForm } from "@tanstack/react-form";
import { submitJob } from "~/lib/api";
import { keys } from "~/lib/query-keys";
import { useState } from "react";

export const Route = createFileRoute("/jobs/new")({
  component: NewJobPage,
});

function NewJobPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [submitError, setSubmitError] = useState<string | null>(null);

  const form = useForm({
    defaultValues: {
      repoUrl: "",
      runCommand: "",
    },
    onSubmit: async ({ value }) => {
      setSubmitError(null);
      try {
        const data = await submitJob(value.repoUrl, value.runCommand || undefined);
        void queryClient.invalidateQueries({ queryKey: keys.jobs() });
        void queryClient.invalidateQueries({ queryKey: keys.balance() });
        void navigate({ to: "/jobs/$jobId", params: { jobId: data.job_id } });
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
        <h1 className="text-2xl font-bold">Create Sandbox</h1>
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
              if (!value.startsWith("https://")) return "URL must start with https://";
              try {
                new URL(value);
              } catch {
                return "Must be a valid URL";
              }
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
                type="url"
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="https://github.com/user/repo"
                className="mt-1 w-full px-3 py-2 rounded-md border border-input bg-background text-sm"
              />
              {field.state.meta.isTouched && field.state.meta.errors.length > 0 && (
                <p className="mt-1 text-sm text-destructive">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Field name="runCommand">
          {(field) => (
            <div>
              <label htmlFor={field.name} className="text-sm font-medium">
                Run command <span className="text-muted-foreground">(optional)</span>
              </label>
              <input
                id={field.name}
                type="text"
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="npm test"
                className="mt-1 w-full px-3 py-2 rounded-md border border-input bg-background text-sm"
              />
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
              {isSubmitting ? "Creating..." : "Create Sandbox"}
            </button>
          )}
        </form.Subscribe>
      </form>
    </div>
  );
}
