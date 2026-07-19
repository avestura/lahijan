/**
 * Blog index (`/blog`).
 *
 * CMS-less for MVP per WS-19. Renders the index header and an empty-state
 * placeholder. The blog post detail route (`/blog/:slug`) is also wired so
 * deep links don't completely 404 once posts exist.
 */
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { Section } from "@/components/marketing/Section";
import { Card, CardContent } from "@/components/ui/card";
import { SeoHead } from "@/components/seo/SeoHead";

export function BlogListPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead titleKey="page.blog.title" descriptionKey="page.blog.lead" path="/blog" />
      <Section className="bg-background">
        <div className="mx-auto max-w-3xl text-center">
          <h1 className="text-4xl font-extrabold tracking-tight text-foreground md:text-5xl">
            {t("page.blog.title")}
          </h1>
          <p className="mt-4 text-lg text-muted-foreground">{t("page.blog.subtitle")}</p>
          <p className="mt-6 text-base text-muted-foreground">{t("page.blog.lead")}</p>
        </div>
      </Section>
      <Section className="bg-muted/30">
        <Card className="mx-auto max-w-2xl border-dashed text-center">
          <CardContent className="p-10">
            <p className="text-sm text-muted-foreground">{t("page.blog.empty")}</p>
          </CardContent>
        </Card>
      </Section>
      <CTASection compact />
    </>
  );
}
