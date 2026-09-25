// scripts/pages-404.mjs
//
// Post-build step for static hosts such as GitHub Pages:
//   - dist/404.html: the prerendered not-found page. Hosts serve it for any
//     unknown path; the client router then renders the same not-found view.
//   - dist/.nojekyll: stops GitHub Pages from running Jekyll, which would
//     drop files and folders that start with an underscore.
import { copyFileSync, existsSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const dist = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "dist");
const notFound = path.join(dist, "not-found", "index.html");

if (!existsSync(notFound)) {
  console.error(`[pages] ${notFound} is missing; is /not-found prerendered?`);
  process.exit(1);
}
copyFileSync(notFound, path.join(dist, "404.html"));
writeFileSync(path.join(dist, ".nojekyll"), "");
console.log("[pages] wrote dist/404.html and dist/.nojekyll");
