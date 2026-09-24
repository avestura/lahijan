/**
 * Blog post placeholder (`/blog/:slug`).
 *
 * There are no posts in MVP; this route exists so the path resolves to the
 * site chrome instead of a raw 404 once posts are added (Phase 7). Until
 * posts exist, it shows the empty-state copy from the blog index.
 */
import { useParams } from "react-router-dom";

import { BlogEmptyState } from "@/components/marketing/BlogEmptyState";
import { Section } from "@/components/marketing/Section";
import { SeoHead } from "@/components/seo/SeoHead";

export function BlogPostPage() {
  // Read the slug so the canonical URL reflects it; this also documents the
  // param shape for the future.
  const { slug } = useParams();
  return (
    <>
      <SeoHead
        titleKey="page.blog.title"
        descriptionKey="page.blog.lead"
        path={`/blog/${slug ?? ""}`}
      />
      <Section rhythm="tight">
        <BlogEmptyState />
      </Section>
    </>
  );
}
