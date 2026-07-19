/**
 * CTASection — bottom-of-page banner with the two primary actions:
 * "Self-host Lahijan" and "Read the docs".
 *
 * Reused on the landing page and on each top-level page in a compact mode.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { Button } from "@/components/ui/button";

interface CTASectionProps {
  /** Render as a slim banner (no eyebrow / less padding). */
  compact?: boolean;
}

export function CTASection({ compact = false }: CTASectionProps) {
  const { t } = useTranslation();

  return (
    <Section className="bg-background" containerClassName={undefined}>
      <div className="mx-auto max-w-4xl overflow-hidden rounded-2xl border border-border bg-gradient-to-br from-primary to-primary/70 p-8 text-center shadow-lg md:p-12">
        <h2
          className={
            compact
              ? "text-2xl font-bold tracking-tight text-primary-foreground"
              : "text-3xl font-bold tracking-tight text-primary-foreground md:text-4xl"
          }
        >
          {t("finalCta.title")}
        </h2>
        <p className="mx-auto mt-4 max-w-2xl text-primary-foreground/90">
          {t("finalCta.subtitle")}
        </p>
        <div className="mt-8 flex flex-col items-center justify-center gap-3 sm:flex-row">
          <Button asChild size="lg" variant="secondary">
            <a href="#how-it-works">{t("finalCta.primary")}</a>
          </Button>
          <Button
            asChild
            size="lg"
            variant="outline"
            className="border-primary-foreground/30 bg-transparent text-primary-foreground hover:bg-primary-foreground/10 hover:text-primary-foreground"
          >
            <Link to="/docs">{t("finalCta.secondary")}</Link>
          </Button>
        </div>
      </div>
    </Section>
  );
}
