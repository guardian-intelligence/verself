import { expect, test } from "@playwright/test";
import { deriveProductBaseURL } from "@verself/web-env";

const productBaseURL = deriveProductBaseURL();

test.describe("Electric shape authorization", () => {
  test("generic Electric shape endpoint is not publicly table-controlled", async ({ request }) => {
    const url = new URL("/v1/shape", productBaseURL);
    url.searchParams.set("table", "executions");
    url.searchParams.set("offset", "-1");

    const response = await request.get(url.toString());

    expect([401, 403, 404, 405]).toContain(response.status());
  });
});
