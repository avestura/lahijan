/**
 * Example test for the cn utility. Proves Vitest + RTL pipeline is wired
 * end to end and that TypeScript strict mode picks the test files up.
 */
import { describe, expect, it } from "vitest";

import { cn } from "@/lib/utils";

describe("cn utility", () => {
  it("joins conditional classes", () => {
    const cond = false;
    expect(cn("a", cond && "b", undefined, "c")).toBe("a c");
  });

  it("tailwind-merge resolves conflicts (last wins)", () => {
    expect(cn("p-2", "p-4")).toBe("p-4");
  });

  it("handles arrays and objects (via clsx)", () => {
    expect(cn(["a", "b"], { c: true, d: false })).toBe("a b c");
  });
});
