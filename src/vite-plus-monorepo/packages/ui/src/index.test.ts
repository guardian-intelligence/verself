import { describe, expect, it } from "vite-plus/test";
import { baselineCapabilities, summarizeCapabilities } from "./index.tsx";

describe("summarizeCapabilities", () => {
  it("describes the configured baseline checks", () => {
    expect(summarizeCapabilities(baselineCapabilities)).toBe("3 workspace checkpoints are live.");
  });
});
