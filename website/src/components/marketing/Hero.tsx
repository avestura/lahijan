/**
 * Hero — top-of-page hero block for the landing page.
 *
 * Renders the project badge, headline, subtitle, and the primary + secondary
 * CTAs. The primary CTA ("Self-host Lahijan") jumps to the architecture /
 * how-it-works section; the secondary CTA scrolls to features.
 *
 * Below the fold: a "Get started in one command" code snippet.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { CodeSnippet } from "@/components/marketing/CodeSnippet";
import { Button } from "@/components/ui/button";

export function Hero() {
  const { t } = useTranslation();

  return (
    <section className="relative overflow-hidden border-b border-border bg-gradient-to-b from-background to-accent/30">
      <div className="container py-20 md:py-28">
        <div className="mx-auto flex max-w-3xl flex-col items-center text-center">
          <span className="inline-flex items-center gap-2 rounded-full border border-border bg-card px-3 py-1 text-xs font-medium text-muted-foreground shadow-sm">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-75" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-success" />
            </span>
            {t("hero.badge")}
          </span>

          <h1 className="mt-6 text-4xl font-extrabold tracking-tight text-foreground md:text-6xl">
            {t("hero.title")}
          </h1>

          <p className="mt-6 max-w-2xl text-lg text-muted-foreground md:text-xl">
            {t("hero.subtitle")}
          </p>

          <div className="mt-8 flex w-full flex-col gap-3 sm:w-auto sm:flex-row sm:items-center sm:justify-center">
            <Button asChild size="lg">
              <a href="#how-it-works">{t("hero.primary")}</a>
            </Button>
            <Button asChild size="lg" variant="outline">
              <a href="#features">{t("hero.secondary")}</a>
            </Button>
          </div>

          <div className="mt-12 w-full">
            <CodeSnippet />
          </div>
        </div>
      </div>
    </section>
  );
}

/** A link-style button used in CTA sections; reroute through <Link>. */
export function CtaLinkButton({
  to,
  label,
  variant = "default",
}: {
  to: string;
  label: string;
  variant?: "default" | "outline" | "secondary" | "ghost";
}) {
  void useTranslation();
  if (to.startsWith("http") || to.startsWith("/web")) {
    return (
      <Button asChild size="lg" variant={variant}>
        <a href={to}>{label}</a>
      </Button>
    );
  }
  return (
    <Button asChild size="lg" variant={variant}>
      <Link to={to}>{label}</Link>
    </Button>
  );
}
