/**
 * canonicalizeRecordName unit tests. Pure function; runs in plain Node.
 */
import { describe, expect, it } from "vitest";

import { canonicalizeRecordName } from "./schemas";

describe("canonicalizeRecordName", () => {
  const zone = "aryan.com.";

  it.each([
    ["relative label", "www", "www.aryan.com."],
    ["multi-label relative", "sub.www", "sub.www.aryan.com."],
    ["apex @", "@", "aryan.com."],
    ["empty -> apex", "", "aryan.com."],
    ["FQDN missing dot", "www.aryan.com", "www.aryan.com."],
    ["apex missing dot", "aryan.com", "aryan.com."],
    ["already canonical", "www.aryan.com.", "www.aryan.com."],
    ["uppercased input is lowercased", "WWW", "www.aryan.com."],
    ["whitespace is trimmed", "  www  ", "www.aryan.com."],
  ])("%s", (_label, input, expected) => {
    expect(canonicalizeRecordName(input, zone)).toBe(expected);
  });

  it("normalizes a zone name passed without a trailing dot", () => {
    expect(canonicalizeRecordName("www", "aryan.com")).toBe("www.aryan.com.");
  });
});
