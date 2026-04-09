import { type QueryClient, type QueryKey, type QueryOptions } from "@tanstack/react-query";
import { notFound } from "@tanstack/react-router";
import { SandboxRentalApiError } from "~/server-fns/api";

export async function ensureOrNotFound<TQueryFnData, TQueryKey extends QueryKey = QueryKey>(
  queryClient: QueryClient,
  options: QueryOptions<TQueryFnData, Error, TQueryFnData, TQueryKey>,
) {
  try {
    return await queryClient.ensureQueryData(options);
  } catch (error) {
    if (error instanceof SandboxRentalApiError && error.status === 404) {
      throw notFound();
    }
    throw error;
  }
}
