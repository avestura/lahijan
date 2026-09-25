import { useLocation } from "react-router-dom";

import { DOCS_PAGES } from "@/docs/nav";

/** The docs slug for the current location, or null outside the docs. */
export function useDocsSlug(): string | null {
  const { pathname } = useLocation();
  const m = /\/docs(?:\/(.*?))?\/?$/.exec(pathname);
  if (!m) return null;
  const slug = m[1] ?? "";
  return DOCS_PAGES.some((p) => p.slug === slug) ? slug : null;
}
