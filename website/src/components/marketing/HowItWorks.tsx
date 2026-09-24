/**
 * HowItWorks — three-step "deploy → onboard → scale" block on the landing
 * page. An ordered list in a collapsed grid (reading order survives RTL),
 * each step led by a large mono step number rather than a coloured disc.
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { useFormatNumber } from "@/lib/format";

const STEPS = [
  { titleKey: "howItWorks.step1.title", bodyKey: "howItWorks.step1.body" },
  { titleKey: "howItWorks.step2.title", bodyKey: "howItWorks.step2.body" },
  { titleKey: "howItWorks.step3.title", bodyKey: "howItWorks.step3.body" },
] as const;

export function HowItWorks() {
  const { t } = useTranslation();
  const fmt = useFormatNumber();

  return (
    <Section id="how-it-works">
      <SectionHeading
        eyebrow={t("howItWorks.eyebrow")}
        title={t("howItWorks.sectionTitle")}
        subtitle={t("howItWorks.sectionSubtitle")}
      />
      <ol
        role="list"
        className="site-bleed bx-collapse mt-16 grid-cols-1 border-y border-line md:grid-cols-3"
        aria-label={t("howItWorks.sectionTitle")}
      >
        {STEPS.map(({ titleKey, bodyKey }, idx) => (
          <li key={titleKey} className="px-4 py-10 md:px-8">
            <span aria-hidden="true" className="bx-mono block text-5xl font-medium text-ink-faint">
              {fmt(idx + 1, 2)}
            </span>
            <h3 className="mt-10 text-xl">{t(titleKey)}</h3>
            <p className="mt-3 max-w-[44ch] text-base text-muted-foreground">{t(bodyKey)}</p>
          </li>
        ))}
      </ol>
    </Section>
  );
}
