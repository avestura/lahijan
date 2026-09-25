/**
 * Documentation content checks, run over every page in src/content/docs:
 *   - nav.ts and the files agree (no missing page, no orphan page);
 *   - every page compiles;
 *   - internal links point at pages (and headings) that exist;
 *   - the style rules hold: no em or en dashes;
 *   - the generated configuration reference is up to date.
 */
import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

import { compileDoc } from "../../scripts/docs/markdown";
import {
  CONFIG_FILE,
  DESCRIPTIONS_FILE,
  OUTPUT_FILE,
  renderConfigReference,
} from "../../scripts/docs/config-reference.mjs";
import { DOCS_PAGES } from "@/docs/nav";

const ROOT = path.resolve(__dirname, "../content/docs");

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) return walk(full);
    return entry.endsWith(".md") && entry !== "README.md" ? [full] : [];
  });
}

const files = walk(ROOT);
const slugOf = (file: string) => {
  const s = path.relative(ROOT, file).replace(/\\/g, "/").replace(/\.md$/, "");
  return s === "index" ? "" : s;
};
const docs = new Map(files.map((f) => [slugOf(f), { file: f, src: readFileSync(f, "utf8") }]));

describe("docs content", () => {
  it("has a page for every nav entry", () => {
    const missing = DOCS_PAGES.filter((p) => !docs.has(p.slug)).map((p) => p.slug || "index");
    expect(missing).toEqual([]);
  });

  it("has no page missing from the nav", () => {
    const listed = new Set(DOCS_PAGES.map((p) => p.slug));
    const orphans = [...docs.keys()].filter((s) => !listed.has(s));
    expect(orphans).toEqual([]);
  });

  it("uses no em or en dashes", () => {
    const hits: string[] = [];
    for (const [slug, { src }] of docs) {
      src.split("\n").forEach((line, i) => {
        if (/[–—]/.test(line)) hits.push(`${slug || "index"}.md:${i + 1}`);
      });
    }
    expect(hits).toEqual([]);
  });

  it("compiles every page and links only to existing pages and headings", () => {
    const compiled = new Map(
      [...docs].map(([slug, { src, file }]) => [slug, compileDoc(src, "/", file)]),
    );
    const broken: string[] = [];
    for (const [slug, doc] of compiled) {
      for (const m of doc.html.matchAll(/href="\/docs\/?([^"#]*)(?:#([^"]*))?"/g)) {
        const target = (m[1] ?? "").replace(/\/$/, "");
        const anchor = m[2];
        const page = compiled.get(target);
        if (!page) broken.push(`${slug || "index"} -> /docs/${target}`);
        else if (
          anchor &&
          !page.headings.some((h) => h.id === anchor) &&
          !page.html.includes(`id="${anchor}"`)
        ) {
          broken.push(`${slug || "index"} -> /docs/${target}#${anchor}`);
        }
      }
    }
    expect(broken).toEqual([]);
  });

  it("has an up-to-date configuration reference", () => {
    const expected = renderConfigReference(
      readFileSync(CONFIG_FILE, "utf8"),
      readFileSync(DESCRIPTIONS_FILE, "utf8"),
    );
    expect(readFileSync(OUTPUT_FILE, "utf8").replace(/\r\n/g, "\n")).toBe(expected);
  });
});
