import { useLiveQuery } from "@tanstack/react-db";
import { useMemo } from "react";
import { useSignedInAuth } from "@verself/auth-web/react";
import {
  createExecutionLogsCollection,
  createExecutionsCollection,
  createRunnerProviderRepositoriesCollection,
} from "~/lib/collections";

export function useExecutionRows(orgId: string) {
  const auth = useSignedInAuth();
  const collection = useMemo(
    () => createExecutionsCollection(auth, orgId),
    [auth.cachePartition, orgId],
  );
  const liveQuery = useLiveQuery(collection);
  const executions = useMemo(() => sortExecutions(liveQuery.data), [liveQuery.data]);

  return {
    ...liveQuery,
    executions,
    isEmpty: executions.length === 0,
  };
}

export function useRunnerProviderRepositoryRows(orgId: string) {
  const auth = useSignedInAuth();
  const collection = useMemo(
    () => createRunnerProviderRepositoriesCollection(auth, orgId),
    [auth.cachePartition, orgId],
  );
  const liveQuery = useLiveQuery(collection);
  const repositories = useMemo(
    () => sortRunnerProviderRepositories(liveQuery.data),
    [liveQuery.data],
  );

  return {
    ...liveQuery,
    repositories,
    isEmpty: repositories.length === 0,
  };
}

export function useExecutionLogs(attemptId: string) {
  const auth = useSignedInAuth();
  const collection = useMemo(
    () => createExecutionLogsCollection(auth, attemptId),
    [attemptId, auth.cachePartition],
  );
  const liveQuery = useLiveQuery(collection);
  const orderedLogs = useMemo(() => sortLogChunks(liveQuery.data), [liveQuery.data]);
  const logText = useMemo(() => buildLogText(orderedLogs), [orderedLogs]);

  return {
    ...liveQuery,
    logText,
    isEmpty: orderedLogs.length === 0,
  };
}

export function sortExecutions<T extends { created_at: string }>(executions: readonly T[]) {
  return [...executions].sort(
    (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );
}

export function sortRunnerProviderRepositories<T extends { updated_at: string }>(
  repositories: readonly T[],
) {
  return [...repositories].sort(
    (a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
  );
}

export function sortLogChunks<T extends { seq: number }>(chunks: readonly T[]) {
  return [...chunks].sort((a, b) => a.seq - b.seq);
}

export function buildLogText(chunks: readonly { chunk: string }[]) {
  if (chunks.length === 0) return "";
  return chunks.map((chunk) => chunk.chunk).join("");
}
