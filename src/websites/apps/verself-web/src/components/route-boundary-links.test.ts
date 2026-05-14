import { describe, expect, it } from "vite-plus/test";
import {
  resolveAuthenticatedShellFallbackPath,
  resolveShellFallbackPath,
  shellNotFoundFallbackData,
} from "../features/shell/fallback-routing.ts";

describe("route boundary fallback links", () => {
  it("uses not-found data before loose route params", () => {
    expect(
      resolveShellFallbackPath({
        notFoundData: shellNotFoundFallbackData("/guardian/builds"),
        orgSlug: "executions",
      }),
    ).toBe("/guardian/builds");
  });

  it("falls back to an org-scoped builds route when the current params are trusted", () => {
    expect(resolveShellFallbackPath({ notFoundData: undefined, orgSlug: "guardian" })).toBe(
      "/guardian/builds",
    );
  });

  it("rejects non-root fallback data", () => {
    expect(
      resolveShellFallbackPath({
        notFoundData: shellNotFoundFallbackData("executions/builds"),
        orgSlug: null,
      }),
    ).toBe("/login");
  });

  it("resolves invalid org-route fallbacks from the authenticated selected organization", () => {
    expect(
      resolveAuthenticatedShellFallbackPath(
        {
          cachePartition: "user:1",
          isAuthenticated: true,
          orgId: "org_1",
          selectedOrgId: "org_1",
          userId: "user_1",
        },
        new Map([["org_1", { display_name: "Guardian", slug: "guardian" }]]),
      ),
    ).toBe("/guardian/builds");
  });
});
