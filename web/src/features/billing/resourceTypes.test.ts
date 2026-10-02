import { describe, expect, it } from "vitest";

import { BILLING_RESOURCE_TYPES, suggestedUnit } from "./resourceTypes";

describe("billing resource types", () => {
  it("suggests the conventional unit for a known type", () => {
    expect(suggestedUnit("compute.cpu")).toBe("core-hours");
    expect(suggestedUnit("network.egress")).toBe("GiB");
  });

  it("returns an empty string for an unknown or empty type", () => {
    expect(suggestedUnit("")).toBe("");
    expect(suggestedUnit("nope.nothing")).toBe("");
  });

  it("lists every type once, each with a unit", () => {
    const values = BILLING_RESOURCE_TYPES.map((r) => r.value);
    expect(new Set(values).size).toBe(values.length);
    for (const r of BILLING_RESOURCE_TYPES) expect(r.unit).not.toBe("");
  });
});
