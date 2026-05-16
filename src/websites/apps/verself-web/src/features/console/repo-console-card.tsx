import type { ReactNode } from "react";
import { useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { ClientOnly } from "@tanstack/react-router";
import { useSignedInAuth } from "@verself/auth-web/react";
import { ElapsedTime } from "@verself/ui/components/elapsed-time";
import { Button } from "@verself/ui/components/ui/button";
import { Skeleton } from "@verself/ui/components/ui/skeleton";
import { cn } from "@verself/ui/lib/utils";
import { FolderMinus, Trash2 } from "lucide-react";
import { formatDateTimeUTC } from "~/lib/format";
import {
  deleteCacheGeneration,
  deleteCachePath,
  type CacheGeneration,
  type CacheVolume,
} from "~/server-fns/api";
import { consoleCacheGenerationsQuery, consoleCacheVolumesQuery, consoleRunsQuery } from "./runs";
import { toConsoleView, type ConsoleRepo, type ConsoleRunStatus } from "./model";

const statusDotClass: Record<ConsoleRunStatus, string> = {
  canceled: "bg-zinc-400",
  failed: "bg-red-500",
  passed: "bg-emerald-500",
  queued: "bg-amber-500",
  running: "bg-blue-500",
  unknown: "bg-zinc-300",
};

export function RepoConsole() {
  const auth = useSignedInAuth();
  const view = toConsoleView(useSuspenseQuery(consoleRunsQuery(auth)).data);
  const cacheVolumes = useSuspenseQuery(consoleCacheVolumesQuery(auth)).data;

  return (
    <ConsoleCanvas>
      <section className="grid gap-4 md:grid-cols-2">
        {view.repos.length > 0 ? (
          view.repos.map((repo) => <RepoCard key={repo.key} repo={repo} />)
        ) : (
          <ConsoleEmpty />
        )}
      </section>
      <CachePanel volumes={cacheVolumes} />
    </ConsoleCanvas>
  );
}

export function RepoConsoleSkeleton() {
  return (
    <ConsoleCanvas>
      <div className="surface-elevated w-full rounded-lg p-4">
        <Skeleton className="h-4 w-24" />
        <div className="mt-5 flex flex-col gap-5">
          {Array.from({ length: 4 }, (_, index) => (
            <div key={index} className="flex items-center gap-3">
              <Skeleton className="size-2.5 rounded-full" />
              <Skeleton className="h-4 flex-1" />
            </div>
          ))}
        </div>
      </div>
    </ConsoleCanvas>
  );
}

function ConsoleCanvas({ children }: { readonly children: ReactNode }) {
  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-10 md:py-16">
      <div className="flex min-h-[28rem] flex-col gap-6">{children}</div>
    </div>
  );
}

function RepoCard({ repo }: { readonly repo: ConsoleRepo }) {
  return (
    <section className="surface-elevated w-full rounded-lg p-4">
      <h2 className="font-mono text-xs tracking-tight text-muted-foreground">{repo.name}</h2>
      <ol className="mt-4 flex flex-col">
        {repo.commits.map((commit, index) => (
          <li key={commit.key} className="relative flex gap-3 pb-5 last:pb-0">
            <div className="relative flex w-3 shrink-0 justify-center">
              {index < repo.commits.length - 1 ? (
                <span
                  aria-hidden="true"
                  className="absolute left-1/2 top-[7px] h-full w-px -translate-x-1/2 bg-border"
                />
              ) : null}
              <span
                className={cn(
                  "relative z-10 mt-[5px] size-2.5 rounded-full",
                  statusDotClass[commit.status],
                )}
              />
            </div>
            <div className="flex min-w-0 flex-1 items-baseline justify-between gap-3">
              <span className="truncate text-sm text-foreground">{commit.label}</span>
              <ClientOnly fallback={null}>
                <ElapsedTime
                  className="shrink-0 text-xs text-muted-foreground"
                  title={formatDateTimeUTC(commit.updatedAt)}
                  value={commit.updatedAt}
                />
              </ClientOnly>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}

function CachePanel({ volumes }: { readonly volumes: readonly CacheVolume[] }) {
  if (volumes.length === 0) {
    return (
      <section className="surface-elevated rounded-lg p-4">
        <h2 className="font-mono text-xs tracking-tight text-muted-foreground">Caches</h2>
        <p className="mt-4 text-sm text-muted-foreground">No durable caches yet</p>
      </section>
    );
  }

  return (
    <section className="surface-elevated rounded-lg p-4">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="font-mono text-xs tracking-tight text-muted-foreground">Caches</h2>
        <span className="text-xs text-muted-foreground">{volumes.length}</span>
      </div>
      <div className="mt-4 flex flex-col divide-y divide-border">
        {volumes.map((volume) => (
          <CacheVolumeRow key={volume.cache_volume_id} volume={volume} />
        ))}
      </div>
    </section>
  );
}

function CacheVolumeRow({ volume }: { readonly volume: CacheVolume }) {
  const auth = useSignedInAuth();
  const generations = useSuspenseQuery(
    consoleCacheGenerationsQuery(auth, volume.cache_volume_id),
  ).data;

  return (
    <article className="py-4 first:pt-0 last:pb-0">
      <div className="flex flex-wrap items-baseline justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate text-sm font-medium text-foreground">{volume.name}</h3>
          <p className="mt-1 truncate text-xs text-muted-foreground">
            {volume.repository_full_name || volume.scope_ref}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-3 text-xs text-muted-foreground">
          <span>{formatBytes(volume.used_bytes)}</span>
          <span>{volume.generation_count} generations</span>
        </div>
      </div>
      <ol className="mt-3 flex flex-col gap-3">
        {generations.map((generation) => (
          <CacheGenerationRow key={generation.cache_generation_id} generation={generation} />
        ))}
      </ol>
    </article>
  );
}

function CacheGenerationRow({ generation }: { readonly generation: CacheGeneration }) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();
  const lastUsedAt = generation.last_used_at ?? generation.committed_at ?? generation.sealed_at;
  const invalidateCaches = () =>
    queryClient.invalidateQueries({ queryKey: ["auth", auth.cachePartition, auth.selectedOrgId] });
  const deleteGeneration = useMutation({
    mutationFn: () =>
      deleteCacheGeneration({ data: { cacheGenerationId: generation.cache_generation_id } }),
    onSuccess: invalidateCaches,
  });
  const deletePathMutation = useMutation({
    mutationFn: (path: string) =>
      deleteCachePath({ data: { cacheVolumeId: generation.cache_volume_id, path } }),
    onSuccess: invalidateCaches,
  });

  return (
    <li className="rounded-md border border-border p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-xs text-foreground">
              {shortID(generation.cache_generation_id)}
            </span>
            {generation.current ? (
              <span className="rounded-sm bg-emerald-500/10 px-1.5 py-0.5 text-[0.6875rem] font-medium text-emerald-700 dark:text-emerald-300">
                Current
              </span>
            ) : null}
            <span className="rounded-sm bg-muted px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground">
              {generation.state}
            </span>
          </div>
          <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>{formatBytes(generation.used_bytes)}</span>
            <span>{formatBytes(generation.written_bytes)} written</span>
            {lastUsedAt ? (
              <ClientOnly fallback={null}>
                <ElapsedTime title={formatDateTimeUTC(lastUsedAt)} value={lastUsedAt} />
              </ClientOnly>
            ) : null}
          </div>
        </div>
        <Button
          aria-label="Delete cache generation"
          disabled={deleteGeneration.isPending}
          onClick={() => {
            if (window.confirm("Delete this durable cache generation?")) {
              deleteGeneration.mutate();
            }
          }}
          size="icon-sm"
          title="Delete generation"
          variant="destructive"
        >
          <Trash2 />
        </Button>
      </div>
      {generation.bind_paths.length > 0 ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {generation.bind_paths.map((path) => (
            <Button
              key={path}
              aria-label={`Delete cache path ${path}`}
              className="max-w-full"
              disabled={deletePathMutation.isPending}
              onClick={() => {
                if (window.confirm(`Delete durable cache generations containing ${path}?`)) {
                  deletePathMutation.mutate(path);
                }
              }}
              size="xs"
              title="Delete path"
              variant="outline"
            >
              <FolderMinus />
              <span className="min-w-0 truncate font-mono">{path}</span>
            </Button>
          ))}
        </div>
      ) : null}
    </li>
  );
}

function ConsoleEmpty() {
  return (
    <div className="surface-elevated flex min-h-[18rem] flex-col items-center justify-center rounded-lg p-4 text-center md:col-span-2">
      <p className="text-sm font-medium text-foreground">No connected repository yet</p>
      <p className="mt-2 max-w-xs text-sm text-muted-foreground">
        Connect a repository and run CI from the Verself CLI. Recent commits will appear here.
      </p>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"] as const;
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  const digits = value >= 10 || unitIndex === 0 ? 0 : 1;
  return `${value.toFixed(digits)} ${units[unitIndex]}`;
}

function shortID(id: string): string {
  return id.slice(0, 8);
}
