/**
 * Header — top-level marketing site chrome.
 *
 * Contains:
 *   - BrandMark (links home)
 *   - Desktop nav (Features / Pricing / Docs / Blog / About)
 *   - Language + theme toggles and the Sign-in link, in one shared-border
 *     button group at the inline end
 *   - Mobile menu (a full-height drawer via the Dialog primitive)
 *
 * Boxy marketing top bar: 56px, solid surface (never translucent or
 * blurred), 1px bottom rule, sticky. The active nav item is marked by a 2px
 * accent bar that replaces the header rule beneath it, drawn with shadows so
 * a hover fill can never punch a gap in the line.
 *
 * All copy goes through `t()`. Layout uses logical properties so RTL flips
 * correctly.
 */
import { useState } from "react";
import { Link, NavLink } from "react-router-dom";
import { MenuIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { BrandMark } from "@/components/layout/BrandMark";
import { LanguageToggle } from "@/components/layout/LanguageToggle";
import { ThemeToggle } from "@/components/layout/ThemeToggle";
import { Button, ButtonGroup } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { DASHBOARD_BASE_URL, NAV_ENTRIES } from "@/lib/site";
import { cn } from "@/lib/utils";

const NAV_ACTIVE_BAR =
  "text-ink shadow-[inset_0_-2px_0_0_var(--bx-accent),0_1px_0_0_var(--bx-accent)]";

export function Header() {
  const { t } = useTranslation();
  const [mobileOpen, setMobileOpen] = useState(false);

  return (
    <header className="sticky top-0 z-10 w-full border-b border-line bg-surface">
      <a href="#main" className="skip-link">
        {t("nav.skipToContent")}
      </a>
      <div className="site-rail flex h-14 items-center justify-between gap-6">
        <div className="flex h-full items-center gap-8">
          <BrandMark />
          <nav aria-label={t("nav.home")} className="hidden h-full items-stretch md:flex">
            {NAV_ENTRIES.map((entry) => (
              <NavLink
                key={entry.route}
                to={entry.route}
                className={({ isActive }) =>
                  cn(
                    "flex items-center px-4 text-base font-medium text-ink-muted no-underline transition-colors duration-80 ease-linear hover:bg-surface-hover hover:text-ink hover:no-underline",
                    isActive && NAV_ACTIVE_BAR,
                  )
                }
              >
                {t(entry.i18nKey)}
              </NavLink>
            ))}
          </nav>
        </div>

        <ButtonGroup>
          <LanguageToggle />
          <ThemeToggle />
          <Button asChild variant="outline" className="hidden md:inline-flex">
            <Link to={DASHBOARD_BASE_URL}>{t("cta.signIn")}</Link>
          </Button>

          <Dialog open={mobileOpen} onOpenChange={setMobileOpen}>
            <DialogTrigger asChild>
              <Button
                variant="outline"
                size="icon"
                className="md:hidden"
                aria-label={t("nav.openMenu")}
              >
                <MenuIcon className="h-5 w-5" aria-hidden="true" />
              </Button>
            </DialogTrigger>
            <DialogContent closeLabel={t("nav.closeMenu")}>
              <div className="flex h-14 items-center border-b border-line px-4">
                <DialogTitle>{t("app.name")}</DialogTitle>
              </div>
              <nav className="flex flex-col" aria-label={t("nav.home")}>
                {NAV_ENTRIES.map((entry) => (
                  <NavLink
                    key={entry.route}
                    to={entry.route}
                    onClick={() => setMobileOpen(false)}
                    className={({ isActive }) =>
                      cn(
                        "flex h-12 items-center border-b border-s-2 border-line border-s-transparent px-4 text-md font-medium text-ink-muted no-underline transition-colors duration-80 ease-linear hover:bg-surface-hover hover:text-ink hover:no-underline",
                        isActive && "border-s-line-accent bg-surface-active text-ink",
                      )
                    }
                  >
                    {t(entry.i18nKey)}
                  </NavLink>
                ))}
              </nav>
              <div className="p-4">
                <Button asChild variant="outline" size="lg" className="w-full">
                  <Link to={DASHBOARD_BASE_URL}>{t("cta.signIn")}</Link>
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </ButtonGroup>
      </div>
    </header>
  );
}
