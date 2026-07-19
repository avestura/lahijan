/**
 * Footer — site-wide footer with product, resource, and legal link groups.
 *
 * All copy goes through `t()`. Layout uses logical properties so RTL flips
 * correctly.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { BrandMark } from "@/components/layout/BrandMark";
import { LEGAL_ENTRIES, NAV_ENTRIES, DASHBOARD_BASE_URL } from "@/lib/site";

export function Footer() {
  const { t } = useTranslation();
  const year = new Date().getFullYear();

  return (
    <footer className="border-t border-border bg-background">
      <div className="container grid grid-cols-1 gap-10 py-12 md:grid-cols-4">
        <div className="space-y-3">
          <BrandMark />
          <p className="max-w-xs text-sm text-muted-foreground">{t("footer.description")}</p>
        </div>

        <FooterColumn heading={t("footer.product")}>
          {NAV_ENTRIES.filter((e) => ["/features", "/pricing"].includes(e.route)).map((entry) => (
            <FooterLink key={entry.route} to={entry.route}>
              {t(entry.i18nKey)}
            </FooterLink>
          ))}
          <FooterLink to={DASHBOARD_BASE_URL}>{t("cta.signIn")}</FooterLink>
        </FooterColumn>

        <FooterColumn heading={t("footer.resources")}>
          {NAV_ENTRIES.filter((e) => ["/docs", "/blog"].includes(e.route)).map((entry) => (
            <FooterLink key={entry.route} to={entry.route}>
              {t(entry.i18nKey)}
            </FooterLink>
          ))}
          <FooterLink to="/about">{t("nav.about")}</FooterLink>
        </FooterColumn>

        <FooterColumn heading={t("footer.legal")}>
          {LEGAL_ENTRIES.map((entry) => (
            <FooterLink key={entry.route} to={entry.route}>
              {t(entry.i18nKey)}
            </FooterLink>
          ))}
        </FooterColumn>
      </div>

      <div className="border-t border-border">
        <div className="container flex flex-col items-start justify-between gap-2 py-6 text-xs text-muted-foreground md:flex-row md:items-center">
          <p>
            {t("footer.copyright", {
              year,
              name: t("app.name"),
              rights: t("footer.rights"),
            })}
          </p>
          <p>{t("footer.madeWith")}</p>
        </div>
      </div>
    </footer>
  );
}

function FooterColumn({ heading, children }: { heading: string; children: React.ReactNode }) {
  return (
    <nav aria-label={heading} className="space-y-3">
      <h2 className="text-sm font-semibold text-foreground">{heading}</h2>
      <ul className="space-y-2">{children}</ul>
    </nav>
  );
}

function FooterLink({ to, children }: { to: string; children: React.ReactNode }) {
  // External-ish link (absolute URL or different SPA root): use <a>. Otherwise
  // use react-router's <Link> for client-side nav.
  const isExternal = to.startsWith("http") || to.startsWith("/web");
  const cls =
    "text-sm text-muted-foreground transition-colors hover:text-foreground hover:underline";
  if (isExternal) {
    return (
      <li>
        <a href={to} className={cls}>
          {children}
        </a>
      </li>
    );
  }
  return (
    <li>
      <Link to={to} className={cls}>
        {children}
      </Link>
    </li>
  );
}
