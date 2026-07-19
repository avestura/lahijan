/**
 * Terms of service (`/legal/terms`).
 *
 * Boilerplate per WS-19: each deployer is responsible for customizing the
 * copy.
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SeoHead } from "@/components/seo/SeoHead";

const TERMS_SECTIONS = ["scope", "accounts", "acceptable", "billing", "contact"] as const;

export function LegalTermsPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead
        titleKey="page.legal.terms.title"
        descriptionKey="page.legal.terms.lead"
        path="/legal/terms"
      />
      <Section className="bg-background">
        <article className="mx-auto max-w-3xl space-y-8">
          <header className="space-y-3">
            <h1 className="text-4xl font-extrabold tracking-tight text-foreground md:text-5xl">
              {t("page.legal.terms.title")}
            </h1>
            <p className="text-sm text-muted-foreground">
              {t("page.legal.terms.updated")}: {new Date().toISOString().slice(0, 10)}
            </p>
            <p className="text-base text-muted-foreground">{t("page.legal.terms.lead")}</p>
          </header>
          {TERMS_SECTIONS.map((section) => (
            <section key={section} className="space-y-2">
              <h2 className="text-xl font-semibold text-foreground">
                {t(`page.legal.terms.sections.${section}.title`)}
              </h2>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {t(`page.legal.terms.sections.${section}.body`)}
              </p>
            </section>
          ))}
        </article>
      </Section>
    </>
  );
}
