/**
 * DocsSearch — the docs search palette (Boxy command palette,
 * references/components-forms.md).
 *
 * 640px, pinned 96px from the top, over a solid scrim. Focus stays in the
 * input; the selected row is aria-activedescendant, moved by the arrow keys
 * and by the pointer (on mousemove, so a list scrolling under a still pointer
 * does not steal the selection). Matches render as an inverted inline block.
 * With an empty query it lists the getting-started pages, never a blank panel.
 *
 * The search data (virtual:docs-index) is its own chunk, fetched the first
 * time the palette opens.
 */
import * as DialogPrimitive from "@radix-ui/react-dialog";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CornerDownLeftIcon,
  FileTextIcon,
  SearchIcon,
} from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { DOCS_PAGES, docsPath } from "@/docs/nav";
import { searchDocs } from "@/docs/search";
import type { IndexEntry, SearchResult as Result } from "@/docs/search";
import { cn } from "@/lib/utils";

function Highlight({ text, query }: { text: string; query: string }) {
  const term = query.trim().split(/\s+/)[0] ?? "";
  const at = term ? text.toLowerCase().indexOf(term.toLowerCase()) : -1;
  if (at < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <mark className="bx-mark">{text.slice(at, at + term.length)}</mark>
      {text.slice(at + term.length)}
    </>
  );
}

const KEY_ESC = "esc";

function KeyHint({ keys, label }: { keys: React.ReactNode; label: string }) {
  return (
    <span className="flex items-center gap-1 font-mono text-2xs uppercase tracking-label text-ink-subtle">
      <kbd className="bx-kbd [&>svg]:size-3">{keys}</kbd>
      {label}
    </span>
  );
}

interface DocsSearchProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function DocsSearch({ open, onOpenChange }: DocsSearchProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const listId = useId();
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState<IndexEntry[] | null>(null);
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLUListElement>(null);

  useEffect(() => {
    if (!open || index) return;
    let cancelled = false;
    void import("virtual:docs-index").then((m) => {
      if (!cancelled) setIndex(m.default);
    });
    return () => {
      cancelled = true;
    };
  }, [open, index]);

  const results: Result[] = useMemo(() => {
    if (!query.trim()) {
      return DOCS_PAGES.slice(0, 6).map((p) => ({ slug: p.slug, title: p.title, context: "" }));
    }
    return index ? searchDocs(index, query) : [];
  }, [index, query]);

  useEffect(() => setActive(0), [query]);
  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${active}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const close = (next: boolean) => {
    if (!next) setQuery("");
    onOpenChange(next);
  };

  const go = (slug: string) => {
    close(false);
    navigate(docsPath(slug));
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((i) => (results.length ? (i + 1) % results.length : 0));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => (results.length ? (i - 1 + results.length) % results.length : 0));
    } else if (e.key === "Enter") {
      const r = results[active];
      if (r) {
        e.preventDefault();
        go(r.slug);
      }
    }
  };

  const optionId = (i: number) => `${listId}-opt-${i}`;
  const loading = Boolean(query.trim()) && !index;

  return (
    <DialogPrimitive.Root open={open} onOpenChange={close}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-scrim data-[state=open]:animate-fade-in" />
        <DialogPrimitive.Content
          className="bx-raised fixed start-1/2 top-24 z-40 flex w-[min(640px,calc(100vw-32px))] -translate-x-1/2 flex-col border border-line-heavy bg-popover text-popover-foreground shadow-3 rtl:translate-x-1/2"
          aria-describedby={undefined}
        >
          <DialogPrimitive.Title className="sr-only">{t("docs.search")}</DialogPrimitive.Title>
          <div className="flex h-12 items-center gap-3 border-b border-line px-4">
            <SearchIcon className="h-4 w-4 shrink-0 text-ink-subtle" aria-hidden="true" />
            <input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onKeyDown}
              placeholder={t("docs.searchPlaceholder")}
              aria-label={t("docs.search")}
              role="combobox"
              aria-expanded="true"
              aria-controls={listId}
              aria-activedescendant={results[active] ? optionId(active) : undefined}
              className="h-full w-full bg-transparent text-md text-ink outline-none placeholder:text-ink-subtle"
              data-testid="docs-search-input"
            />
          </div>
          <p className="flex h-6 items-center border-b border-line-subtle bg-surface-sunken px-4 font-mono text-2xs font-medium uppercase tracking-label text-ink-subtle">
            {query.trim() ? t("docs.results") : t("docs.startHere")}
          </p>
          <ul
            ref={listRef}
            id={listId}
            role="listbox"
            aria-label={t("docs.results")}
            className="max-h-[min(360px,calc(100vh-240px))] overflow-y-auto"
          >
            {results.map((r, i) => (
              <li
                key={r.slug || "home"}
                id={optionId(i)}
                role="option"
                aria-selected={i === active}
                data-index={i}
                onMouseMove={() => setActive(i)}
                onClick={() => go(r.slug)}
                className={cn(
                  "flex h-10 cursor-pointer items-center gap-3 px-4 text-base text-ink transition-[background-color,box-shadow] duration-80 ease-linear",
                  i === active &&
                    "bg-surface-hover shadow-[inset_2px_0_0_0_var(--bx-accent)] rtl:shadow-[inset_-2px_0_0_0_var(--bx-accent)]",
                )}
                data-testid={`docs-search-result-${r.slug || "home"}`}
              >
                <FileTextIcon
                  className={cn("h-4 w-4 shrink-0", i === active ? "text-ink" : "text-ink-subtle")}
                  aria-hidden="true"
                />
                <span className="min-w-0 truncate">
                  <Highlight text={r.title} query={query} />
                  {r.context && (
                    <span className="text-ink-subtle">
                      {" / "}
                      <Highlight text={r.context} query={query} />
                    </span>
                  )}
                </span>
                <span className="ms-auto shrink-0 font-mono text-xs text-ink-subtle" dir="ltr">
                  {docsPath(r.slug)}
                </span>
              </li>
            ))}
          </ul>
          {(loading || (query.trim() && index && results.length === 0)) && (
            <p className="px-4 py-8 font-mono text-xs uppercase tracking-label text-ink-subtle">
              {loading ? t("docs.loading") : t("docs.noResults", { query: query.trim() })}
            </p>
          )}
          <div className="flex h-8 items-center gap-4 border-t border-line bg-surface-sunken px-4">
            <KeyHint
              keys={
                <>
                  <ArrowUpIcon aria-hidden="true" />
                  <ArrowDownIcon aria-hidden="true" />
                </>
              }
              label={t("docs.hintNavigate")}
            />
            <KeyHint
              keys={<CornerDownLeftIcon aria-hidden="true" />}
              label={t("docs.hintSelect")}
            />
            <KeyHint keys={KEY_ESC} label={t("docs.hintClose")} />
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
