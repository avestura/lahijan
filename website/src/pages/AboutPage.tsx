/**
 * About page (`/about`).
 *
 * Page header, mission + name origin, principles, CTA. Both content blocks
 * are collapsed grids so each reads as one ruled table.
 */
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { PageHeader } from "@/components/marketing/PageHeader";
import { Section } from "@/components/marketing/Section";
import { SeoHead } from "@/components/seo/SeoHead";
import { useFormatNumber } from "@/lib/format";

const PRINCIPLE_KEYS = ["transparent", "tenantIsolation", "open"] as const;
const STORY_KEYS = ["mission", "nameOrigin"] as const;

export function AboutPage() {
  const { t } = useTranslation();
  const fmt = useFormatNumber();

  return (
    <>
      <SeoHead titleKey="page.about.title" descriptionKey="page.about.lead" path="/about" />
      <PageHeader
        eyebrow={t("page.about.title")}
        title={t("page.about.subtitle")}
        lead={t("page.about.lead")}
      />

      <Section rhythm="tight">
        <div className="site-bleed bx-collapse grid-cols-1 border-y border-line md:grid-cols-2">
          {STORY_KEYS.map((key) => (
            <div key={key} className="px-4 py-10 md:px-8">
              <h2 className="text-2xl">{t(`page.about.${key}.title`)}</h2>
              <p className="mt-4 max-w-[56ch] text-md text-muted-foreground">
                {t(`page.about.${key}.body`)}
              </p>
            </div>
          ))}
        </div>
      </Section>

      <Section aria-labelledby="principles-title">
        <h2 id="principles-title" className="max-w-[20ch] text-3xl md:text-4xl">
          {t("page.about.principles.title")}
        </h2>
        <ul
          role="list"
          className="site-bleed bx-collapse mt-16 grid-cols-1 border-y border-line md:grid-cols-3"
        >
          {PRINCIPLE_KEYS.map((key, idx) => (
            <li key={key} className="px-4 py-10 md:px-8">
              <span aria-hidden="true" className="bx-mono text-xs text-ink-faint">
                {fmt(idx + 1, 2)}
              </span>
              <h3 className="mt-6 text-xl">{t(`page.about.principles.items.${key}.title`)}</h3>
              <p className="mt-3 text-base text-muted-foreground">
                {t(`page.about.principles.items.${key}.body`)}
              </p>
            </li>
          ))}
        </ul>
      </Section>

      <CTASection compact />
    </>
  );
}
