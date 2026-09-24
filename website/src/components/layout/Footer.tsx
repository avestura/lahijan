/**
 * Footer — site-wide footer with product, resource, and legal link groups.
 *
 * Boxy footer: full-bleed 1px top rule on a sunken surface, a collapsed grid
 * (a wider brand cell plus three link columns with mono headings), and a
 * bottom bar separated by a 1px rule carrying the copyright, the licence
 * line and a mono build string at the inline end.
 *
 * All copy goes through `t()`. Layout uses logical properties so RTL flips
 * correctly.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { BrandMark } from "@/components/layout/BrandMark";
import { APP_VERSION, LEGAL_ENTRIES, NAV_ENTRIES, DASHBOARD_BASE_URL } from "@/lib/site";

export function Footer() {
  const { t } = useTranslation();
  const year = new Date().getFullYear();

  return (
    <footer className="border-t border-line bg-surface-sunken">
      <div className="site-rail">
        <div className="site-bleed bx-collapse grid-cols-1 border-b border-line md:grid-cols-2 lg:grid-cols-5">
          <div className="bg-surface-sunken px-4 py-10 md:px-8 lg:col-span-2">
            <BrandMark />
            <p className="mt-4 max-w-[36ch] text-base text-muted-foreground">
              {t("footer.description")}
            </p>
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

        <div className="flex flex-col items-start justify-between gap-3 py-6 text-sm text-ink-subtle md:flex-row md:items-center">
          <p>
            {t("footer.copyright", {
              year,
              name: t("app.name"),
              rights: t("footer.rights"),
            })}
          </p>
          <p>{t("footer.license")}</p>
          <p className="bx-label" dir="ltr">
            {t("footer.version", { version: APP_VERSION })}
          </p>
        </div>
      </div>
    </footer>
  );
}

function FooterColumn({ heading, children }: { heading: string; children: React.ReactNode }) {
  return (
    <nav aria-label={heading} className="bg-surface-sunken px-4 py-10 md:px-8">
      <h2 className="bx-label">{heading}</h2>
      <ul className="mt-4 flex flex-col">{children}</ul>
    </nav>
  );
}

function FooterLink({ to, children }: { to: string; children: React.ReactNode }) {
  // External-ish link (absolute URL or different SPA root): use <a>. Otherwise
  // use react-router's <Link> for client-side nav.
  const isExternal = to.startsWith("http") || to.startsWith("/web");
  const cls =
    "inline-flex h-7 items-center text-base text-ink-muted no-underline transition-colors duration-80 ease-linear hover:text-ink hover:underline";
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
