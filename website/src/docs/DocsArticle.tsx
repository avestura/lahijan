/**
 * DocsArticle — one documentation page: breadcrumb, title, lede, the
 * compiled prose, a previous/next pager and an "Edit this page" link, with
 * the on-this-page list in its own column (Boxy docs pattern).
 *
 * The body HTML is compiled from Markdown at build time
 * (scripts/docs/markdown.ts). Two behaviours are attached here:
 *   - code block COPY buttons (terminal blocks copy the commands only);
 *   - links to other docs pages navigate in-app instead of reloading.
 */
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { SeoHead } from "@/components/seo/SeoHead";
import { DOCS_PAGES, docsPath, locateDoc } from "@/docs/nav";
import { REPO_URL } from "@/lib/site";

export interface DocContent {
  title: string;
  description: string;
  html: string;
  headings: { id: string; text: string; depth: 2 | 3 }[];
}

interface DocsArticleProps {
  slug: string;
  doc: DocContent;
  /** Extra content under the prose (the docs home adds its section index). */
  children?: React.ReactNode;
}

const BASE = import.meta.env.BASE_URL.replace(/\/$/, "");

/** Tracks which heading is in view for the on-this-page list. */
function useActiveHeading(ids: string[]): string | null {
  const [active, setActive] = useState<string | null>(ids[0] ?? null);
  useEffect(() => {
    if (typeof IntersectionObserver === "undefined" || ids.length === 0) return;
    const visible = new Set<string>();
    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) visible.add(e.target.id);
          else visible.delete(e.target.id);
        }
        const first = ids.find((id) => visible.has(id));
        if (first) setActive(first);
      },
      // 56px sticky header: a heading counts once it passes just below it.
      { rootMargin: "-88px 0px -70% 0px" },
    );
    for (const id of ids) {
      const el = document.getElementById(id);
      if (el) obs.observe(el);
    }
    return () => obs.disconnect();
  }, [ids]);
  return active;
}

export function DocsArticle({ slug, doc, children }: DocsArticleProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const bodyRef = useRef<HTMLDivElement>(null);
  const place = locateDoc(slug);
  const idx = DOCS_PAGES.findIndex((p) => p.slug === slug);
  const prev = idx > 0 ? DOCS_PAGES[idx - 1] : undefined;
  const next = idx >= 0 ? DOCS_PAGES[idx + 1] : undefined;
  const active = useActiveHeading(doc.headings.map((h) => h.id));

  // Copy buttons and in-app links inside the compiled HTML.
  useEffect(() => {
    const root = bodyRef.current;
    if (!root) return;
    const onClick = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      const copy = target.closest<HTMLButtonElement>("[data-copy]");
      if (copy) {
        const block = copy.closest(".bx-codeblock");
        const terminal = block?.classList.contains("bx-codeblock--term");
        const lines = Array.from(
          block?.querySelectorAll<HTMLElement>(terminal ? ".ln--cmd" : ".ln") ?? [],
        );
        const text = lines.map((l) => l.textContent ?? "").join("\n");
        void navigator.clipboard?.writeText(text).then(() => {
          const label = copy.textContent;
          copy.textContent = t("docs.copied");
          window.setTimeout(() => {
            copy.textContent = label;
          }, 1500);
        });
        return;
      }
      const link = target.closest<HTMLAnchorElement>("a[data-internal]");
      if (link && !e.metaKey && !e.ctrlKey && !e.shiftKey && e.button === 0) {
        const href = link.getAttribute("href") ?? "";
        const path = href.startsWith(BASE) ? href.slice(BASE.length) || "/" : href;
        e.preventDefault();
        navigate(path);
      }
    };
    root.addEventListener("click", onClick);
    return () => root.removeEventListener("click", onClick);
  }, [navigate, t]);

  const editUrl = `${REPO_URL}/edit/main/website/src/content/docs/${slug || "index"}.md`;

  return (
    <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_224px]">
      <SeoHead title={doc.title} description={doc.description} path={docsPath(slug)} />
      <article className="min-w-0 px-4 py-10 md:px-12">
        <nav className="bx-breadcrumb" aria-label={t("docs.breadcrumb")}>
          <ol>
            <li>
              {slug ? (
                <Link to={docsPath("")}>{t("docs.home")}</Link>
              ) : (
                <span aria-current="page">{t("docs.home")}</span>
              )}
            </li>
            {place && slug && <li>{place.section.title}</li>}
            {place?.group && <li>{place.group.title}</li>}
            {slug && (
              <li>
                <span aria-current="page">{place?.link.title ?? doc.title}</span>
              </li>
            )}
          </ol>
        </nav>

        <header className="mt-4 max-w-prose">
          <h1 className="font-display text-4xl font-semibold tracking-display text-ink">
            {doc.title}
          </h1>
          {doc.description && <p className="mt-4 text-lg text-ink-muted">{doc.description}</p>}
        </header>

        <div
          ref={bodyRef}
          className="bx-prose mt-8"
          dangerouslySetInnerHTML={{ __html: doc.html }}
        />

        {children}

        <nav
          aria-label={t("docs.pager")}
          className="bx-collapse mt-12 grid-cols-1 border border-line sm:grid-cols-2"
        >
          {prev ? (
            <Link
              to={docsPath(prev.slug)}
              className="flex flex-col gap-1 px-4 py-4 no-underline transition-colors duration-80 ease-linear hover:bg-surface-hover hover:no-underline"
            >
              <span className="bx-label">{t("docs.previous")}</span>
              <span className="text-md font-medium text-ink">{prev.title}</span>
            </Link>
          ) : (
            <span className="hidden sm:block" />
          )}
          {next ? (
            <Link
              to={docsPath(next.slug)}
              className="flex flex-col items-end gap-1 px-4 py-4 text-end no-underline transition-colors duration-80 ease-linear hover:bg-surface-hover hover:no-underline"
            >
              <span className="bx-label">{t("docs.next")}</span>
              <span className="text-md font-medium text-ink">{next.title}</span>
            </Link>
          ) : (
            <span className="hidden sm:block" />
          )}
        </nav>

        <p className="mt-6 text-sm">
          <a
            href={editUrl}
            className="text-ink-subtle underline decoration-line-strong underline-offset-4 hover:text-ink"
          >
            {t("docs.editPage")}
          </a>
        </p>
      </article>

      <aside className="hidden border-s border-line xl:block">
        {doc.headings.length > 0 && (
          <div className="sticky top-14 px-4 py-10">
            <p className="bx-label mb-3">{t("docs.onThisPage")}</p>
            <nav className="bx-toc" aria-label={t("docs.onThisPage")}>
              {doc.headings.map((h) => (
                <a
                  key={h.id}
                  href={`#${h.id}`}
                  className={h.depth === 3 ? "sub" : undefined}
                  aria-current={active === h.id ? "true" : undefined}
                >
                  {h.text}
                </a>
              ))}
            </nav>
          </div>
        )}
      </aside>
    </div>
  );
}
