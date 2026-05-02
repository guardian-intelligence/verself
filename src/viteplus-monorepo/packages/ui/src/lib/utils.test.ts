import { describe, expect, it } from "vite-plus/test";
import { cn } from "./utils.ts";

describe("cn", () => {
  it("merges class names", () => {
    expect(cn("px-2", "px-4")).toBe("px-4");
  });

  it("handles conditional classes", () => {
    const isHidden = false;
    expect(cn("base", isHidden && "hidden", "extra")).toBe("base extra");
  });
});
