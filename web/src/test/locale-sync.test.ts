/**
 * Locale sync test.
 *
 * Asserts en.json and fa.json are key-for-key identical (the ADR-0017
 * non-negotiable). CI runs this via `npm run test` so a one-locale change
 * fails CI.
 */
import { describe, expect, it } from "vitest";

import en from "@/locales/en.json";
import fa from "@/locales/fa.json";

type Dict = Record<string, unknown>;

function flattenKeys(obj: unknown, prefix = ""): string[] {
  if (obj === null || typeof obj !== "object") return [];
  if (Array.isArray(obj)) return [prefix];
  const out: string[] = [];
  for (const [key, value] of Object.entries(obj as Dict)) {
    const next = prefix ? `${prefix}.${key}` : key;
    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      out.push(...flattenKeys(value, next));
    } else {
      out.push(next);
    }
  }
  return out.sort();
}

describe("locale key sync", () => {
  it("en.json and fa.json expose the exact same key set", () => {
    const enKeys = flattenKeys(en);
    const faKeys = flattenKeys(fa);
    expect(new Set(faKeys)).toEqual(new Set(enKeys));
  });

  it("every key in en.json has a value", () => {
    function check(obj: unknown, prefix = "") {
      if (obj === null || typeof obj !== "object") return;
      for (const [key, value] of Object.entries(obj as Dict)) {
        const next = prefix ? `${prefix}.${key}` : key;
        if (value !== null && typeof value === "object" && !Array.isArray(value)) {
          check(value, next);
        } else {
          expect(value, `locale key ${next} should be a non-empty string`).to.satisfy(
            (v: unknown) => typeof v === "string" && v.length > 0,
          );
        }
      }
    }
    check(en);
    check(fa);
  });
});
