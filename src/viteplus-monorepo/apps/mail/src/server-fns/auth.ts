import { createMiddleware, createServerFn } from "@tanstack/react-start";

export const webmailAuthMiddleware = createMiddleware({ type: "function" }).server(async ({ next }) => {
  const { getWebmailAuthSession } = await import("../server/auth.server");
  const auth = await getWebmailAuthSession();
  if (!auth) {
    throw new Error("Authentication required");
  }

  return next({
    context: {
      auth,
    },
  });
});

export const getViewer = createServerFn({ method: "GET" }).handler(async () => {
  const { getWebmailViewer } = await import("../server/auth.server");
  return getWebmailViewer();
});

export const getLoginRedirectURL = createServerFn({ method: "GET" })
  .inputValidator((data: { redirectTo?: string | null }) => data)
  .handler(async ({ data }) => {
    const { beginWebmailLogin } = await import("../server/auth.server");
    return beginWebmailLogin(data.redirectTo);
  });

export const getCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { finishWebmailLogin } = await import("../server/auth.server");
  const { redirectTo } = await finishWebmailLogin();
  return redirectTo;
});

export const getLogoutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { logoutWebmail } = await import("../server/auth.server");
  return logoutWebmail();
});
