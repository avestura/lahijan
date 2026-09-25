/**
 * CTASection — the page's one inverted block, with the two closing actions:
 * "Self-host Lahijan" and "Read the docs".
 *
 * Uses the `.bx-inverse` scope from boxy.css, which remaps the role tokens
 * inside itself, so the ordinary utilities (`text-foreground`,
 * `text-muted-foreground`, `border-line`) and the `contrast` / `outline`
 * buttons render correctly on the dark block in both themes. This is the
 * editorial substitute for a gradient banner. Render at most one per page.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { Button, ButtonGroup } from "@/components/ui/button";
import { DOCS_PATH, INSTALL_PATH } from "@/lib/site";
import { cn } from "@/lib/utils";

interface CTASectionProps {
  /** Tighter block padding for inner pages. */
  compact?: boolean;
}

export function CTASection({ compact = false }: CTASectionProps) {
  const { t } = useTranslation();

  return (
    <section className="bx-inverse border-b border-line">
      <div
        className={cn(
          "site-rail flex flex-col items-start justify-between gap-12 lg:flex-row lg:items-end",
          compact ? "py-20 md:py-24" : "py-24 md:py-32",
        )}
      >
        <div>
          <p className="bx-label">{t("finalCta.eyebrow")}</p>
          <h2
            className={cn(
              "mt-3 max-w-[20ch] text-foreground",
              compact ? "text-3xl md:text-4xl" : "site-subdisplay",
            )}
          >
            {t("finalCta.title")}
          </h2>
          <p className="mt-5 max-w-[48ch] text-lg text-muted-foreground">
            {t("finalCta.subtitle")}
          </p>
          <p className="mt-3 text-sm text-muted-foreground">{t("finalCta.note")}</p>
        </div>
        <ButtonGroup className="shrink-0">
          <Button asChild size="lg" variant="contrast">
            <Link to={INSTALL_PATH}>{t("finalCta.primary")}</Link>
          </Button>
          <Button asChild size="lg" variant="outline">
            <Link to={DOCS_PATH}>{t("finalCta.secondary")}</Link>
          </Button>
        </ButtonGroup>
      </div>
    </section>
  );
}
