import { useLiveQuery } from "@tanstack/react-db";
import { useMemo } from "react";
import { useSignedInAuth } from "@verself/auth-web/react";
import { createNotificationInboxStateCollection } from "~/lib/collections";

export function useNotificationInboxState() {
  const auth = useSignedInAuth();
  const collection = useMemo(
    () => createNotificationInboxStateCollection(auth),
    [auth.cachePartition],
  );
  const liveQuery = useLiveQuery(collection);
  const inboxState = liveQuery.data[0] ?? null;
  const nextSequence = electricSequenceNumber(inboxState?.next_sequence, 1);
  const latestSequence = Math.max(nextSequence - 1, 0);
  const readUpToSequence = electricSequenceNumber(inboxState?.read_up_to_sequence, 0);
  const unreadCount = Math.min(Math.max(latestSequence - readUpToSequence, 0), 999);

  return {
    ...liveQuery,
    inboxState,
    latestSequence,
    revision: inboxState?.updated_at ?? "",
    readUpToSequence,
    unreadCount,
  };
}

function electricSequenceNumber(value: unknown, fallback: number): number {
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value >= 0 ? value : fallback;
  }
  if (typeof value === "bigint") {
    if (value < 0n || value > BigInt(Number.MAX_SAFE_INTEGER)) {
      return fallback;
    }
    return Number(value);
  }
  if (typeof value === "string" && /^\d+$/.test(value)) {
    const parsed = BigInt(value);
    if (parsed > BigInt(Number.MAX_SAFE_INTEGER)) {
      return fallback;
    }
    return Number(parsed);
  }
  return fallback;
}
