/**
 * HowItWorks — three-step "deploy → onboard → scale" block on the landing
 * page. Rendered as an ordered list to preserve reading order in RTL too.
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface Step {
  number: string;
  titleKey: string;
  bodyKey: string;
}

const STEPS: readonly Step[] = [
  { number: "1", titleKey: "howItWorks.step1.title", bodyKey: "howItWorks.step1.body" },
  { number: "2", titleKey: "howItWorks.step2.title", bodyKey: "howItWorks.step2.body" },
  { number: "3", titleKey: "howItWorks.step3.title", bodyKey: "howItWorks.step3.body" },
];

export function HowItWorks() {
  const { t } = useTranslation();

  return (
    <Section id="how-it-works" className="bg-background">
      <SectionHeading
        title={t("howItWorks.sectionTitle")}
        subtitle={t("howItWorks.sectionSubtitle")}
      />
      <ol
        role="list"
        className="mt-12 grid grid-cols-1 gap-6 md:grid-cols-3"
        aria-label={t("howItWorks.sectionTitle")}
      >
        {STEPS.map(({ number, titleKey, bodyKey }) => (
          <li key={titleKey}>
            <Card className="h-full">
              <CardHeader>
                <span
                  aria-hidden="true"
                  className="mb-3 inline-flex h-8 w-8 items-center justify-center rounded-full bg-primary text-sm font-semibold text-primary-foreground"
                >
                  {number}
                </span>
                <CardTitle className="text-lg">{t(titleKey)}</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-sm leading-relaxed text-muted-foreground">{t(bodyKey)}</p>
              </CardContent>
            </Card>
          </li>
        ))}
      </ol>
    </Section>
  );
}
