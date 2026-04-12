import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vite-plus/test";
import { buildCatalogTree } from "./catalog.ts";
import { policyFormFromDocument } from "./reducer.ts";
import { fixtureOperations } from "./test-fixtures.ts";
import { PolicyMatrix } from "./tree.tsx";

const catalog = buildCatalogTree(fixtureOperations);

function makeForm(roleKey: string, permissions: readonly string[]) {
  return policyFormFromDocument({
    org_id: "1",
    version: 0,
    roles: [{ role_key: roleKey, display_name: "Tester", permissions }],
    updated_at: "",
    updated_by: "",
  });
}

describe("PolicyMatrix render", () => {
  it("renders the permission column header", () => {
    const html = renderToString(
      <PolicyMatrix
        catalog={catalog}
        state={makeForm("tester", [])}
        dispatch={() => {}}
        canEdit={true}
      />,
    );
    expect(html).toContain("Permission");
  });

  it("renders an indeterminate checkbox for a mixed group state", () => {
    // Half of billing's leaves are on (billing:read), half off (billing:checkout).
    const html = renderToString(
      <PolicyMatrix
        catalog={catalog}
        state={makeForm("tester", ["billing:read"])}
        dispatch={() => {}}
        canEdit={true}
      />,
    );
    // aria-checked is the WAI-ARIA contract for tri-state checkboxes
    // (https://www.w3.org/WAI/ARIA/apg/patterns/checkbox-tri-state/), so the
    // assertion stays correct even if Radix renames its internal data-state.
    expect(html).toContain('aria-checked="mixed"');
  });

  it("renders a checked checkbox when the group is fully on", () => {
    const html = renderToString(
      <PolicyMatrix
        catalog={catalog}
        state={makeForm("tester", ["billing:read", "billing:checkout"])}
        dispatch={() => {}}
        canEdit={true}
      />,
    );
    expect(html).toContain('aria-checked="true"');
  });

  it("renders an unchecked checkbox when the group is fully off", () => {
    const html = renderToString(
      <PolicyMatrix
        catalog={catalog}
        state={makeForm("tester", [])}
        dispatch={() => {}}
        canEdit={true}
      />,
    );
    expect(html).toContain('aria-checked="false"');
  });

  it("includes an aria-expanded toggle for every group row", () => {
    const html = renderToString(
      <PolicyMatrix
        catalog={catalog}
        state={makeForm("tester", [])}
        dispatch={() => {}}
        canEdit={true}
      />,
    );
    expect(html).toContain('aria-expanded="true"');
    expect(html).toMatch(/aria-label="Collapse Billing"/);
  });
});
