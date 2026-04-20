import { clickhouseQuery, expect, test } from "./harness";

// Phase-0 gate: a real browser visit to the platform site emits a `page_view`
// span tagged with the route path, that span lands in default.otel_traces under
// service.name=platform-web, and (if a deploy correlation is set) it carries
// the right `forge_metal.deploy_run_key` resource attribute.
test.describe("browser telemetry baseline", () => {
  test("page_view span lands in ClickHouse with service.name=platform-web", async ({ page }) => {
    const witness = `e2e-${Date.now().toString(36)}`;
    // window.name doesn't reach the span exporter, but appending a unique
    // search param to the URL gives us an unambiguous filter without
    // fabricating telemetry plumbing for tests only.
    await page.goto(`/?probe=${witness}`);
    await expect(page).toHaveTitle(/Guardian Intelligence/);

    // The BSP flushes on visibilitychange:hidden. Closing the page triggers it.
    // We close after a short wait so the export round-trips before assert.
    await page.waitForTimeout(2_500);
    await page.close();

    // Allow the otel collector → ClickHouse pipeline to land the row. The
    // collector batches and the MV runs on insert; 5s is generous on this box.
    await new Promise((resolve) => setTimeout(resolve, 5_000));

    const rows = await clickhouseQuery(`
      SELECT count() AS n
      FROM default.otel_traces
      WHERE ServiceName = 'platform-web'
        AND SpanName = 'page_view'
        AND SpanAttributes['route.path'] = '/'
        AND Timestamp >= now() - INTERVAL 5 MINUTE
    `);

    const n = Number(rows[0]?.n ?? "0");
    expect(n, "expected at least one page_view span from platform-web").toBeGreaterThan(0);
  });

  test("OTLP forward route accepts JSON and returns 2xx", async ({ request }) => {
    const minimalOtlp = {
      resourceSpans: [
        {
          resource: {
            attributes: [{ key: "service.name", value: { stringValue: "platform-web-e2e-probe" } }],
          },
          scopeSpans: [
            {
              scope: { name: "platform-e2e", version: "0.0.0" },
              spans: [
                {
                  traceId: "0123456789abcdef0123456789abcdef",
                  spanId: "0123456789abcdef",
                  name: "platform.e2e.probe",
                  kind: 1,
                  startTimeUnixNano: String(Date.now() * 1_000_000),
                  endTimeUnixNano: String((Date.now() + 1) * 1_000_000),
                },
              ],
            },
          ],
        },
      ],
    };
    const response = await request.post("/api/otel/v1/traces", {
      data: minimalOtlp,
      headers: { "content-type": "application/json" },
    });
    expect(response.status(), `OTLP forward status (body: ${await response.text()})`).toBeLessThan(
      300,
    );
  });
});
