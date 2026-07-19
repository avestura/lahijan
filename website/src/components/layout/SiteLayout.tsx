/**
 * SiteLayout — the chrome that wraps every marketing page.
 *
 * Renders the Header, the routed page (via <Outlet />), the Footer, and
 * boots analytics. Per-page <head> tags are emitted by each page via the
 * <SeoHead> helper (which writes through vite-react-ssg's <Head>).
 */
import { Outlet, ScrollRestoration } from "react-router-dom";

import { Footer } from "@/components/layout/Footer";
import { Header } from "@/components/layout/Header";
import { useAnalytics } from "@/lib/analytics";

export function SiteLayout() {
  useAnalytics();
  return (
    <div className="flex min-h-screen flex-col">
      <Header />
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      <Footer />
      <ScrollRestoration />
    </div>
  );
}
