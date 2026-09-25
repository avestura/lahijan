/** Client-side docs search over the virtual:docs-index data. */
export interface IndexEntry {
  slug: string;
  title: string;
  description: string;
  headings: string[];
  text: string;
}

export interface SearchResult {
  slug: string;
  title: string;
  /** The heading or snippet that matched, when it was not the title. */
  context: string;
}

const MAX_RESULTS = 12;

/** Scores and ranks pages for a query; exported for tests. */
export function searchDocs(index: IndexEntry[], query: string): SearchResult[] {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const terms = q.split(/\s+/);
  const scored: { r: SearchResult; score: number }[] = [];
  for (const entry of index) {
    const title = entry.title.toLowerCase();
    let score = 0;
    let context = "";
    for (const term of terms) {
      if (title.includes(term)) score += title.startsWith(term) ? 12 : 8;
      const heading = entry.headings.find((h) => h.toLowerCase().includes(term));
      if (heading) {
        score += 4;
        context ||= heading;
      }
      if (entry.description.toLowerCase().includes(term)) score += 2;
      if (entry.text.toLowerCase().includes(term)) score += 1;
      else if (!title.includes(term) && !heading) {
        score = 0; // every term must appear somewhere on the page
        break;
      }
    }
    if (score > 0) scored.push({ r: { slug: entry.slug, title: entry.title, context }, score });
  }
  return scored
    .sort((a, b) => b.score - a.score || a.r.title.localeCompare(b.r.title))
    .slice(0, MAX_RESULTS)
    .map((s) => s.r);
}
