/**
 * Header — top-level marketing site chrome.
 *
 * Contains:
 *   - BrandMark (links home)
 *   - Desktop nav (Features / Pricing / Docs / Blog / About)
 *   - Theme toggle, language toggle
 *   - Sign-in CTA → deep-links to the dashboard SPA
 *   - Mobile menu (drawer via the Dialog primitive)
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
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { DASHBOARD_BASE_URL, NAV_ENTRIES } from "@/lib/site";
import { cn } from "@/lib/utils";

export function Header() {
  const { t } = useTranslation();
  const [mobileOpen, setMobileOpen] = useState(false);

  return (
    <header className="sticky top-0 z-40 w-full border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80">
      <a href="#main" className="skip-link">
        {t("nav.skipToContent")}
      </a>
      <div className="container flex h-16 items-center justify-between gap-4">
        <BrandMark />

        <nav aria-label={t("nav.home")} className="hidden items-center gap-1 md:flex">
          {NAV_ENTRIES.map((entry) => (
            <NavLink
              key={entry.route}
              to={entry.route}
              className={({ isActive }) =>
                cn(
                  "rounded-md px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground",
                  isActive && "text-foreground",
                )
              }
            >
              {t(entry.i18nKey)}
            </NavLink>
          ))}
        </nav>

        <div className="flex items-center gap-1">
          <LanguageToggle />
          <ThemeToggle />
          <Button asChild className="hidden md:inline-flex">
            <Link to={DASHBOARD_BASE_URL}>{t("cta.signIn")}</Link>
          </Button>

          <Dialog open={mobileOpen} onOpenChange={setMobileOpen}>
            <DialogTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="md:hidden"
                aria-label={t("nav.openMenu")}
              >
                <MenuIcon className="h-5 w-5" />
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogTitle>{t("app.name")}</DialogTitle>
              <nav className="mt-4 flex flex-col gap-1" aria-label={t("nav.home")}>
                {NAV_ENTRIES.map((entry) => (
                  <NavLink
                    key={entry.route}
                    to={entry.route}
                    onClick={() => setMobileOpen(false)}
                    className={({ isActive }) =>
                      cn(
                        "rounded-md px-3 py-2 text-base font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground",
                        isActive && "text-foreground",
                      )
                    }
                  >
                    {t(entry.i18nKey)}
                  </NavLink>
                ))}
              </nav>
              <Button asChild className="mt-2 w-full">
                <Link to={DASHBOARD_BASE_URL}>{t("cta.signIn")}</Link>
              </Button>
            </DialogContent>
          </Dialog>
        </div>
      </div>
    </header>
  );
}
