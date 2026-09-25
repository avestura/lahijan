/**
 * Vite plugin: documentation pages.
 *
 *   import doc from "@/content/docs/compute/instances.md"
 *     -> { title, description, html, headings }   (compiled at build time)
 *
 *   import index from "virtual:docs-index"
 *     -> [{ slug, title, description, headings, text }]  (search data; the
 *        docs search loads it lazily, so it is its own chunk)
 */
import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import type { Plugin } from "vite";

import { compileDoc } from "./markdown";

const VIRTUAL_INDEX = "virtual:docs-index";
const RESOLVED_INDEX = `\0${VIRTUAL_INDEX}`;

function walk(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...walk(full));
    else if (entry.endsWith(".md") && entry !== "README.md") out.push(full);
  }
  return out;
}

export function docsPlugin(contentDir: string): Plugin {
  let base = "/";
  const root = path.resolve(contentDir);
  const isDoc = (id: string) => {
    const file = path.resolve(id.split("?")[0] ?? id);
    return (
      file.endsWith(".md") &&
      path.basename(file) !== "README.md" &&
      file.startsWith(root + path.sep)
    );
  };

  return {
    name: "lahijan-docs",
    enforce: "pre",
    configResolved(config) {
      base = config.base;
    },
    resolveId(id) {
      return id === VIRTUAL_INDEX ? RESOLVED_INDEX : null;
    },
    load(id) {
      if (id !== RESOLVED_INDEX) return null;
      const entries = walk(root)
        .sort()
        .map((file) => {
          this.addWatchFile(file);
          const doc = compileDoc(readFileSync(file, "utf8"), base, file);
          const slug = path.relative(root, file).replace(/\\/g, "/").replace(/\.md$/, "");
          return {
            slug: slug === "index" ? "" : slug,
            title: doc.title,
            description: doc.description,
            headings: doc.headings.map((h) => h.text),
            text: doc.text.slice(0, 4000),
          };
        });
      return `export default ${JSON.stringify(entries)};`;
    },
    transform(code, id) {
      if (!isDoc(id)) return null;
      const doc = compileDoc(code, base, id);
      const { title, description, html, headings } = doc;
      return {
        code: `export default ${JSON.stringify({ title, description, html, headings })};`,
        map: null,
      };
    },
  };
}
