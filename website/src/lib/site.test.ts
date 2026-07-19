/**
 * site.ts tests — URL normalization and absoluteUrl builder.
 */
import { describe, expect, it } from "vitest";

import {
  DASHBOARD_BASE_URL,
  DOCS_URL,
  SITE_URL,
  absoluteUrl,
  LEGAL_ENTRIES,
  NAV_ENTRIES,
} from "@/lib/site";

describe("site config", () => {
  it("SITE_URL has no trailing slash", () => {
    expect(SITE_URL.endsWith("/")).toBe(false);
  });

  it("DASHBOARD_BASE_URL ends with a slash", () => {
    expect(DASHBOARD_BASE_URL.endsWith("/")).toBe(true);
  });

  it("DOCS_URL ends with a slash (deep links append cleanly)", () => {
    expect(DOCS_URL.endsWith("/")).toBe(true);
  });

  it("absoluteUrl builds correct URLs for paths", () => {
    expect(absoluteUrl("/features")).toBe(`${SITE_URL}/features`);
    expect(absoluteUrl("pricing")).toBe(`${SITE_URL}/pricing`);
    expect(absoluteUrl("/legal/privacy")).toBe(`${SITE_URL}/legal/privacy`);
  });

  it("absoluteUrl passes through absolute URLs", () => {
    expect(absoluteUrl("https://example.com/foo")).toBe("https://example.com/foo");
  });

  it("NAV_ENTRIES contains the expected top-level routes", () => {
    const routes = NAV_ENTRIES.map((e) => e.route);
    expect(routes).toEqual(["/features", "/pricing", "/docs", "/blog", "/about"]);
  });

  it("LEGAL_ENTRIES contains the legal routes", () => {
    const routes = LEGAL_ENTRIES.map((e) => e.route);
    expect(routes).toEqual(["/legal/privacy", "/legal/terms"]);
  });

  it("every nav entry i18nKey is dotted (looks like an i18n key)", () => {
    const dotted = /^[a-z]+\.[a-z]+/;
    for (const entry of [...NAV_ENTRIES, ...LEGAL_ENTRIES]) {
      expect(entry.i18nKey, `${entry.route} should have a dotted i18n key`).toMatch(dotted);
    }
  });
});
