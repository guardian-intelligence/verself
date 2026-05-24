import { useMemo } from "react";
import { flightFixture } from "./fixtures";
import type { Flight } from "./model";

export type LiveFlights = {
  readonly flights: readonly Flight[];
  readonly isLoading: boolean;
};

export function useFlights(): LiveFlights {
  const flights = useMemo(() => flightFixture("building-on-time"), []);
  return { flights, isLoading: false };
}
