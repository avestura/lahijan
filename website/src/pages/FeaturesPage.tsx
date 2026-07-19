/**
 * Features page (`/features`).
 *
 * Lead copy + FeatureGrid + CTA. The FeatureGrid already links each card
 * to its anchored section id on the page (#compute, #dns, ...).
 */
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { FeatureGrid } from "@/components/marketing/FeatureGrid";
import { Section } from "@/components/marketing/Section";
import { SeoHead } from "@/components/seo/SeoHead";

export function FeaturesPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead
        titleKey="page.features.title"
        descriptionKey="page.features.lead"
        path="/features"
      />
      <Section className="bg-background">
        <div className="mx-auto max-w-3xl text-center">
          <h1 className="text-4xl font-extrabold tracking-tight text-foreground md:text-5xl">
            {t("page.features.title")}
          </h1>
          <p className="mt-4 text-lg text-muted-foreground">{t("page.features.subtitle")}</p>
          <p className="mt-6 text-base text-muted-foreground">{t("page.features.lead")}</p>
        </div>
      </Section>
      <FeatureGrid />
      <CTASection compact />
    </>
  );
}
