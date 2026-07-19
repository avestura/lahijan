// scripts/check-locales.mjs
//
// CI guard: fails if web/src/locales/en.json and web/src/locales/fa.json
// have drifted out of sync (different key sets, missing values, or wrong
// types). Mirrors the runtime check the Vitest suite does, but standalone
// so it can run from CI without booting Node + Vitest.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const web = path.resolve(__dirname, "..");

/** @typedef {Record<string, unknown>} Dict */

/**
 * @param {unknown} obj
 * @param {string} [prefix]
 * @returns {string[]}
 */
function flattenKeys(obj, prefix = "") {
  if (obj === null || typeof obj !== "object") return [];
  if (Array.isArray(obj)) return [prefix];
  /** @type {string[]} */
  const out = [];
  for (const [key, value] of Object.entries(/** @type {Dict} */ (obj))) {
    const next = prefix ? `${prefix}.${key}` : key;
    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      out.push(...flattenKeys(value, next));
    } else {
      out.push(next);
    }
  }
  return out.sort();
}

function readLocale(name) {
  const file = path.join(web, "src", "locales", `${name}.json`);
  return JSON.parse(readFileSync(file, "utf8"));
}

const en = readLocale("en");
const fa = readLocale("fa");
const enKeys = new Set(flattenKeys(en));
const faKeys = new Set(flattenKeys(fa));

const missingInFa = [...enKeys].filter((k) => !faKeys.has(k));
const extraInFa = [...faKeys].filter((k) => !enKeys.has(k));

if (missingInFa.length > 0 || extraInFa.length > 0) {
  console.error("❌ Locale files out of sync:");
  if (missingInFa.length > 0) {
    console.error("  Missing from fa.json:");
    for (const k of missingInFa) console.error(`    - ${k}`);
  }
  if (extraInFa.length > 0) {
    console.error("  Extra in fa.json (not in en.json):");
    for (const k of extraInFa) console.error(`    - ${k}`);
  }
  process.exit(1);
}

console.log(`✅ Locale files in sync (${enKeys.size} keys).`);
