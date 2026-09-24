/**
 * 404 fallback. Rendered by react-router's errorElement, inside SiteLayout.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { PageHeader } from "@/components/marketing/PageHeader";
import { Button } from "@/components/ui/button";
import { SeoHead } from "@/components/seo/SeoHead";

export function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead titleKey="page.notFound.title" path="/404" />
      <PageHeader
        eyebrow={t("page.notFound.code")}
        title={t("page.notFound.title")}
        lead={t("page.notFound.body")}
      >
        <Button asChild size="lg" className="mt-10">
          <Link to="/">{t("page.notFound.home")}</Link>
        </Button>
      </PageHeader>
    </>
  );
}
