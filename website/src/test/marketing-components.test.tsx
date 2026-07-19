/**
 * Marketing component smoke tests.
 *
 * Renders the heavy-hitters (Hero, FeatureGrid, FAQ, Footer, Header,
 * PricingTable) and asserts that their translated strings appear in the
 * DOM. Catches regressions where a component forgets to call `t()` or
 * uses an absent key.
 */
import { render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { describe, expect, it, vi } from "vitest";

import { FAQ } from "@/components/marketing/FAQ";
import { FeatureGrid } from "@/components/marketing/FeatureGrid";
import { PricingTable } from "@/components/marketing/PricingTable";
import { Footer } from "@/components/layout/Footer";
import { Header } from "@/components/layout/Header";
import { Hero } from "@/components/marketing/Hero";

// vite-react-ssg's <Head> writes to <head> via react-helmet-async at build
// time; in jsdom we just need it to render its children (or nothing).
vi.mock("vite-react-ssg", () => ({
  Head: ({ children }: { children?: ReactNode }) => (children ? <>{children}</> : null),
}));

describe("Hero", () => {
  it("renders the translated headline and subtitle", () => {
    render(<Hero />);
    const t = en();
    expect(screen.getByText(t("hero.title"))).toBeInTheDocument();
    expect(screen.getByText(t("hero.subtitle"))).toBeInTheDocument();
  });

  it("renders the docker compose command from the locale bundle", () => {
    render(<Hero />);
    const t = en();
    expect(screen.getByText(t("codeSnippet.command"))).toBeInTheDocument();
  });
});

describe("FeatureGrid", () => {
  it("renders all six feature cards with their translated titles", () => {
    render(<FeatureGrid />);
    const t = en();
    const keys = ["compute", "dns", "storage", "plugins", "rbac", "billing"] as const;
    for (const k of keys) {
      expect(screen.getByText(t(`features.${k}.title`))).toBeInTheDocument();
    }
  });
});

describe("FAQ", () => {
  it("renders every question in the locale bundle", () => {
    render(<FAQ />);
    const t = en();
    const keys = ["selfhost", "backends", "multiTenant", "pricing", "plugins", "sdk"] as const;
    for (const k of keys) {
      expect(screen.getByText(t(`faq.items.${k}.q`))).toBeInTheDocument();
    }
  });
});

describe("PricingTable", () => {
  it("lists the three pricing categories by their translated names", () => {
    render(<PricingTable />);
    const t = en();
    expect(screen.getByText(t("pricing.items.compute.name"))).toBeInTheDocument();
    expect(screen.getByText(t("pricing.items.dns.name"))).toBeInTheDocument();
    expect(screen.getByText(t("pricing.items.storage.name"))).toBeInTheDocument();
  });
});

describe("Header", () => {
  it("renders a Sign-in CTA that deep-links to the dashboard", () => {
    render(<Header />);
    const t = en();
    const link = screen.getByRole("link", { name: t("cta.signIn") });
    expect(link).toHaveAttribute("href", "/web/");
  });
});

describe("Footer", () => {
  it("renders the privacy + terms legal links", () => {
    render(<Footer />);
    const t = en();
    // Look only inside the legal column to avoid matches in other columns.
    const navs = screen.getAllByRole("navigation");
    const legalNav = navs.find((n) => within(n).queryByText(t("footer.legal"))) ?? navs[0]!;
    expect(within(legalNav).getByText(t("nav.privacy"))).toBeInTheDocument();
    expect(within(legalNav).getByText(t("nav.terms"))).toBeInTheDocument();
  });
});

/**
 * Helper: call useTranslation outside React render via a one-off probe
 * component. Returns the `t` function bound to the shared default instance
 * (initialized in src/test/setup.ts with "en").
 */
function en(): ReturnType<typeof useTranslation>["t"] {
  let translator: ReturnType<typeof useTranslation>["t"] | null = null;
  function Probe() {
    const { t } = useTranslation();
    translator = t;
    return null;
  }
  render(<Probe />);
  if (!translator) throw new Error("useTranslation did not run inside the probe");
  return translator;
}
