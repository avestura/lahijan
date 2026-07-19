/**
 * Pricing page (`/pricing`).
 *
 * Lead copy + PricingTable + FAQ + CTA.
 */
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { FAQ } from "@/components/marketing/FAQ";
import { PricingTable } from "@/components/marketing/PricingTable";
import { Section } from "@/components/marketing/Section";
import { SeoHead } from "@/components/seo/SeoHead";

export function PricingPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead titleKey="page.pricing.title" descriptionKey="page.pricing.lead" path="/pricing" />
      <Section className="bg-background">
        <div className="mx-auto max-w-3xl text-center">
          <h1 className="text-4xl font-extrabold tracking-tight text-foreground md:text-5xl">
            {t("page.pricing.title")}
          </h1>
          <p className="mt-4 text-lg text-muted-foreground">{t("page.pricing.subtitle")}</p>
          <p className="mt-6 text-base text-muted-foreground">{t("page.pricing.lead")}</p>
        </div>
      </Section>
      <PricingTable />
      <FAQ />
      <CTASection compact />
    </>
  );
}
