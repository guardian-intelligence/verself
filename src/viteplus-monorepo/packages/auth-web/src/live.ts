import { createElectricShapeCollection } from "@verself/web-env";
import {
  webAuthAccountSchema,
  webAuthClientSchema,
  webAuthSessionSchema,
  type WebAuthAccount,
  type WebAuthClient,
  type WebAuthSession,
} from "./isomorphic.ts";

export type LiveAuthCollections = ReturnType<typeof createLiveAuthCollections>;

export function createLiveAuthCollections(onAuthUnavailable: () => void) {
  let authUnavailable = false;
  const onError = (error: Error) => {
    const status = fetchErrorStatus(error);
    if (status === 401 || status === 403) {
      if (authUnavailable) {
        return;
      }
      authUnavailable = true;
      void clearLiveAuthCollections(collections);
      onAuthUnavailable();
    }
  };

  const collections = {
    client: createElectricShapeCollection({
      id: "auth:client",
      schema: webAuthClientSchema,
      getKey: (item: WebAuthClient) => item.client_handle,
      shapePath: "/api/sync/auth/client",
      onError,
    }),
    accounts: createElectricShapeCollection({
      id: "auth:accounts",
      schema: webAuthAccountSchema,
      getKey: (item: WebAuthAccount) => `${item.client_handle}:${item.account_handle}`,
      shapePath: "/api/sync/auth/accounts",
      onError,
    }),
    sessions: createElectricShapeCollection({
      id: "auth:sessions",
      schema: webAuthSessionSchema,
      getKey: (item: WebAuthSession) => `${item.client_handle}:${item.session_handle}`,
      shapePath: "/api/sync/auth/sessions",
      onError,
    }),
  };
  return collections;
}

export async function clearLiveAuthCollections(collections: LiveAuthCollections): Promise<void> {
  await Promise.allSettled([
    collections.client.cleanup(),
    collections.accounts.cleanup(),
    collections.sessions.cleanup(),
  ]);
}

function fetchErrorStatus(error: Error): number {
  const candidate = error as Error & { status?: unknown };
  return typeof candidate.status === "number" ? candidate.status : 0;
}
