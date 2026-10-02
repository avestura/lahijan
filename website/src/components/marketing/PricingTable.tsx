/**
 * PricingTable — what Lahijan meters, with no prices.
 *
 * Lahijan is self-hosted and has no hosted offering, so the marketing site
 * never names a price: each operator sets their own catalog. This grid only
 * shows the unit each service is metered in.
 *
 * Boxy pricing: a collapsed grid that is literally a table. Each column has
 * the mono service label, the metered unit in large mono, and the billing
 * rule.
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";

interface PriceRow {
  itemKey: "compute" | "dns" | "storage";
  unitKey: string;
}

const ROWS: readonly PriceRow[] = [
  { itemKey: "compute", unitKey: "pricing.unit.cpu" },
  { itemKey: "dns", unitKey: "pricing.unit.zone" },
  { itemKey: "storage", unitKey: "pricing.unit.storage" },
];

export function PricingTable() {
  const { t } = useTranslation();

  return (
    <Section id="pricing">
      <SectionHeading
        eyebrow={t("pricing.eyebrow")}
        title={t("pricing.sectionTitle")}
        subtitle={t("pricing.sectionSubtitle")}
      />

      <dl className="site-bleed bx-collapse mt-16 grid-cols-1 border-y border-line md:grid-cols-3">
        {ROWS.map(({ itemKey, unitKey }) => (
          <div key={itemKey} className="flex flex-col px-4 py-10 md:px-8">
            <dt className="bx-label">{t(`pricing.items.${itemKey}.name`)}</dt>
            <dd className="mt-6 flex flex-wrap items-baseline gap-x-2">
              <span className="bx-mono text-2xl font-medium text-foreground">{t(unitKey)}</span>
            </dd>
            <dd className="mt-6 border-t border-line-subtle pt-6 text-base text-muted-foreground">
              {t(`pricing.items.${itemKey}.body`)}
            </dd>
          </div>
        ))}
      </dl>

      <p className="mt-6 text-sm text-ink-subtle">{t("pricing.disclaimer")}</p>
    </Section>
  );
}
