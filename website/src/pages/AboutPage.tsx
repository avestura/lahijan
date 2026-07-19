/**
 * About page (`/about`).
 *
 * Lead copy, mission, principles, and the "why Lahijan?" section.
 */
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { Section } from "@/components/marketing/Section";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { SeoHead } from "@/components/seo/SeoHead";

const PRINCIPLE_KEYS = ["transparent", "tenantIsolation", "open"] as const;

export function AboutPage() {
  const { t } = useTranslation();

  return (
    <>
      <SeoHead titleKey="page.about.title" descriptionKey="page.about.lead" path="/about" />
      <Section className="bg-background">
        <div className="mx-auto max-w-3xl text-center">
          <h1 className="text-4xl font-extrabold tracking-tight text-foreground md:text-5xl">
            {t("page.about.title")}
          </h1>
          <p className="mt-4 text-lg text-muted-foreground">{t("page.about.subtitle")}</p>
          <p className="mt-6 text-base text-muted-foreground">{t("page.about.lead")}</p>
        </div>
      </Section>

      <Section className="bg-muted/30" containerClassName="grid grid-cols-1 gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">{t("page.about.mission.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm leading-relaxed text-muted-foreground">
              {t("page.about.mission.body")}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">{t("page.about.nameOrigin.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm leading-relaxed text-muted-foreground">
              {t("page.about.nameOrigin.body")}
            </p>
          </CardContent>
        </Card>
      </Section>

      <Section className="bg-background">
        <h2 className="text-center text-2xl font-bold tracking-tight text-foreground md:text-3xl">
          {t("page.about.principles.title")}
        </h2>
        <ul role="list" className="mx-auto mt-10 grid max-w-4xl grid-cols-1 gap-6 md:grid-cols-3">
          {PRINCIPLE_KEYS.map((key) => (
            <li key={key}>
              <Card className="h-full">
                <CardHeader>
                  <CardTitle className="text-base">
                    {t(`page.about.principles.items.${key}.title`)}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <p className="text-sm leading-relaxed text-muted-foreground">
                    {t(`page.about.principles.items.${key}.body`)}
                  </p>
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      </Section>

      <CTASection compact />
    </>
  );
}
