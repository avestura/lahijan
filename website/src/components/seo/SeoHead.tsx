/**
 * SeoHead — per-page <head> writer.
 *
 * Wraps vite-react-ssg's <Head> so every page gets a consistent set of meta
 * tags (title, description, OpenGraph, Twitter, canonical) just by passing
 * a title + description. SSG picks these up at build time so the
 * prerendered HTML already has them — no client-side JS required for SEO.
 */
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Head } from "vite-react-ssg";

import { absoluteUrl, DEFAULT_OG_IMAGE } from "@/lib/site";

interface SeoHeadProps {
  /** Translation key for the page title (without the site suffix). */
  titleKey: string;
  /** Translation key for the meta description. */
  descriptionKey?: string;
  /** Path of the page (used for the canonical URL). Defaults to "/". */
  path?: string;
  /** Optional OpenGraph image path (relative). Defaults to the site OG image. */
  ogImage?: string;
}

export function SeoHead({ titleKey, descriptionKey, path = "/", ogImage }: SeoHeadProps) {
  const { t } = useTranslation();
  const title = t(titleKey);
  const siteName = t("app.name");
  const description = descriptionKey ? t(descriptionKey) : t("app.description");
  const canonical = absoluteUrl(path);
  const ogImageAbsolute = absoluteUrl(ogImage ?? DEFAULT_OG_IMAGE);

  // Set document.title for client-side navigations (Head handles SSG output).
  useEffect(() => {
    document.title = `${title} · ${siteName}`;
  }, [title, siteName]);

  return (
    <Head>
      <title>{`${title} · ${siteName}`}</title>
      <meta name="description" content={description} />
      <link rel="canonical" href={canonical} />

      {/* OpenGraph */}
      <meta property="og:type" content="website" />
      <meta property="og:site_name" content={siteName} />
      <meta property="og:title" content={title} />
      <meta property="og:description" content={description} />
      <meta property="og:url" content={canonical} />
      <meta property="og:image" content={ogImageAbsolute} />

      {/* Twitter */}
      <meta name="twitter:card" content="summary_large_image" />
      <meta name="twitter:title" content={title} />
      <meta name="twitter:description" content={description} />
      <meta name="twitter:image" content={ogImageAbsolute} />

      {/* Hreflang for the two supported locales (en, fa). */}
      <link rel="alternate" hrefLang="en" href={canonical} />
      <link rel="alternate" hrefLang="fa" href={canonical} />
      <link rel="alternate" hrefLang="x-default" href={canonical} />
    </Head>
  );
}
