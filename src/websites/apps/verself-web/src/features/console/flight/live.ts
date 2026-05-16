import { useMemo } from "react";
import { useLiveQuery } from "@tanstack/react-db";
import { useSignedInAuth } from "@verself/auth-web/react";
import { createFlightsCollection, type FlightRow } from "./collection";
import { toFlights, type Flight } from "./model";

export type LiveFlights = {
  readonly flights: readonly Flight[];
  readonly isLoading: boolean;
};

export function useFlights(): LiveFlights {
  const auth = useSignedInAuth();
  // Collection identity is the Electric subscription identity; memoize on the
  // auth cache partition only, so a re-render never reopens the shape.
  const collection = useMemo(() => createFlightsCollection(auth), [auth.cachePartition]);
  const liveQuery = useLiveQuery(collection);
  const flights = useMemo(
    () => toFlights(liveQuery.data as readonly FlightRow[]),
    [liveQuery.data],
  );
  return { flights, isLoading: liveQuery.isLoading };
}
