/**
 * DocsHome — the documentation root (/docs): the index.md introduction plus
 * an index of every section, as a collapsed auto grid (boxy.css
 * `.bx-auto-grid.bx-collapse`: cells draw their own end and bottom rules, so
 * an unfilled last row never shows the line colour through empty tracks).
 */
import { Link } from "react-router-dom";

import { DocsArticle } from "@/docs/DocsArticle";
import type { DocContent } from "@/docs/DocsArticle";
import { DOCS_NAV, docsPath, isGroup } from "@/docs/nav";

export function DocsHome({ doc }: { doc: DocContent }) {
  return (
    <DocsArticle slug="" doc={doc}>
      <div className="bx-auto-grid bx-collapse mt-12 [--bx-min:260px]" data-testid="docs-sections">
        {DOCS_NAV.filter((s) => s.title !== "Get started").map((section) => (
          <section key={section.title} className="px-4 py-6">
            <h2 className="bx-label">{section.title}</h2>
            <ul className="mt-4 grid gap-2">
              {section.items.map((item) => {
                const link = isGroup(item) ? item.items[0] : item;
                if (!link) return null;
                return (
                  <li key={isGroup(item) ? item.title : link.slug}>
                    <Link
                      to={docsPath(link.slug)}
                      className="text-base text-ink underline decoration-line-strong underline-offset-4 hover:decoration-current"
                    >
                      {isGroup(item) ? item.title : link.title}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </section>
        ))}
      </div>
    </DocsArticle>
  );
}
