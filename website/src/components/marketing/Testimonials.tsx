/**
 * Testimonials — placeholder quote cards.
 *
 * Per WS-19 open question 1 / "testimonial placeholder section (filled by
 * operator)", these are stock placeholder quotes that an operator customizes
 * in their own deployment (via the locale bundle).
 */
import { QuoteIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Card, CardContent } from "@/components/ui/card";

type TKey = "t1" | "t2" | "t3";
const T_KEYS: readonly TKey[] = ["t1", "t2", "t3"];

export function Testimonials() {
  const { t } = useTranslation();
  return (
    <Section id="testimonials" className="bg-muted/30">
      <SectionHeading
        title={t("testimonials.sectionTitle")}
        subtitle={t("testimonials.sectionSubtitle")}
      />
      <ul role="list" className="mt-12 grid grid-cols-1 gap-6 md:grid-cols-3">
        {T_KEYS.map((key) => (
          <li key={key}>
            <Card className="h-full">
              <CardContent className="space-y-4 p-6">
                <QuoteIcon className="h-6 w-6 text-primary/60" />
                <blockquote className="text-sm leading-relaxed text-foreground">
                  {t(`testimonials.items.${key}.quote`)}
                </blockquote>
                <figcaption className="text-xs text-muted-foreground">
                  <span className="block font-semibold text-foreground">
                    {t(`testimonials.items.${key}.name`)}
                  </span>
                  {t(`testimonials.items.${key}.role`)}
                </figcaption>
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>
    </Section>
  );
}
