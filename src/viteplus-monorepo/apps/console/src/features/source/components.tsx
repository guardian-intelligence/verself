import { useState } from "react";
import { useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { useSignedInAuth } from "@verself/auth-web/react";
import { Badge } from "@verself/ui/components/ui/badge";
import { Button } from "@verself/ui/components/ui/button";
import { Field, FieldLabel } from "@verself/ui/components/ui/field";
import { Input } from "@verself/ui/components/ui/input";
import { Select } from "@verself/ui/components/ui/select";
import { toast } from "@verself/ui/components/ui/sonner";
import { Copy, GitBranch, GitCommit, GitPullRequest, KeyRound, Terminal } from "lucide-react";
import { EmptyState } from "~/components/empty-state";
import { projectsQuery } from "~/features/projects/queries";
import { formatDateTimeUTC } from "~/lib/format";
import { createProject, createSourceGitCredential, createSourceRepository } from "~/server-fns/api";
import type { SourceGitCredential, SourceRepository } from "~/server-fns/api";
import { sourceRefsQuery, sourceRepositoriesQuery } from "./queries";

export function SourceRepositoriesPanel({ gitOrigin }: { gitOrigin: string }) {
  const auth = useSignedInAuth();
  const { repositories } = useSuspenseQuery(sourceRepositoriesQuery(auth)).data;

  if (repositories.length === 0) {
    return <SourceRepositoryEmptyState gitOrigin={gitOrigin} />;
  }

  return (
    <div className="grid gap-4">
      {repositories.map((repo) => (
        <SourceRepositoryCard key={repo.repo_id} gitOrigin={gitOrigin} repo={repo} />
      ))}
    </div>
  );
}

function SourceRepositoryCard({ gitOrigin, repo }: { gitOrigin: string; repo: SourceRepository }) {
  const auth = useSignedInAuth();
  const refs = useSuspenseQuery(sourceRefsQuery(auth, repo.repo_id)).data.refs;
  const activeBranches = refs.filter((ref) => ref.name !== repo.default_branch);
  const pushURL = `${gitOrigin.replace(/\/$/, "")}/${repo.org_path}/${repo.slug}.git`;

  return (
    <article className="rounded-md border bg-card">
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3 border-b px-4 py-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-lg font-semibold leading-6 tracking-tight">{repo.name}</h2>
            <Badge variant="outline">{repo.backend}</Badge>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs font-medium text-muted-foreground">
            <span>{repo.slug}</span>
            <span className="font-mono">{repo.default_branch}</span>
            <span>{formatDateTimeUTC(repo.updated_at)}</span>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge variant={activeBranches.length > 0 ? "info" : "outline"}>
            {activeBranches.length} active branches
          </Badge>
        </div>
      </div>

      <div className="px-4 py-4">
        <div className="mb-4 grid gap-2 rounded-md border bg-background p-3">
          <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
            <Terminal className="size-3.5" aria-hidden="true" />
            Git remote
          </div>
          <code className="break-all font-mono text-xs text-foreground">
            git remote add verself {pushURL}
          </code>
          <code className="break-all font-mono text-xs text-foreground">
            git push verself {repo.default_branch}
          </code>
        </div>
        <h3 className="mb-3 flex items-center gap-2 text-sm font-semibold">
          <GitPullRequest className="size-4 text-muted-foreground" aria-hidden="true" />
          Branches
        </h3>
        {activeBranches.length === 0 ? (
          <p className="text-sm text-muted-foreground">No active PR branches.</p>
        ) : (
          <ul className="grid gap-3">
            {activeBranches.map((branch) => (
              <li key={branch.name} className="flex min-w-0 items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate font-mono text-sm">{branch.name}</div>
                  <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
                    <GitCommit className="size-3" aria-hidden="true" />
                    <span className="font-mono">{shortCommit(branch.commit)}</span>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </article>
  );
}

function SourceRepositoryEmptyState({ gitOrigin }: { gitOrigin: string }) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();
  const projects = useSuspenseQuery(projectsQuery(auth)).data.projects;
  const [projectID, setProjectID] = useState(projects[0]?.project_id ?? "");
  const [projectName, setProjectName] = useState("main");
  const [credential, setCredential] = useState<SourceGitCredential | null>(null);
  const createRepo = useMutation({
    mutationFn: async () => {
      const name = projectName.trim();
      if (!name) {
        throw new Error("Repository name is required.");
      }
      const resolvedProjectID =
        projectID ||
        (
          await createProject({
            data: { display_name: name },
          })
        ).project_id;
      return createSourceRepository({
        data: {
          default_branch: "main",
          name,
          project_id: resolvedProjectID,
        },
      });
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: projectsQuery(auth).queryKey }),
        queryClient.invalidateQueries({ queryKey: sourceRepositoriesQuery(auth).queryKey }),
      ]);
      toast.success("Repository created");
    },
    onError: (error) => {
      toast.error("Failed to create repository", {
        description: error instanceof Error ? error.message : String(error),
      });
    },
  });
  const createCredential = useMutation({
    mutationFn: () => createSourceGitCredential({ data: { label: "console git push" } }),
    onSuccess: (nextCredential) => {
      setCredential(nextCredential);
      toast.success("Git credential created");
    },
    onError: (error) => {
      toast.error("Failed to create Git credential", {
        description: error instanceof Error ? error.message : String(error),
      });
    },
  });
  const orgPath = credential?.org_path ?? "<org>";
  const repoSlug =
    projectName
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9-]+/g, "-")
      .replace(/^-+|-+$/g, "") || "<repo>";
  const pushURL = `${gitOrigin.replace(/\/$/, "")}/${orgPath}/${repoSlug}.git`;

  return (
    <EmptyState
      icon={<GitBranch className="size-5" aria-hidden="true" />}
      title="Create a repository"
      body={
        <div className="grid gap-4 text-left">
          <div className="grid gap-3">
            <Field>
              <FieldLabel htmlFor="source-project">Project</FieldLabel>
              <Select
                id="source-project"
                value={projectID}
                onChange={(event) => setProjectID(event.target.value)}
              >
                {projects.length === 0 ? (
                  <option value="">Create project from repo name</option>
                ) : null}
                {projects.map((project) => (
                  <option key={project.project_id} value={project.project_id}>
                    {project.display_name}
                  </option>
                ))}
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="source-repo-name">Repository name</FieldLabel>
              <Input
                id="source-repo-name"
                value={projectName}
                onChange={(event) => setProjectName(event.target.value)}
              />
            </Field>
            <Button
              type="button"
              size="sm"
              className="w-fit"
              onClick={() => createRepo.mutate()}
              disabled={createRepo.isPending}
            >
              <GitBranch className="size-3.5" aria-hidden="true" />
              {createRepo.isPending ? "Creating..." : "Create repository"}
            </Button>
          </div>
          <div className="grid gap-2 border-t pt-3">
            <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
              <Terminal className="size-3.5" aria-hidden="true" />
              Git remote
            </div>
            <code className="break-all font-mono text-xs text-foreground">
              git remote add verself {pushURL}
            </code>
            <code className="break-all font-mono text-xs text-foreground">
              git push verself main
            </code>
          </div>
          <div className="grid gap-2 border-t pt-3">
            <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
              <KeyRound className="size-3.5" aria-hidden="true" />
              HTTPS credential
            </div>
            {credential ? (
              <div className="grid gap-2">
                <CredentialLine label="Username" value={credential.username} />
                <CredentialLine label="Token" value={credential.token} secret />
              </div>
            ) : (
              <Button
                type="button"
                size="sm"
                className="w-fit"
                onClick={() => createCredential.mutate()}
                disabled={createCredential.isPending}
              >
                <KeyRound className="size-3.5" aria-hidden="true" />
                {createCredential.isPending ? "Creating..." : "Create Git credential"}
              </Button>
            )}
          </div>
        </div>
      }
    />
  );
}

function CredentialLine({
  label,
  value,
  secret = false,
}: {
  label: string;
  value: string;
  secret?: boolean;
}) {
  const displayValue = secret ? `${value.slice(0, 12)}...` : value;
  return (
    <div className="grid gap-1">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <div className="flex min-w-0 items-center gap-2">
        <code className="min-w-0 flex-1 break-all rounded-sm bg-muted px-2 py-1 font-mono text-xs text-foreground">
          {displayValue}
        </code>
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={`Copy ${label.toLowerCase()}`}
          onClick={() => copyValue(value, label)}
        >
          <Copy className="size-3.5" aria-hidden="true" />
        </Button>
      </div>
    </div>
  );
}

function shortCommit(commit: string) {
  return commit.length > 12 ? commit.slice(0, 12) : commit;
}

function copyValue(value: string, label: string) {
  navigator.clipboard?.writeText(value).then(
    () => toast(`${label} copied`),
    () => toast.error(`Unable to copy ${label.toLowerCase()}`),
  );
}
