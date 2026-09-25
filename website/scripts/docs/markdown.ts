/**
 * Build-time Markdown compiler for the documentation pages.
 *
 * Runs inside Vite (see scripts/docs/plugin.ts), never in the browser: each
 * `src/content/docs/**.md` file becomes a module holding prerendered HTML in
 * Boxy markup, so the client ships no Markdown parser.
 *
 * Output follows the Boxy content specs (references/components-content.md):
 *   - prose wrapper classes come from the page component (`bx-prose`);
 *   - fenced code becomes a `.bx-codeblock` with a header strip (file name,
 *     language, COPY) and one `<span class="ln">` per line;
 *   - `console` fences render as terminal blocks: `$ ` lines are commands,
 *     the rest is output;
 *   - GitHub-style alerts (`> [!NOTE]`) become `.bx-callout` blocks;
 *   - tables are wrapped for horizontal scroll; lists get `.bx-list`;
 *   - h2/h3 get stable ids and are collected for the on-this-page list.
 */
import MarkdownIt from "markdown-it";
import type Token from "markdown-it/lib/token.mjs";
import { parse as parseYaml } from "yaml";

import { highlight } from "./highlight";

export interface DocHeading {
  id: string;
  text: string;
  depth: 2 | 3;
}

export interface CompiledDoc {
  title: string;
  description: string;
  html: string;
  headings: DocHeading[];
  /** Plain text of the body, trimmed, for the search index. */
  text: string;
}

const CALLOUTS: Record<string, { kind: string; label: string }> = {
  NOTE: { kind: "info", label: "Note" },
  TIP: { kind: "success", label: "Tip" },
  IMPORTANT: { kind: "info", label: "Important" },
  WARNING: { kind: "warning", label: "Warning" },
  CAUTION: { kind: "danger", label: "Caution" },
};

export function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

/** Lowercase, ASCII-dash slug for heading ids. */
export function slugify(text: string): string {
  return (
    text
      .toLowerCase()
      .replace(/`/g, "")
      .replace(/[^a-z0-9\s-]/g, "")
      .trim()
      .replace(/\s+/g, "-")
      .replace(/-+/g, "-") || "section"
  );
}

/** Splits `---\nyaml\n---\nbody` front matter off a Markdown source. */
export function splitFrontMatter(src: string): { data: Record<string, unknown>; body: string } {
  const normalized = src.replace(/\r\n/g, "\n");
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(normalized);
  if (!m) return { data: {}, body: normalized };
  const raw = m[1] ?? "";
  let data: Record<string, unknown>;
  try {
    data = (parseYaml(raw) ?? {}) as Record<string, unknown>;
  } catch {
    // Front matter is flat `key: value` lines; an unquoted value that itself
    // contains ": " is not valid YAML, so fall back to splitting each line at
    // its first colon.
    data = {};
    for (const line of raw.split("\n")) {
      const at = line.indexOf(":");
      if (at > 0)
        data[line.slice(0, at).trim()] = line
          .slice(at + 1)
          .trim()
          .replace(/^"(.*)"$/, "$1");
    }
  }
  return { data, body: normalized.slice(m[0].length) };
}

/** Parses a fence info string: `yaml title="docker-compose.yml"`. */
function parseFenceInfo(info: string): { lang: string; title: string } {
  const lang = (info.trim().split(/\s+/)[0] ?? "").toLowerCase();
  const title = /title="([^"]*)"/.exec(info)?.[1] ?? "";
  return { lang, title };
}

const TERMINAL_LANGS = new Set(["console", "shell-session", "terminal"]);

function renderCodeBlock(content: string, info: string): string {
  const { lang, title } = parseFenceInfo(info);
  const lines = content.replace(/\n$/, "").split("\n");
  const terminal = TERMINAL_LANGS.has(lang);
  const body = lines
    .map((line) => {
      if (terminal) {
        if (line.startsWith("$ ")) {
          return `<span class="ln ln--cmd">${highlight(line.slice(2), "sh")}</span>`;
        }
        return `<span class="ln ln--out">${escapeHtml(line) || " "}</span>`;
      }
      return `<span class="ln">${highlight(line, lang) || " "}</span>`;
    })
    .join("");
  const label = terminal ? "terminal" : lang || "text";
  const head = [
    title ? `<span class="bx-label bx-codeblock__file">${escapeHtml(title)}</span>` : "",
    `<span class="bx-label">${escapeHtml(label)}</span>`,
    `<span class="bx-codeblock__spacer"></span>`,
    `<button type="button" class="bx-codeblock__btn" data-copy>copy</button>`,
  ].join("");
  const cls = `bx-codeblock${terminal ? " bx-codeblock--term" : ""}`;
  return `<div class="${cls}"><div class="bx-codeblock__head">${head}</div><pre><code>${body}</code></pre></div>\n`;
}

/** Turns `> [!NOTE]` blockquotes into callouts (GitHub alert syntax). */
function calloutRule(md: MarkdownIt): void {
  md.core.ruler.after("inline", "bx-callouts", (state) => {
    const tokens = state.tokens;
    for (let i = 0; i < tokens.length; i++) {
      const open = tokens[i];
      if (open?.type !== "blockquote_open") continue;
      const inline = tokens[i + 2];
      if (inline?.type !== "inline") continue;
      const m = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/.exec(inline.content);
      if (!m) continue;
      const spec = CALLOUTS[m[1] as string];
      if (!spec) continue;
      open.meta = { callout: spec };
      // Drop the marker (and the line break after it) from the first paragraph.
      const children = inline.children ?? [];
      const first = children[0];
      if (first && first.type === "text") {
        first.content = first.content.replace(/^\[!\w+\]\s*/, "");
        if (!first.content && children[1]?.type === "softbreak") children.splice(0, 2);
        else if (!first.content) children.splice(0, 1);
      }
      // Find the matching close and mark it.
      let depth = 0;
      for (let j = i; j < tokens.length; j++) {
        const t = tokens[j] as Token;
        if (t.type === "blockquote_open") depth++;
        if (t.type === "blockquote_close" && --depth === 0) {
          t.meta = { callout: spec };
          break;
        }
      }
    }
  });
}

export function createMarkdown(base: string): MarkdownIt {
  const md = new MarkdownIt({ html: false, linkify: false, typographer: false });
  calloutRule(md);

  md.renderer.rules.fence = (tokens, idx) => {
    const t = tokens[idx] as Token;
    return renderCodeBlock(t.content, t.info);
  };
  md.renderer.rules.code_block = (tokens, idx) =>
    renderCodeBlock((tokens[idx] as Token).content, "");

  md.renderer.rules.blockquote_open = (tokens, idx) => {
    const spec = (tokens[idx] as Token).meta?.callout as
      | { kind: string; label: string }
      | undefined;
    if (!spec) return '<blockquote class="bx-quote">\n';
    return `<div class="bx-callout bx-callout--${spec.kind}" role="note"><div class="bx-callout__body"><p class="bx-label bx-callout__label">${spec.label}</p>\n`;
  };
  md.renderer.rules.blockquote_close = (tokens, idx) =>
    (tokens[idx] as Token).meta?.callout ? "</div></div>\n" : "</blockquote>\n";

  md.renderer.rules.table_open = () => '<div class="bx-table-wrap"><table class="bx-table">\n';
  md.renderer.rules.table_close = () => "</table></div>\n";

  md.renderer.rules.bullet_list_open = () => '<ul class="bx-list">\n';
  md.renderer.rules.ordered_list_open = (tokens, idx) => {
    const start = (tokens[idx] as Token).attrGet("start");
    return `<ol class="bx-list"${start ? ` start="${start}"` : ""}>\n`;
  };

  // Internal links are written as /docs/... and resolved against the site
  // base (GitHub Pages serves the site under /<repo>/).
  const defaultLinkOpen =
    md.renderer.rules.link_open ??
    ((tokens, idx, opts, _env, self) => self.renderToken(tokens, idx, opts));
  md.renderer.rules.link_open = (tokens, idx, opts, env, self) => {
    const t = tokens[idx] as Token;
    const href = t.attrGet("href") ?? "";
    if (href.startsWith("/")) {
      t.attrSet("href", `${base.replace(/\/$/, "")}${href}`);
      t.attrSet("data-internal", "");
    } else if (/^https?:/.test(href)) {
      t.attrSet("rel", "noopener");
    }
    return defaultLinkOpen(tokens, idx, opts, env, self);
  };

  return md;
}

/** Compiles one docs page. `base` is the Vite base (e.g. "/" or "/lahijan/"). */
export function compileDoc(src: string, base: string, file = "doc"): CompiledDoc {
  const { data, body } = splitFrontMatter(src);
  const title = typeof data.title === "string" ? data.title : "";
  const description = typeof data.description === "string" ? data.description : "";
  if (!title) throw new Error(`${file}: front matter needs a "title"`);

  const md = createMarkdown(base);
  const env = {};
  const tokens = md.parse(body, env);

  // Heading ids + outline.
  const headings: DocHeading[] = [];
  const used = new Map<string, number>();
  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i] as Token;
    if (t.type !== "heading_open") continue;
    const level = Number(t.tag.slice(1));
    const inline = tokens[i + 1] as Token;
    const text = (inline.children ?? []).map((c) => c.content).join("");
    let id = slugify(text);
    const n = used.get(id) ?? 0;
    used.set(id, n + 1);
    if (n > 0) id = `${id}-${n}`;
    t.attrSet("id", id);
    if (level === 2 || level === 3) headings.push({ id, text, depth: level });
    if (level === 1)
      throw new Error(`${file}: use "title" in front matter instead of a level-1 heading`);
  }

  const html = md.renderer.render(tokens, md.options, env);
  const text = body
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/[#>*_`|[\]()-]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  return { title, description, html, headings, text };
}
