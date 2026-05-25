import { describe, expect, it } from "vite-plus/test";
import {
  accountSelectionLoginPath,
  resolveAuthenticatedShellFallbackPath,
  resolveShellFallbackPath,
  shellNotFoundFallbackData,
} from "../features/shell/fallback-routing.ts";

describe("route boundary fallback links", () => {
  it("uses not-found data before loose route params", () => {
    expect(
      resolveShellFallbackPath({
        notFoundData: shellNotFoundFallbackData("/guardian"),
        orgSlug: "executions",
      }),
    ).toBe("/guardian");
  });

  it("falls back to the org-scoped console route when the current params are trusted", () => {
    expect(resolveShellFallbackPath({ notFoundData: undefined, orgSlug: "guardian" })).toBe(
      "/guardian",
    );
  });

  it("rejects non-root fallback data", () => {
    expect(
      resolveShellFallbackPath({
        notFoundData: shellNotFoundFallbackData("executions/console"),
        orgSlug: null,
      }),
    ).toBe("/login");
  });

  it("resolves invalid org-route fallbacks from the authenticated selected organization", () => {
    expect(
      resolveAuthenticatedShellFallbackPath(
        {
          cachePartition: "user:1",
          clientHandle: "bc_1",
          accountHandle: "ba_1",
          isAuthenticated: true,
          orgId: "org_1",
          selectedOrgId: "org_1",
          userId: "user_1",
        },
        new Map([["org_1", { display_name: "Guardian", slug: "guardian" }]]),
      ),
    ).toBe("/guardian");
  });

  it("routes authenticated users without an organization to onboarding", () => {
    expect(
      resolveAuthenticatedShellFallbackPath(
        {
          cachePartition: "user:1",
          clientHandle: "bc_1",
          accountHandle: "ba_1",
          isAuthenticated: true,
          orgId: null,
          selectedOrgId: null,
          userId: "user_1",
        },
        new Map(),
      ),
    ).toBe("/onboarding");
  });

  it("preserves org route intent when asking the browser to select an account", () => {
    expect(accountSelectionLoginPath("/acme-corp")).toBe(
      "/login?prompt=select_account&redirect=%2Facme-corp",
    );
  });
});
