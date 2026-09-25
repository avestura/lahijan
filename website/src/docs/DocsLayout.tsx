/**
 * DocsLayout — the documentation shell (Boxy docs layout,
 * references/components-marketing.md):
 *
 *   | sidebar 256px | article (prose 68ch) | on-this-page 224px |
 *
 * separated by 1px rules, no shadows. The sidebar and the on-this-page list
 * are sticky under the site header. Below `lg` the tree moves into a drawer
 * opened from a toolbar that also holds the search trigger; the
 * on-this-page list hides below `xl`.
 *
 * Docs use the industrial density of the product rather than the editorial
 * rhythm of the marketing pages, so the shell sets data-mode="industrial".
 */
import { MenuIcon, SearchIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { Outlet } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { DocsSearch } from "@/docs/DocsSearch";
import { DocsSidebar } from "@/docs/DocsSidebar";
import { useDocsSlug } from "@/docs/useDocsSlug";

// Keyboard hint: a platform symbol, not translatable copy.
const KBD_HINT = "Ctrl K";

export function DocsLayout() {
  const { t, i18n } = useTranslation();
  const slug = useDocsSlug();
  const [searchOpen, setSearchOpen] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  // The prerendered HTML is English; decide on the notice after hydration so
  // server and client markup match.
  const [translatedUi, setTranslatedUi] = useState(false);
  useEffect(() => {
    setTranslatedUi(!(i18n.resolvedLanguage ?? i18n.language).startsWith("en"));
  }, [i18n.resolvedLanguage, i18n.language]);

  // Ctrl/Cmd + K opens the search anywhere in the docs.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const searchButton = (
    <button
      type="button"
      onClick={() => setSearchOpen(true)}
      className="flex h-8 w-full items-center gap-2 border border-line bg-surface px-2 text-base text-ink-subtle transition-colors duration-80 ease-linear hover:border-line-strong hover:text-ink"
      data-testid="docs-search-trigger"
    >
      <SearchIcon className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span className="flex-1 text-start">{t("docs.search")}</span>
      <kbd className="bx-kbd" dir="ltr">
        {KBD_HINT}
      </kbd>
    </button>
  );

  return (
    <div data-mode="industrial" className="site-rail !px-0">
      {translatedUi && (
        <p className="border-b border-line bg-surface-sunken px-4 py-2 text-sm text-ink-muted">
          {t("docs.englishOnly")}
        </p>
      )}

      {/* Small screens: toolbar with the tree drawer and the search. */}
      <div className="flex items-center gap-2 border-b border-line px-4 py-2 lg:hidden">
        <Dialog open={menuOpen} onOpenChange={setMenuOpen}>
          <DialogTrigger asChild>
            <Button variant="outline" size="sm" className="shrink-0 gap-2">
              <MenuIcon className="h-4 w-4" aria-hidden="true" />
              {t("docs.menu")}
            </Button>
          </DialogTrigger>
          <DialogContent closeLabel={t("nav.closeMenu")}>
            <div className="flex h-14 shrink-0 items-center border-b border-line px-4">
              <DialogTitle>{t("docs.title")}</DialogTitle>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              <DocsSidebar currentSlug={slug} onNavigate={() => setMenuOpen(false)} />
            </div>
          </DialogContent>
        </Dialog>
        <div className="min-w-0 flex-1">{searchButton}</div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[256px_minmax(0,1fr)]">
        <aside className="hidden border-e border-line bg-surface lg:block">
          <div className="sticky top-14 flex max-h-[calc(100vh-56px)] flex-col">
            <div className="border-b border-line p-3">{searchButton}</div>
            <div className="min-h-0 flex-1 overflow-y-auto pb-8">
              <DocsSidebar currentSlug={slug} />
            </div>
          </div>
        </aside>
        <div className="min-w-0">
          <Outlet />
        </div>
      </div>

      <DocsSearch open={searchOpen} onOpenChange={setSearchOpen} />
    </div>
  );
}
