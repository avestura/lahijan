/**
 * Blog post placeholder (`/blog/:slug`).
 *
 * There are no posts in MVP; this route exists so the path resolves to the
 * site chrome instead of a raw 404 once posts are added (Phase 7). Until
 * posts exist, it shows the empty-state copy from the blog index.
 */
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { Card, CardContent } from "@/components/ui/card";
import { SeoHead } from "@/components/seo/SeoHead";

export function BlogPostPage() {
  const { t } = useTranslation();
  // Read the slug so it appears in the rendered DOM; this also documents
  // the param shape for the future.
  const { slug } = useParams();
  return (
    <>
      <SeoHead
        titleKey="page.blog.title"
        descriptionKey="page.blog.lead"
        path={`/blog/${slug ?? ""}`}
      />
      <Section className="bg-background">
        <Card className="mx-auto max-w-2xl border-dashed text-center">
          <CardContent className="p-10">
            <p className="text-sm text-muted-foreground">{t("page.blog.empty")}</p>
          </CardContent>
        </Card>
      </Section>
    </>
  );
}
