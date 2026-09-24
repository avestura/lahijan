/* boxy-ignore-file: this test lists the banned utilities on purpose. */
/**
 * Boxy design-system smoke tests.
 *
 * Runtime belt-and-braces for boxy-check: rendered marketing components
 * emit no rounded, blurred-shadow, backdrop-blur or transition-all
 * utilities; the CTA band uses the inverse scope; the layout is editorial.
 */
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { Footer } from "@/components/layout/Footer";
import { Header } from "@/components/layout/Header";
import { SiteLayout } from "@/components/layout/SiteLayout";
import { CTASection } from "@/components/marketing/CTASection";
import { FAQ } from "@/components/marketing/FAQ";
import { FeatureGrid } from "@/components/marketing/FeatureGrid";
import { Hero } from "@/components/marketing/Hero";
import { PricingTable } from "@/components/marketing/PricingTable";

vi.mock("vite-react-ssg", () => ({
  Head: ({ children }: { children?: ReactNode }) => (children ? <>{children}</> : null),
}));

const BANNED =
  /(^|\s)(rounded(-(?!none\b)\S+)?|shadow(-(sm|md|lg|xl|2xl|inner))?|backdrop-blur\S*|transition-all|bg-gradient\S*)(?=\s|$)/;

describe("Boxy geometry", () => {
  it.each([
    ["Hero", <Hero key="h" />],
    ["FeatureGrid", <FeatureGrid key="f" />],
    ["PricingTable", <PricingTable key="p" />],
    ["FAQ", <FAQ key="q" />],
    ["CTASection", <CTASection key="c" />],
    ["Header", <Header key="hd" />],
    ["Footer", <Footer key="ft" />],
  ])("%s emits no rounded / blurred / gradient utilities", (_name, el) => {
    const { container } = render(el);
    const offenders: string[] = [];
    for (const node of container.querySelectorAll("[class]")) {
      const m = BANNED.exec(node.getAttribute("class") ?? "");
      if (m) offenders.push(`${m[2]} on <${node.tagName.toLowerCase()}>`);
    }
    expect(offenders).toEqual([]);
  });

  it("the CTA band is the inverse scope", () => {
    const { container } = render(<CTASection />);
    expect(container.querySelector("section")).toHaveClass("bx-inverse");
  });

  it("the site layout is in editorial mode", () => {
    const { container } = render(
      <SiteLayout>
        <p>content</p>
      </SiteLayout>,
    );
    expect(container.querySelector("[data-mode]")).toHaveAttribute("data-mode", "editorial");
  });
});
