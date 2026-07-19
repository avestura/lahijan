/**
 * i18n tests — locale + direction metadata.
 */
import { describe, expect, it } from "vitest";

import {
  DEFAULT_LOCALE,
  isRTL,
  LOCALE_DIRECTIONS,
  LOCALE_LABELS,
  SUPPORTED_LOCALES,
} from "@/lib/i18n";

describe("i18n metadata", () => {
  it("exposes en + fa as the two supported locales", () => {
    expect(SUPPORTED_LOCALES).toEqual(["en", "fa"]);
  });

  it("defaults to English", () => {
    expect(DEFAULT_LOCALE).toBe("en");
  });

  it("marks fa as RTL and en as LTR", () => {
    expect(LOCALE_DIRECTIONS.en).toBe("ltr");
    expect(LOCALE_DIRECTIONS.fa).toBe("rtl");
  });

  it("isRTL() returns true only for fa", () => {
    expect(isRTL("fa")).toBe(true);
    expect(isRTL("en")).toBe(false);
  });

  it("exposes a non-empty label for every supported locale", () => {
    for (const l of SUPPORTED_LOCALES) {
      expect(LOCALE_LABELS[l].length).toBeGreaterThan(0);
    }
  });
});
