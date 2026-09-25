// Generates the configuration reference page from the server's default
// config file plus curated operator-facing descriptions.
//
//   node scripts/docs/config-reference.mjs          write the page
//   node scripts/docs/config-reference.mjs --check  exit 1 if it is stale
//
// Inputs:
//   ../internal/app/lahijan/conf/.lahijan.conf.default.yaml  keys, defaults, types
//   scripts/docs/config-descriptions.yaml                    descriptions
// Output:
//   src/content/docs/reference/configuration.md
//
// The default YAML's own comments are written for developers, so they are
// not published; every leaf key needs a curated description instead (the
// generator fails when one is missing or when a description is stale).
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isMap, isScalar, isSeq, parse, parseDocument } from "yaml";

const here = path.dirname(fileURLToPath(import.meta.url));
const website = path.resolve(here, "../..");
export const CONFIG_FILE = path.resolve(
  website,
  "../internal/app/lahijan/conf/.lahijan.conf.default.yaml",
);
export const DESCRIPTIONS_FILE = path.resolve(here, "config-descriptions.yaml");
export const OUTPUT_FILE = path.resolve(website, "src/content/docs/reference/configuration.md");

// Defaults the server computes at startup (internal/app/lahijan/program).
const COMPUTED = {
  "http.server.concurrency": "262144",
  "http.server.bodylimit": "4194304",
};

/** Walks the default config and returns leaf settings in file order. */
export function collectSettings(configYaml) {
  const doc = parseDocument(configYaml);
  const out = [];
  const walk = (node, prefix) => {
    for (const pair of node.items) {
      const key = prefix ? `${prefix}.${pair.key.value}` : String(pair.key.value);
      const value = pair.value;
      if (isMap(value)) {
        walk(value, key);
      } else if (isSeq(value)) {
        const items = value.items.map((i) => (isScalar(i) ? i.value : String(i)));
        // An empty list carries its element type as a trailing `# !!str` comment.
        const tag = /!!(\w+)/.exec(value.comment ?? "")?.[1];
        const elem = items.length ? typeof items[0] : tag === "int" ? "number" : "string";
        out.push({ key, kind: "list", elem, value: items });
      } else if (isScalar(value)) {
        out.push({ key, kind: typeof value.value, value: value.value });
      }
    }
  };
  walk(doc.contents, "");
  return out;
}

export const envName = (key) => `LAHIJAN_${key.replace(/\./g, "_").toUpperCase()}`;

function formatDefault(s) {
  if (COMPUTED[s.key]) return `\`${COMPUTED[s.key]}\` (computed)`;
  if (s.kind === "list")
    return s.value.length ? s.value.map((v) => `\`${v}\``).join(", ") : "empty list";
  if (s.kind === "string") return s.value === "" ? "empty" : `\`${s.value}\``;
  return `\`${String(s.value)}\``;
}

function typeLabel(s) {
  if (s.key in COMPUTED) return "integer";
  if (s.kind === "list") return s.elem === "number" ? "list of integers" : "list of strings";
  if (s.kind === "number") return Number.isInteger(s.value) ? "integer" : "number";
  if (s.kind === "boolean") return "boolean";
  return "string";
}

const cell = (text) =>
  String(text)
    .replace(/\|/g, "\\|")
    .replace(/\s*\n\s*/g, " ")
    .trim();

/** Checks descriptions against the settings; returns a list of problems. */
export function validateDescriptions(settings, desc) {
  const problems = [];
  const keys = new Set(settings.map((s) => s.key));
  for (const s of settings) if (!desc.keys?.[s.key]) problems.push(`missing description: ${s.key}`);
  for (const k of Object.keys(desc.keys ?? {}))
    if (!keys.has(k)) problems.push(`stale description: ${k}`);
  const tops = new Set(
    settings.map((s) => (s.key.includes(".") ? s.key.split(".")[0] : "general")),
  );
  for (const t of tops) if (!desc.sections?.[t]) problems.push(`missing section: ${t}`);
  return problems;
}

export function renderConfigReference(configYaml, descriptionsYaml) {
  const settings = collectSettings(configYaml);
  const desc = parse(descriptionsYaml);
  const problems = validateDescriptions(settings, desc);
  if (problems.length) throw new Error(`config-descriptions.yaml:\n  ${problems.join("\n  ")}`);

  const lines = [];
  lines.push(
    "---",
    "title: Configuration reference",
    "description: Every Lahijan server setting with its default, environment variable and command-line flag.",
    "---",
    "",
    "The Lahijan server reads its settings from four layers. Each layer overrides the ones below it:",
    "",
    "1. **Command-line flags**, one per setting: `--http.server.port=8080`.",
    "2. **Environment variables** named `LAHIJAN_` plus the setting path in capitals with dots replaced by underscores: `LAHIJAN_HTTP_SERVER_PORT=8080`.",
    "3. **A config file** named `.lahijan.conf.default.yaml`, looked up in `/etc/lahijan`, then `$HOME/.lahijan`, then the working directory. The first one found is merged over the built-in defaults. It only needs the settings you change.",
    "4. **Built-in defaults**, shown in the tables below.",
    "",
    "Setting names are case-sensitive in flags (`--database.maxConns`) and case-insensitive in the environment (`LAHIJAN_DATABASE_MAXCONNS`).",
    "",
    '```yaml title="/etc/lahijan/.lahijan.conf.default.yaml"',
    "http:",
    "  server:",
    "    port: 8080",
    "database:",
    "  host: db.internal",
    "  sslmode: verify-full",
    "```",
    "",
    "> [!NOTE]",
    "> List settings take comma-separated values as flags (`--http.server.cors.allowOrigins=https://a.example.com,https://b.example.com`) and space-separated values as environment variables.",
    "",
    "> [!WARNING]",
    "> Put secrets such as `database.password`, `auth.signing.key` and provider API keys in environment variables or a file readable only by the server, never in a file you commit. In the production compose stack they come from `deployments/.env.prod`. See [Environment file](/docs/operations/environment).",
    "",
  );

  // Group: top-level scalars form "general"; everything else by top-level key.
  const order = [];
  const byTop = new Map();
  for (const s of settings) {
    const top = s.key.includes(".") ? s.key.split(".")[0] : "general";
    if (!byTop.has(top)) {
      byTop.set(top, []);
      order.push(top);
    }
    byTop.get(top).push(s);
  }
  if (order.includes("general")) {
    order.splice(order.indexOf("general"), 1);
    order.unshift("general");
  }

  const table = (rows) => {
    const out = [
      "| Setting | Environment variable | Type | Default | Description |",
      "| --- | --- | --- | --- | --- |",
    ];
    for (const s of rows) {
      out.push(
        `| \`${s.key}\` | \`${envName(s.key)}\` | ${typeLabel(s)} | ${cell(formatDefault(s))} | ${cell(desc.keys[s.key])} |`,
      );
    }
    return out;
  };

  for (const top of order) {
    const section = desc.sections[top];
    lines.push(`## ${section.title}`, "");
    if (section.intro) lines.push(cell(section.intro), "");
    const rows = byTop.get(top);
    // Direct leaves first, then one subsection per second-level group.
    const direct = rows.filter((s) => top === "general" || s.key.split(".").length === 2);
    const groups = new Map();
    for (const s of rows) {
      if (direct.includes(s)) continue;
      const g = s.key.split(".").slice(0, 2).join(".");
      if (!groups.has(g)) groups.set(g, []);
      groups.get(g).push(s);
    }
    if (direct.length) lines.push(...table(direct), "");
    for (const [g, groupRows] of groups) {
      lines.push(`### \`${g}\``, "");
      const intro = desc.groups?.[g];
      if (intro) lines.push(cell(intro), "");
      lines.push(...table(groupRows), "");
    }
  }
  return lines.join("\n");
}

function main() {
  const out = renderConfigReference(
    readFileSync(CONFIG_FILE, "utf8"),
    readFileSync(DESCRIPTIONS_FILE, "utf8"),
  );
  if (process.argv.includes("--check")) {
    let current = "";
    try {
      current = readFileSync(OUTPUT_FILE, "utf8");
    } catch {
      /* missing counts as stale */
    }
    if (current.replace(/\r\n/g, "\n") !== out) {
      console.error("configuration.md is out of date: run `npm run docs:config`.");
      process.exit(1);
    }
    console.log("configuration.md is up to date.");
    return;
  }
  mkdirSync(path.dirname(OUTPUT_FILE), { recursive: true });
  writeFileSync(OUTPUT_FILE, out);
  console.log(`wrote ${path.relative(website, OUTPUT_FILE)}`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
