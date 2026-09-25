/**
 * Docs compiler tests (scripts/docs/markdown.ts): the Boxy markup each
 * Markdown construct turns into.
 */
import { describe, expect, it } from "vitest";

import { compileDoc, slugify } from "../../scripts/docs/markdown";

const page = (body: string) => `---\ntitle: Test page\ndescription: A page.\n---\n\n${body}`;

describe("compileDoc", () => {
  it("reads front matter and requires a title", () => {
    const doc = compileDoc(page("Hello."), "/");
    expect(doc.title).toBe("Test page");
    expect(doc.description).toBe("A page.");
    expect(() => compileDoc("No front matter.", "/")).toThrow(/title/);
  });

  it("rejects level-1 headings (the title is rendered by the page)", () => {
    expect(() => compileDoc(page("# Duplicate title"), "/")).toThrow(/level-1/);
  });

  it("gives h2/h3 unique ids and collects the outline", () => {
    const doc = compileDoc(page("## Set up\n\n### Options\n\n## Set up"), "/");
    expect(doc.headings).toEqual([
      { id: "set-up", text: "Set up", depth: 2 },
      { id: "options", text: "Options", depth: 3 },
      { id: "set-up-1", text: "Set up", depth: 2 },
    ]);
    expect(doc.html).toContain('<h2 id="set-up">');
  });

  it("renders fenced code as a Boxy code block with a file name and copy button", () => {
    const doc = compileDoc(page('```yaml title="app.yaml"\nport: 3000 # listen\n```'), "/");
    expect(doc.html).toContain('class="bx-codeblock"');
    expect(doc.html).toContain("app.yaml");
    expect(doc.html).toContain("data-copy");
    expect(doc.html).toContain('<span class="tk-k">port</span>');
    expect(doc.html).toContain('<span class="tk-c"># listen</span>');
  });

  it("renders console fences as terminal blocks", () => {
    const doc = compileDoc(page("```console\n$ make run\nlistening on :3000\n```"), "/");
    expect(doc.html).toContain("bx-codeblock--term");
    expect(doc.html).toContain('class="ln ln--cmd"');
    expect(doc.html).toContain('<span class="ln ln--out">listening on :3000</span>');
  });

  it("escapes HTML inside code", () => {
    const doc = compileDoc(page("```text\n<script>alert(1)</script>\n```"), "/");
    expect(doc.html).not.toContain("<script>");
    expect(doc.html).toContain("&lt;script&gt;");
  });

  it("turns GitHub alerts into callouts", () => {
    const doc = compileDoc(page("> [!WARNING]\n> This deletes data."), "/");
    expect(doc.html).toContain("bx-callout bx-callout--warning");
    expect(doc.html).toContain(">Warning</p>");
    expect(doc.html).toContain("This deletes data.");
    expect(doc.html).not.toContain("[!WARNING]");
  });

  it("keeps plain blockquotes as quotes", () => {
    expect(compileDoc(page("> Just a quote."), "/").html).toContain('class="bx-quote"');
  });

  it("wraps tables and styles lists", () => {
    const doc = compileDoc(
      page("| A | B |\n| - | - |\n| 1 | 2 |\n\n- one\n- two\n\n1. first"),
      "/",
    );
    expect(doc.html).toContain('<div class="bx-table-wrap"><table class="bx-table">');
    expect(doc.html).toContain('<ul class="bx-list">');
    expect(doc.html).toContain('<ol class="bx-list">');
  });

  it("prefixes internal links with the site base and marks them", () => {
    const doc = compileDoc(
      page("[Profiles](/docs/compute/profiles) and [site](https://example.com)"),
      "/lahijan/",
    );
    expect(doc.html).toContain('href="/lahijan/docs/compute/profiles" data-internal=""');
    expect(doc.html).toContain('href="https://example.com" rel="noopener"');
  });

  it("does not render raw HTML", () => {
    expect(compileDoc(page("<b>bold</b>"), "/").html).toContain("&lt;b&gt;");
  });
});

describe("slugify", () => {
  it("makes ASCII dash slugs", () => {
    expect(slugify("Create a `bucket`!")).toBe("create-a-bucket");
    expect(slugify("???")).toBe("section");
  });
});
