import { createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import {
  beginLogin,
  createAuthMiddleware,
  finishLogin,
  getAuthViewer,
  logout,
  type AuthSession,
} from "@forge-metal/auth-web";
import { getAuthConfig } from "../server/auth";

const loginRedirectInputSchema = v.object({
  redirectTo: v.optional(v.nullable(v.string())),
});

export interface RentASandboxAuthContext {
  auth: AuthSession;
}

export const rentASandboxAuthMiddleware = createAuthMiddleware(getAuthConfig);
export const getViewer = createServerFn({ method: "GET" }).handler(async () =>
  getAuthViewer(getAuthConfig),
);
export const getLoginRedirectURL = createServerFn({ method: "GET" })
  .inputValidator(loginRedirectInputSchema)
  .handler(async ({ data }) => beginLogin(getAuthConfig(), data.redirectTo));
export const getCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { redirectTo } = await finishLogin(getAuthConfig());
  return redirectTo;
});
export const getLogoutRedirectURL = createServerFn({ method: "GET" }).handler(async () =>
  logout(getAuthConfig),
);
