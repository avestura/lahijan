/**
 * ArchitectureDiagram — visual component-layer diagram for the landing page.
 *
 * Renders the four layers (browser, Lahijan backend, PostgreSQL, backends)
 * as stacked cards with arrows between them. Source: docs/architecture/
 * overview.md (component diagram), translated into visual form.
 *
 * Per pillar 1 (Transparent infrastructure), the backends are referred to
 * only by their user-facing names ("compute · DNS · object storage"). The
 * names "Incus", "PowerDNS", "SeaweedFS" never appear here.
 */
import { ArrowDownIcon, DatabaseIcon, LayoutIcon, ServerIcon, BoxesIcon } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { cn } from "@/lib/utils";

interface DiagramLayer {
  icon: LucideIcon;
  titleKey: string;
  bodyKey: string;
}

const LAYERS: readonly DiagramLayer[] = [
  { icon: LayoutIcon, titleKey: "architecture.browser", bodyKey: "architecture.browserBody" },
  { icon: ServerIcon, titleKey: "architecture.backend", bodyKey: "architecture.backendBody" },
  { icon: DatabaseIcon, titleKey: "architecture.database", bodyKey: "architecture.databaseBody" },
  { icon: BoxesIcon, titleKey: "architecture.backends", bodyKey: "architecture.backendsBody" },
];

export function ArchitectureDiagram() {
  const { t } = useTranslation();

  return (
    <Section id="architecture" className="bg-muted/30">
      <SectionHeading
        title={t("architecture.sectionTitle")}
        subtitle={t("architecture.sectionSubtitle")}
      />
      <ol
        role="list"
        className="mx-auto mt-12 flex max-w-3xl flex-col items-stretch gap-4"
        aria-label={t("architecture.sectionTitle")}
      >
        {LAYERS.map(({ icon: Icon, titleKey, bodyKey }, idx) => (
          <li key={titleKey}>
            <div
              className={cn(
                "flex items-start gap-4 rounded-xl border border-border bg-card p-5 shadow-sm",
                "animate-fade-in-up",
              )}
            >
              <span className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
                <Icon className="h-5 w-5" />
              </span>
              <div className="space-y-1">
                <h3 className="text-base font-semibold text-foreground">{t(titleKey)}</h3>
                <p className="text-sm text-muted-foreground">{t(bodyKey)}</p>
              </div>
            </div>
            {idx < LAYERS.length - 1 ? (
              <div aria-hidden="true" className="my-1 flex justify-center text-muted-foreground">
                <ArrowDownIcon className="h-5 w-5" />
              </div>
            ) : null}
          </li>
        ))}
      </ol>
    </Section>
  );
}
