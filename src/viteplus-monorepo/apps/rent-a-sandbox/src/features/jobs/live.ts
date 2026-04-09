import { useLiveQuery } from "@tanstack/react-db";
import { useMemo } from "react";
import {
  createExecutionLogsCollection,
  createExecutionsCollection,
} from "~/lib/collections";

export function useExecutionRows(orgId: string) {
  const collection = useMemo(() => createExecutionsCollection(orgId), [orgId]);

  const liveQuery = useLiveQuery(
    (q) =>
      q
        .from({ execution: collection })
        .select(({ execution }) => execution)
        .orderBy(({ execution }) => execution.created_at, "desc"),
    [collection],
  );

  const executions = useMemo(
    () => sortExecutions(liveQuery.data ?? []),
    [liveQuery.data],
  );

  return {
    ...liveQuery,
    executions,
    isEmpty: Array.isArray(liveQuery.data) && liveQuery.data.length === 0,
  };
}

export function useExecutionLogs(attemptId: string) {
  const collection = useMemo(
    () => createExecutionLogsCollection(attemptId),
    [attemptId],
  );

  const liveQuery = useLiveQuery(
    (q) =>
      q
        .from({ log: collection })
        .select(({ log }) => log)
        .orderBy(({ log }) => log.seq, "asc"),
    [collection],
  );

  const logText = useMemo(
    () => buildLogText(liveQuery.data ?? []),
    [liveQuery.data],
  );

  return {
    ...liveQuery,
    logText,
    isEmpty: Array.isArray(liveQuery.data) && liveQuery.data.length === 0,
  };
}

export function sortExecutions<T extends { created_at: string }>(
  executions: readonly T[],
) {
  return [...executions].sort(
    (a, b) =>
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
}

export function buildLogText(chunks: readonly { chunk: string }[]) {
  if (chunks.length === 0) return "";
  return chunks.map((chunk) => chunk.chunk).join("");
}

export function formatExecutionRepo(repo?: string, repoURL?: string) {
  if (repo) return repo;
  if (!repoURL) return "--";
  return repoURL.replace("https://", "");
}
