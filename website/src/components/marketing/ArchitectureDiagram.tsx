/**
 * ArchitectureDiagram — the component-layer diagram for the landing page.
 *
 * The four layers (browser, Lahijan backend, PostgreSQL, services) render as
 * a ruled stack: one collapsed column where each row carries a mono layer
 * tag, a 20px line icon, the title and a one-line description. Requests flow
 * top to bottom. Source: docs/architecture/overview.md (component diagram).
 *
 * Per pillar 1 (Transparent infrastructure), the backends are referred to
 * only by their user-facing names ("compute · DNS · object storage").
 */
import { ArrowDownIcon, BoxesIcon, DatabaseIcon, LayoutIcon, ServerIcon } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { useFormatNumber } from "@/lib/format";

interface DiagramLayer {
  icon: LucideIcon;
  key: "browser" | "backend" | "database" | "backends";
}

const LAYERS: readonly DiagramLayer[] = [
  { icon: LayoutIcon, key: "browser" },
  { icon: ServerIcon, key: "backend" },
  { icon: DatabaseIcon, key: "database" },
  { icon: BoxesIcon, key: "backends" },
];

export function ArchitectureDiagram() {
  const { t } = useTranslation();
  const fmt = useFormatNumber();

  return (
    <Section id="architecture" containerClassName="grid grid-cols-1 gap-16 lg:grid-cols-12">
      <div className="lg:col-span-5">
        <div className="lg:sticky lg:top-24">
          <SectionHeading
            eyebrow={t("architecture.eyebrow")}
            title={t("architecture.sectionTitle")}
            subtitle={t("architecture.sectionSubtitle")}
          />
        </div>
      </div>
      <ol
        role="list"
        className="bx-collapse grid-cols-1 border border-line lg:col-span-7"
        aria-label={t("architecture.sectionTitle")}
      >
        {LAYERS.map(({ icon: Icon, key }, idx) => (
          <li key={key} className="grid grid-cols-[auto_1fr] gap-x-6 p-6 md:p-8">
            <span className="flex h-10 w-10 items-center justify-center border border-line bg-surface-sunken text-ink">
              <Icon className="h-5 w-5" strokeWidth={1.5} aria-hidden="true" />
            </span>
            <div>
              <p className="bx-label">
                {fmt(idx + 1, 2)} / {t(`architecture.tags.${key}`)}
              </p>
              <h3 className="mt-2 text-xl">{t(`architecture.${key}`)}</h3>
              <p className="mt-2 text-base text-muted-foreground">{t(`architecture.${key}Body`)}</p>
            </div>
            {idx < LAYERS.length - 1 ? (
              <ArrowDownIcon
                className="col-start-1 mt-4 h-4 w-4 justify-self-center text-ink-faint"
                aria-hidden="true"
              />
            ) : null}
          </li>
        ))}
      </ol>
    </Section>
  );
}
