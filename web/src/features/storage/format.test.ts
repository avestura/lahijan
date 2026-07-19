/**
 * formatBytes unit tests. Pure-function, runs in plain Node.
 */
import { describe, expect, it } from "vitest";

import { formatBytes } from "./format";

describe("formatBytes", () => {
  it.each([
    [0, "0 B"],
    [1, "1 B"],
    [1023, "1023 B"],
    [1024, "1.00 KiB"],
    [1024 * 1024, "1.00 MiB"],
    [1024 * 1024 * 1024, "1.00 GiB"],
    [1024 * 1024 * 1024 * 1024, "1.00 TiB"],
  ])("formats %d bytes as %s", (bytes, expected) => {
    expect(formatBytes(bytes)).toBe(expected);
  });

  it("handles undefined and negative input", () => {
    expect(formatBytes(undefined)).toBe("0 B");
    expect(formatBytes(-1)).toBe("0 B");
  });

  it("handles bigint input", () => {
    expect(formatBytes(1024n)).toBe("1.00 KiB");
    expect(formatBytes(0n)).toBe("0 B");
  });

  it("rounds and drops trailing precision as values grow", () => {
    expect(formatBytes(1024 * 50)).toBe("50.0 KiB");
    expect(formatBytes(1024 * 500)).toBe("500 KiB");
    expect(formatBytes(1024 * 1024 * 12.5)).toBe("12.5 MiB");
  });
});
