/**
 * Permission gate test.
 *
 * Verifies the role implications used by usePerm. The actual UI gating is
 * exercised by the component tests.
 */
import { describe, expect, it } from "vitest";

import { rolesImply } from "@/lib/perm";

describe("rolesImply", () => {
  it("grants everything to platform.admin", () => {
    expect(rolesImply(["platform.admin"], "compute.instance.create")).toBe(true);
    expect(rolesImply(["platform.admin"], "anything.else")).toBe(true);
  });

  it("owner and admin imply all perms (wildcard)", () => {
    expect(rolesImply(["owner"], "compute.instance.create")).toBe(true);
    expect(rolesImply(["admin"], "dns.zone.create")).toBe(true);
  });

  it("member can read and create compute instances", () => {
    expect(rolesImply(["member"], "compute.read")).toBe(true);
    expect(rolesImply(["member"], "compute.instance.create")).toBe(true);
  });

  it("viewer cannot mutate", () => {
    expect(rolesImply(["viewer"], "compute.read")).toBe(true);
    expect(rolesImply(["viewer"], "compute.instance.create")).toBe(false);
  });

  it("empty roles grants nothing", () => {
    expect(rolesImply([], "compute.read")).toBe(false);
  });

  it("unknown role grants nothing", () => {
    expect(rolesImply(["nonsense"], "compute.read")).toBe(false);
  });
});
