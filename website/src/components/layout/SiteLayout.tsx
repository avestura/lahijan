/**
 * SiteLayout — the chrome that wraps every marketing page.
 *
 * Renders the Header, the routed page (via <Outlet />, or `children` when
 * used as the router's error element), the Footer, and boots analytics.
 * Per-page <head> tags are emitted by each page via the <SeoHead> helper
 * (which writes through vite-react-ssg's <Head>).
 *
 * Boxy mode: every marketing route is `editorial` (big display type,
 * 128-160px section rhythm, collapsed grids). The dashboard in web/ uses
 * `industrial`; the two never mix within a page. index.html also sets
 * data-mode on <html> so the prerendered markup has it before hydration.
 */
import { Outlet, ScrollRestoration } from "react-router-dom";

import { Footer } from "@/components/layout/Footer";
import { Header } from "@/components/layout/Header";
import { useAnalytics } from "@/lib/analytics";

export function SiteLayout({ children }: { children?: React.ReactNode }) {
  useAnalytics();
  return (
    <div data-mode="editorial" className="flex min-h-screen flex-col bg-background text-foreground">
      <Header />
      <main id="main" className="flex-1">
        {children ?? <Outlet />}
      </main>
      <Footer />
      <ScrollRestoration />
    </div>
  );
}
