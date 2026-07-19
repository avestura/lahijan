// scripts/generate-sitemap.mjs
//
// Post-build step: walk dist/*.html and emit sitemap.xml + robots.txt.
// Generates URLs against VITE_SITE_URL (default: http://localhost:4173).
// Run after `vite-react-ssg build` so the file list reflects what was
// actually prerendered.
import { writeFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const website = path.resolve(__dirname, "..");
const dist = path.join(website, "dist");

const siteUrl = (process.env.VITE_SITE_URL ?? "http://localhost:4173").replace(/\/+$/, "");

if (!existsSync(dist)) {
  console.error(`[sitemap] dist/ not found at ${dist} — run the build first.`);
  process.exit(1);
}

/** @returns {Array<{path: string, mtime: Date}>} */
function walkHtml(dir, base = "") {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    const rel = `${base}${entry}`;
    const st = statSync(full);
    if (st.isDirectory()) {
      out.push(...walkHtml(full, `${rel}/`));
    } else if (entry.endsWith(".html")) {
      out.push({ path: rel, mtime: st.mtime });
    }
  }
  return out;
}

/** Map a dist-relative HTML path to its public URL path. */
function htmlToUrlPath(p) {
  // index.html -> "/"
  // features.html -> "/features"
  // legal/privacy.html -> "/legal/privacy"
  if (p === "index.html") return "/";
  if (p.endsWith("/index.html")) return `/${p.slice(0, -"/index.html".length)}`;
  if (p.endsWith(".html")) return `/${p.slice(0, -".html".length)}`;
  return `/${p}`;
}

// Skip the static-loader-data manifest (JSON) — walkHtml already filters to
// .html, but we still want to exclude any non-page HTML that may land in
// dist (none today, but cheap insurance).
const SKIP = new Set([]);

const htmlFiles = walkHtml(dist).filter((f) => !SKIP.has(f.path));
const urls = htmlFiles
  .map((f) => ({ loc: htmlToUrlPath(f.path), lastmod: f.mtime.toISOString() }))
  .sort((a, b) => a.loc.localeCompare(b.loc));

const sitemap = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls
  .map(
    (u) =>
      `  <url>\n    <loc>${siteUrl}${u.loc}</loc>\n    <lastmod>${u.lastmod}</lastmod>\n  </url>`,
  )
  .join("\n")}
</urlset>
`;

const today = new Date().toISOString().slice(0, 10);
const robots = `# https://www.robotstxt.org/robotstxt.html
User-agent: *
Allow: /
Disallow: /web/

Sitemap: ${siteUrl}/sitemap.xml
`;

writeFileSync(path.join(dist, "sitemap.xml"), sitemap, "utf8");
writeFileSync(path.join(dist, "robots.txt"), robots, "utf8");

console.log(
  `[sitemap] wrote ${urls.length} URLs to dist/sitemap.xml and dist/robots.txt (site=${siteUrl}, generated=${today})`,
);
