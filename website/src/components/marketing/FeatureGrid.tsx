/**
 * FeatureGrid — the feature block used on the landing page and mirrored on
 * /features.
 *
 * Boxy editorial feature grid: one collapsed grid running rail to rail, so
 * the whole section reads as a single ruled table rather than a scatter of
 * floating cards. Three columns at lg (compute and the agent span two to
 * keep every row full), two at md, one below. Each cell: a 20px line icon
 * in ink, a mono index, a mono tag, the title, the body and a link.
 *
 * On the landing page each cell links to its section on /features; on
 * /features itself it links to the in-page detail row.
 *
 * Per pillar 1 (Transparent infrastructure) the cells speak of "compute /
 * DNS / object storage", never the underlying backends' names.
 */
import { ArrowRightIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { FEATURE_DEFS } from "@/components/marketing/feature-defs";
import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { useFormatNumber } from "@/lib/format";
import { cn } from "@/lib/utils";

interface FeatureGridProps {
  /** `page` links cells to the in-page detail rows on /features. */
  variant?: "landing" | "page";
}

export function FeatureGrid({ variant = "landing" }: FeatureGridProps) {
  const { t } = useTranslation();
  const fmt = useFormatNumber();

  return (
    <Section id="features">
      <SectionHeading
        eyebrow={t("features.eyebrow")}
        title={t("features.sectionTitle")}
        subtitle={t("features.sectionSubtitle")}
      />
      <ul
        role="list"
        className="site-bleed bx-collapse mt-16 grid-cols-1 border-y border-line md:grid-cols-2 lg:grid-cols-3"
      >
        {FEATURE_DEFS.map(({ key, icon: Icon, span }, idx) => (
          <li key={key} className={cn(span)}>
            <a
              href={variant === "page" ? `#${key}` : `/features#${key}`}
              className="group flex h-full flex-col px-4 py-10 text-foreground no-underline transition-colors duration-80 ease-linear hover:bg-surface-hover hover:no-underline md:px-8"
            >
              <div className="flex items-start justify-between">
                <Icon className="h-5 w-5 text-ink" strokeWidth={1.5} aria-hidden="true" />
                <span className="bx-mono text-xs text-ink-faint" aria-hidden="true">
                  {fmt(idx + 1, 2)}
                </span>
              </div>
              <p className="bx-label mt-8">{t(`features.${key}.tag`)}</p>
              <h3 className="mt-2 text-xl">{t(`features.${key}.title`)}</h3>
              <p className="mt-3 max-w-[52ch] text-base text-muted-foreground">
                {t(`features.${key}.body`)}
              </p>
              <span className="mt-auto inline-flex items-center gap-2 pt-6 text-base font-medium text-ink-accent group-hover:underline">
                {t("features.learnMore")}
                <ArrowRightIcon className="h-4 w-4 rtl:rotate-180" aria-hidden="true" />
              </span>
            </a>
          </li>
        ))}
      </ul>
    </Section>
  );
}
