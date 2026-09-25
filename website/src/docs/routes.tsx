/**
 * Documentation routes: one prerendered route per page in nav.ts, each
 * loading its compiled Markdown module on demand (a separate chunk per page).
 */
import type { RouteObject } from "react-router-dom";

import { DocsArticle } from "@/docs/DocsArticle";
import type { DocContent } from "@/docs/DocsArticle";
import { DocsHome } from "@/docs/DocsHome";
import { DocsLayout } from "@/docs/DocsLayout";
import { DOCS_PAGES } from "@/docs/nav";

// README.md in the content folder is the writing guide, not a page.
const modules = import.meta.glob<{ default: DocContent }>([
  "../content/docs/**/*.md",
  "!../content/docs/README.md",
]);

function loader(slug: string) {
  const load = modules[`../content/docs/${slug || "index"}.md`];
  if (!load) throw new Error(`docs page missing: src/content/docs/${slug || "index"}.md`);
  return load;
}

export const docsRoute: RouteObject = {
  path: "docs",
  element: <DocsLayout />,
  children: DOCS_PAGES.map((page): RouteObject => {
    const load = loader(page.slug);
    if (!page.slug) {
      return {
        index: true,
        lazy: async () => {
          const doc = (await load()).default;
          return { Component: () => <DocsHome doc={doc} /> };
        },
      };
    }
    return {
      path: page.slug,
      lazy: async () => {
        const doc = (await load()).default;
        return { Component: () => <DocsArticle key={page.slug} slug={page.slug} doc={doc} /> };
      },
    };
  }),
};

/** Every docs route path, for prerendering and the sitemap. */
export const DOCS_STATIC_PATHS: string[] = DOCS_PAGES.map((p) =>
  p.slug ? `/docs/${p.slug}` : "/docs",
);
