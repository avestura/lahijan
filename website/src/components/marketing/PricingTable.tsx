/**
 * PricingTable — illustrative unit prices for the marketing site.
 *
 * Per WS-19 open question 2, prices on the marketing site are hard-coded
 * (the dashboard pulls the live catalog). Each deployment's operator sets
 * their own catalog; this is a sample.
 *
 * Boxy pricing: a collapsed grid that is literally a table. Each column has
 * the mono service label, the price in large tabular mono with its unit in
 * small subtle ink, and the billing rule. A full-width worked-example row
 * closes the grid. No plan is "recommended" (there are no plans, only
 * metered units), so nothing is inverted here.
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";

interface PriceRow {
  itemKey: "compute" | "dns" | "storage";
  unitKey: string;
  price: string;
}

const ROWS: readonly PriceRow[] = [
  { itemKey: "compute", unitKey: "pricing.unit.cpu", price: "$0.012" },
  { itemKey: "dns", unitKey: "pricing.unit.zone", price: "$0.50" },
  { itemKey: "storage", unitKey: "pricing.unit.storage", price: "$0.02" },
];

// Worked-example monthly cost at the sample rates for a small instance
// (1 vCPU, 2 GB RAM, 20 GB disk).
const SAMPLE_INSTANCE_MONTHLY = "$10.40";

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
        {ROWS.map(({ itemKey, unitKey, price }) => (
          <div key={itemKey} className="flex flex-col px-4 py-10 md:px-8">
            <dt className="bx-label">{t(`pricing.items.${itemKey}.name`)}</dt>
            <dd className="mt-6 flex flex-wrap items-baseline gap-x-2">
              <span dir="ltr" className="bx-mono text-4xl font-medium text-foreground">
                {price}
              </span>
              <span className="text-base text-ink-subtle">{t(unitKey)}</span>
            </dd>
            <dd className="mt-6 border-t border-line-subtle pt-6 text-base text-muted-foreground">
              {t(`pricing.items.${itemKey}.body`)}
            </dd>
          </div>
        ))}
        <div className="bg-surface-sunken px-4 py-8 md:col-span-3 md:px-8">
          <dt className="bx-label">{t("pricing.example.title")}</dt>
          <dd className="mt-3 max-w-[72ch] text-md text-foreground">
            {t("pricing.example.body", { amount: SAMPLE_INSTANCE_MONTHLY })}
          </dd>
        </div>
      </dl>

      <p className="mt-6 text-sm text-ink-subtle">{t("pricing.disclaimer")}</p>
    </Section>
  );
}
