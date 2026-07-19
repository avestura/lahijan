/**
 * Audit API helper unit tests. Pure-function, runs in plain Node.
 */
import { describe, expect, it } from "vitest";

import { buildAuditExportURL } from "./api";

describe("buildAuditExportURL", () => {
  it("builds a CSV URL with no filters", () => {
    const url = buildAuditExportURL("csv", {});
    expect(url).toBe("/api/v1/audit/export?format=csv");
  });

  it("encodes the chosen format and filters", () => {
    const url = buildAuditExportURL("json", {
      action: "compute.instance.create",
      status: "success",
      actorType: "user",
    });
    expect(url).toContain("format=json");
    expect(url).toContain("action=compute.instance.create");
    expect(url).toContain("status=success");
    expect(url).toContain("actorType=user");
  });

  it("skips empty/undefined filter values", () => {
    const url = buildAuditExportURL("csv", {
      action: "",
      status: undefined,
      resourceType: "instance",
    });
    expect(url).toContain("format=csv");
    expect(url).toContain("resourceType=instance");
    expect(url).not.toContain("action=");
    expect(url).not.toContain("status=");
  });
});
