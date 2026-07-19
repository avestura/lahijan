/**
 * Landing page (`/`).
 *
 * The marketing site's front door. Composes the Hero, FeatureGrid,
 * HowItWorks, ArchitectureDiagram, PricingTable, Testimonials, FAQ, and
 * final CTA sections into one scrollable page. Each section has a stable
 * id so the hero's anchor CTAs and the sitemap's deep links work.
 */
import { ArchitectureDiagram } from "@/components/marketing/ArchitectureDiagram";
import { CTASection } from "@/components/marketing/CTASection";
import { FAQ } from "@/components/marketing/FAQ";
import { FeatureGrid } from "@/components/marketing/FeatureGrid";
import { Hero } from "@/components/marketing/Hero";
import { HowItWorks } from "@/components/marketing/HowItWorks";
import { PricingTable } from "@/components/marketing/PricingTable";
import { Testimonials } from "@/components/marketing/Testimonials";
import { SeoHead } from "@/components/seo/SeoHead";

export function LandingPage() {
  return (
    <>
      <SeoHead titleKey="app.tagline" descriptionKey="app.description" path="/" />
      <Hero />
      <FeatureGrid />
      <HowItWorks />
      <ArchitectureDiagram />
      <PricingTable />
      <Testimonials />
      <FAQ />
      <CTASection />
    </>
  );
}
