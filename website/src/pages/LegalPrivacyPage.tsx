/**
 * Privacy policy (`/legal/privacy`).
 *
 * Boilerplate per WS-19: each deployer is responsible for customizing the
 * copy. The structured sections below are placeholders a deployer can edit
 * in their locale bundle (or replace wholesale).
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SeoHead } from "@/components/seo/SeoHead";

const PRIVACY_SECTIONS = ["scope", "data", "retention", "contact"] as const;

export function LegalPrivacyPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead
        titleKey="page.legal.privacy.title"
        descriptionKey="page.legal.privacy.lead"
        path="/legal/privacy"
      />
      <Section className="bg-background">
        <article className="mx-auto max-w-3xl space-y-8">
          <header className="space-y-3">
            <h1 className="text-4xl font-extrabold tracking-tight text-foreground md:text-5xl">
              {t("page.legal.privacy.title")}
            </h1>
            <p className="text-sm text-muted-foreground">
              {t("page.legal.privacy.updated")}: {new Date().toISOString().slice(0, 10)}
            </p>
            <p className="text-base text-muted-foreground">{t("page.legal.privacy.lead")}</p>
          </header>
          {PRIVACY_SECTIONS.map((section) => (
            <section key={section} className="space-y-2">
              <h2 className="text-xl font-semibold text-foreground">
                {t(`page.legal.privacy.sections.${section}.title`)}
              </h2>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {t(`page.legal.privacy.sections.${section}.body`)}
              </p>
            </section>
          ))}
        </article>
      </Section>
    </>
  );
}
