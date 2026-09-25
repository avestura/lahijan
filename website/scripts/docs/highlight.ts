/**
 * Minimal line highlighter for docs code blocks.
 *
 * Boxy keeps syntax colour near-monochrome (components-content.md): keywords
 * take weight, strings take the one accent, numbers the success ink, comments
 * the subtle ink. That needs a handful of token classes, not a grammar
 * engine, so each language is a short ordered list of regex rules applied
 * left to right over one line.
 */
type Rule = [RegExp, string];

const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

const STR_D = /"(?:[^"\\]|\\.)*"/y;
const STR_S = /'(?:[^'\\]|\\.)*'/y;
const STR_B = /`(?:[^`\\]|\\.)*`/y;
const NUM = /\b\d+(?:\.\d+)?\b/y;

const kw = (words: string) => new RegExp(`\\b(?:${words.split(" ").join("|")})\\b`, "y");

const RULES: Record<string, Rule[]> = {
  yaml: [
    [/#.*/y, "tk-c"],
    [/^(\s*-?\s*)[\w.$/-]+(?=:(\s|$))/y, "tk-k"],
    [STR_D, "tk-s"],
    [STR_S, "tk-s"],
    [/\b(?:true|false|null)\b/y, "tk-n"],
    [NUM, "tk-n"],
  ],
  sh: [
    [/#.*/y, "tk-c"],
    [STR_D, "tk-s"],
    [STR_S, "tk-s"],
    [/\$\{?[A-Za-z_][A-Za-z0-9_]*\}?/y, "tk-k"],
    [/(?<=\s|^)--?[A-Za-z][\w.-]*/y, "tk-f"],
  ],
  ini: [
    [/[#;].*/y, "tk-c"],
    [/^\s*[\w.-]+(?=\s*=)/y, "tk-k"],
    [STR_D, "tk-s"],
    [NUM, "tk-n"],
  ],
  json: [
    [/"(?:[^"\\]|\\.)*"(?=\s*:)/y, "tk-k"],
    [STR_D, "tk-s"],
    [/\b(?:true|false|null)\b/y, "tk-n"],
    [/-?\b\d+(?:\.\d+)?\b/y, "tk-n"],
  ],
  go: [
    [/\/\/.*/y, "tk-c"],
    [STR_D, "tk-s"],
    [STR_B, "tk-s"],
    [
      kw(
        "package import func return if else for range var const type struct interface map chan go defer switch case default break continue nil true false",
      ),
      "tk-k",
    ],
    [NUM, "tk-n"],
  ],
  ts: [
    [/\/\/.*/y, "tk-c"],
    [STR_D, "tk-s"],
    [STR_S, "tk-s"],
    [STR_B, "tk-s"],
    [
      kw(
        "import export from const let var function return if else for of in new await async type interface extends class true false null undefined",
      ),
      "tk-k",
    ],
    [NUM, "tk-n"],
  ],
  http: [
    [/^(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\b/y, "tk-k"],
    [/^[\w-]+(?=:\s)/y, "tk-k"],
    [STR_D, "tk-s"],
    [NUM, "tk-n"],
  ],
};

const ALIASES: Record<string, string> = {
  yml: "yaml",
  bash: "sh",
  shell: "sh",
  zsh: "sh",
  env: "ini",
  dotenv: "ini",
  toml: "ini",
  conf: "ini",
  javascript: "ts",
  js: "ts",
  typescript: "ts",
  tsx: "ts",
  golang: "go",
};

/** Highlights one line; unknown languages come back escaped only. */
export function highlight(line: string, lang: string): string {
  const rules = RULES[ALIASES[lang] ?? lang];
  if (!rules) return esc(line);
  let out = "";
  let plain = "";
  let i = 0;
  outer: while (i < line.length) {
    for (const [re, cls] of rules) {
      // `^` rules only match at the start of the line.
      if (re.source.startsWith("^") && i !== 0) continue;
      re.lastIndex = i;
      const m = re.exec(line);
      if (m && m.index === i && m[0].length > 0) {
        // A rule may capture leading indentation; keep it unstyled.
        const lead = /^\s*-?\s*/.exec(m[0])?.[0] ?? "";
        const leadKeep = cls === "tk-k" && re.source.startsWith("^(") ? lead : "";
        out +=
          esc(plain) +
          esc(leadKeep) +
          `<span class="${cls}">${esc(m[0].slice(leadKeep.length))}</span>`;
        plain = "";
        i += m[0].length;
        continue outer;
      }
    }
    plain += line[i];
    i++;
  }
  return out + esc(plain);
}
