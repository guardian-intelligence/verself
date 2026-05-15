import type { ReactNode } from "react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { ClientOnly } from "@tanstack/react-router";
import { useSignedInAuth } from "@verself/auth-web/react";
import { ElapsedTime } from "@verself/ui/components/elapsed-time";
import { Skeleton } from "@verself/ui/components/ui/skeleton";
import { cn } from "@verself/ui/lib/utils";
import { formatDateTimeUTC } from "~/lib/format";
import { consoleRunsQuery } from "./runs";
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

  return (
    <ConsoleCanvas>
      {view.repos.length > 0 ? (
        view.repos.map((repo) => <RepoCard key={repo.key} repo={repo} />)
      ) : (
        <ConsoleEmpty />
      )}
    </ConsoleCanvas>
  );
}

export function RepoConsoleSkeleton() {
  return (
    <ConsoleCanvas>
      <div className="surface-elevated w-full max-w-sm rounded-lg p-4">
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
      <div className="surface-elevated flex min-h-[28rem] flex-col gap-4 rounded-xl p-4 md:p-6">
        {children}
      </div>
    </div>
  );
}

function RepoCard({ repo }: { readonly repo: ConsoleRepo }) {
  return (
    <section className="surface-elevated w-full max-w-sm rounded-lg p-4">
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

function ConsoleEmpty() {
  return (
    <div className="flex min-h-[24rem] flex-col items-center justify-center text-center">
      <p className="text-sm font-medium text-foreground">No connected repository yet</p>
      <p className="mt-2 max-w-xs text-sm text-muted-foreground">
        Connect a repository and run CI from the Verself CLI. Recent commits will appear here.
      </p>
    </div>
  );
}
