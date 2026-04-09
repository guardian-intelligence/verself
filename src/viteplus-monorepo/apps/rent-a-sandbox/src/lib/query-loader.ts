import {
  type EnsureQueryDataOptions,
  type QueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import { notFound } from "@tanstack/react-router";
import { isSandboxRentalNotFound } from "~/server-fns/api";

export async function ensureOrNotFound<
  TQueryFnData,
  TError = Error,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
>(
  queryClient: QueryClient,
  options: EnsureQueryDataOptions<TQueryFnData, TError, TData, TQueryKey>,
) {
  try {
    return await queryClient.ensureQueryData(options);
  } catch (error) {
    if (isSandboxRentalNotFound(error)) {
      throw notFound();
    }
    throw error;
  }
}
