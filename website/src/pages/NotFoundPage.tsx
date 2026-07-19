/**
 * 404 fallback. Rendered by react-router's errorElement.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { Button } from "@/components/ui/button";
import { SeoHead } from "@/components/seo/SeoHead";

export function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead titleKey="page.notFound.title" path="/404" />
      <Section className="bg-background">
        <div className="mx-auto max-w-xl text-center">
          <h1 className="text-4xl font-extrabold tracking-tight text-foreground md:text-5xl">
            {t("page.notFound.title")}
          </h1>
          <p className="mt-4 text-base text-muted-foreground">{t("page.notFound.body")}</p>
          <div className="mt-8">
            <Button asChild>
              <Link to="/">{t("page.notFound.home")}</Link>
            </Button>
          </div>
        </div>
      </Section>
    </>
  );
}
