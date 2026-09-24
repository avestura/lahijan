/**
 * Pricing page (`/pricing`).
 *
 * Page header + PricingTable + FAQ + CTA.
 */
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { FAQ } from "@/components/marketing/FAQ";
import { PageHeader } from "@/components/marketing/PageHeader";
import { PricingTable } from "@/components/marketing/PricingTable";
import { SeoHead } from "@/components/seo/SeoHead";

export function PricingPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead titleKey="page.pricing.title" descriptionKey="page.pricing.lead" path="/pricing" />
      <PageHeader
        eyebrow={t("page.pricing.title")}
        title={t("page.pricing.subtitle")}
        lead={t("page.pricing.lead")}
      />
      <PricingTable />
      <FAQ />
      <CTASection compact />
    </>
  );
}
