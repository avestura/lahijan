// scripts/check-openapi-sync.mjs
//
// CI guard: regenerates the TypeScript schema from api/openapi.yaml via
// openapi-typescript, then fails if the regenerated file differs from the
// committed api/gen/ts/schema.d.ts. This is the frontend analogue of the
// `make openapi-verify` check that runs on the backend.
//
// The check is intentionally part of the frontend pipeline: even when the
// backend CI is green, a bad merge that updates the spec without updating
// the generated code would silently type-mismatch at runtime.
import { spawnSync } from "node:child_process";
import { readFileSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import os from "node:os";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(__dirname, "..", "..");
const spec = path.join(root, "api", "openapi.yaml");
const committed = path.join(root, "api", "gen", "ts", "schema.d.ts");
const tmp = path.join(os.tmpdir(), `lahijan-schema-${process.pid}.d.ts`);

const npx = process.platform === "win32" ? "npx.cmd" : "npx";
// Match the version pinned in api/package.json so the regenerated file is
// byte-identical to what the backend pipeline produces.
const OPENAPI_TS_VERSION = "7.13.0";
const args = ["-y", `openapi-typescript@${OPENAPI_TS_VERSION}`, spec, "-o", tmp];
// On Windows we MUST use shell:true to invoke .cmd files; the DEP0190
// warning is about arg-escaping which we don't care about here (we
// control every arg).
const result = spawnSync(npx, args, {
  stdio: "inherit",
  shell: process.platform === "win32",
});

if (result.error) {
  console.error("❌ Failed to spawn openapi-typescript:", result.error);
  process.exit(1);
}
if (result.status !== 0) {
  console.error(`❌ openapi-typescript exited with status ${result.status}.`);
  rmSync(tmp, { force: true });
  process.exit(result.status ?? 1);
}

const regenerated = readFileSync(tmp, "utf8");
const committedSrc = readFileSync(committed, "utf8");

// Allow the auto-generated header comment to differ (timestamps, version):
// compare only the actual type definitions.
const stripHeader = (s) => s.replace(/^\/\*\*[\s\S]*?\*\//, "").trim();

if (stripHeader(regenerated) !== stripHeader(committedSrc)) {
  console.error("❌ api/gen/ts/schema.d.ts is out of sync with api/openapi.yaml.");
  console.error("   Run `make openapi-gen` from the repo root and commit the result.");
  rmSync(tmp, { force: true });
  process.exit(1);
}

rmSync(tmp, { force: true });
console.log("✅ OpenAPI schema in sync with api/openapi.yaml.");
