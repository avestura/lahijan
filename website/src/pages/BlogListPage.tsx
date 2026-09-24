/**
 * Blog index (`/blog`).
 *
 * CMS-less for MVP per WS-19. Renders the page header and an empty-state
 * block. The blog post detail route (`/blog/:slug`) is also wired so deep
 * links don't completely 404 once posts exist.
 */
import { useTranslation } from "react-i18next";

import { BlogEmptyState } from "@/components/marketing/BlogEmptyState";
import { CTASection } from "@/components/marketing/CTASection";
import { PageHeader } from "@/components/marketing/PageHeader";
import { Section } from "@/components/marketing/Section";
import { SeoHead } from "@/components/seo/SeoHead";

export function BlogListPage() {
  const { t } = useTranslation();
  return (
    <>
      <SeoHead titleKey="page.blog.title" descriptionKey="page.blog.lead" path="/blog" />
      <PageHeader
        eyebrow={t("page.blog.title")}
        title={t("page.blog.subtitle")}
        lead={t("page.blog.lead")}
      />
      <Section rhythm="tight">
        <BlogEmptyState />
      </Section>
      <CTASection compact />
    </>
  );
}
