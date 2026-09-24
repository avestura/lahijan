/**
 * Features page (`/features`).
 *
 * Page header + FeatureGrid + one ruled detail row per feature + CTA. The
 * grid cells link to the detail rows' anchors (#compute, #dns, ...), which
 * are also the targets of the landing page's feature links.
 */
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { FEATURE_DEFS } from "@/components/marketing/feature-defs";
import { FeatureGrid } from "@/components/marketing/FeatureGrid";
import { PageHeader } from "@/components/marketing/PageHeader";
import { Section } from "@/components/marketing/Section";
import { SeoHead } from "@/components/seo/SeoHead";
import { useFormatNumber } from "@/lib/format";

const POINTS = ["p1", "p2", "p3"] as const;

export function FeaturesPage() {
  const { t } = useTranslation();
  const fmt = useFormatNumber();

  return (
    <>
      <SeoHead
        titleKey="page.features.title"
        descriptionKey="page.features.lead"
        path="/features"
      />
      <PageHeader
        eyebrow={t("page.features.title")}
        title={t("page.features.subtitle")}
        lead={t("page.features.lead")}
      />
      <FeatureGrid variant="page" />

      <Section aria-labelledby="feature-detail-title">
        <h2 id="feature-detail-title" className="max-w-[20ch] text-3xl md:text-4xl">
          {t("page.features.detailTitle")}
        </h2>
        <div className="site-bleed mt-16 border-t border-line">
          {FEATURE_DEFS.map(({ key, icon: Icon }, idx) => (
            <article
              key={key}
              id={key}
              className="grid scroll-mt-24 grid-cols-1 gap-6 border-b border-line px-4 py-10 md:px-8 lg:grid-cols-12"
            >
              <div className="lg:col-span-4">
                <p className="bx-label">
                  {fmt(idx + 1, 2)} / {t(`features.${key}.tag`)}
                </p>
                <h3 className="mt-3 flex items-center gap-3 text-2xl">
                  <Icon className="h-5 w-5 shrink-0" strokeWidth={1.5} aria-hidden="true" />
                  {t(`features.${key}.title`)}
                </h3>
              </div>
              <ul role="list" className="flex flex-col lg:col-span-8">
                {POINTS.map((p) => (
                  <li
                    key={p}
                    className="border-b border-line-subtle py-3 text-md text-muted-foreground first:pt-0 last:border-b-0 last:pb-0"
                  >
                    {t(`page.features.detail.${key}.${p}`)}
                  </li>
                ))}
              </ul>
            </article>
          ))}
        </div>
      </Section>

      <CTASection compact />
    </>
  );
}
