/**
 * react-router-dom route configuration for the marketing site.
 *
 * Object-based routes consumed by vite-react-ssg. The SiteLayout is the
 * single parent route; each page is an index or child path under it. The
 * errorElement renders the NotFoundPage (inside the same site chrome) for
 * any unmatched path.
 */
import type { RouteObject } from "react-router-dom";

import { SiteLayout } from "@/components/layout/SiteLayout";
import { DOCS_STATIC_PATHS, docsRoute } from "@/docs/routes";
import { AboutPage } from "@/pages/AboutPage";
import { BlogListPage } from "@/pages/BlogListPage";
import { BlogPostPage } from "@/pages/BlogPostPage";
import { FeaturesPage } from "@/pages/FeaturesPage";
import { LandingPage } from "@/pages/LandingPage";
import { LegalPrivacyPage } from "@/pages/LegalPrivacyPage";
import { LegalTermsPage } from "@/pages/LegalTermsPage";
import { NotFoundPage } from "@/pages/NotFoundPage";
import { PricingPage } from "@/pages/PricingPage";

export const routes: RouteObject[] = [
  {
    path: "/",
    element: <SiteLayout />,
    errorElement: (
      <SiteLayout>
        <NotFoundPage />
      </SiteLayout>
    ),
    children: [
      { index: true, element: <LandingPage /> },
      { path: "features", element: <FeaturesPage /> },
      { path: "pricing", element: <PricingPage /> },
      { path: "about", element: <AboutPage /> },
      docsRoute,
      { path: "blog", element: <BlogListPage /> },
      { path: "blog/:slug", element: <BlogPostPage /> },
      { path: "legal/privacy", element: <LegalPrivacyPage /> },
      { path: "legal/terms", element: <LegalTermsPage /> },
      // Prerendered so static hosts can serve it as 404.html (scripts/pages-404.mjs).
      { path: "not-found", element: <NotFoundPage /> },
    ],
  },
];

/**
 * Static-path list for vite-react-ssg. Each entry corresponds to a route
 * that should be prerendered to its own `index.html` at build time.
 * Dynamic routes (e.g. /blog/:slug) are not prerendered for MVP since
 * there is no blog content yet; they fall through to client-side routing.
 */
export const STATIC_PATHS: readonly string[] = [
  "/",
  "/features",
  "/pricing",
  "/about",
  ...DOCS_STATIC_PATHS,
  "/blog",
  "/legal/privacy",
  "/legal/terms",
  "/not-found",
];
