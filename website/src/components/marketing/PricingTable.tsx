/**
 * PricingTable — illustrative unit-price table for the marketing site.
 *
 * Per WS-19 open question 2, prices on the marketing site are hard-coded
 * (the dashboard pulls the live catalog). Each deployment's operator sets
 * their own catalog; this is a sample.
 *
 * Each row shows a unit price (e.g. "$0.012 / vCPU-hour"). The `currency`
 * prop lets deployers swap the symbol via env later; for MVP we hard-code
 * USD ($).
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface PriceRow {
  itemKey: string;
  nameKey: string;
  unitKey: string;
  price: string;
}

const ROWS: readonly PriceRow[] = [
  {
    itemKey: "compute",
    nameKey: "pricing.items.compute.name",
    unitKey: "pricing.unit.cpu",
    price: "$0.012",
  },
  {
    itemKey: "dns",
    nameKey: "pricing.items.dns.name",
    unitKey: "pricing.unit.zone",
    price: "$0.50",
  },
  {
    itemKey: "storage",
    nameKey: "pricing.items.storage.name",
    unitKey: "pricing.unit.storage",
    price: "$0.02",
  },
];

// Worked-example monthly cost at the sample rates for a small instance
// (1 vCPU, 2 GB RAM, 20 GB disk). Pre-computed at module load.
const SAMPLE_INSTANCE_MONTHLY = "$10.40";

export function PricingTable() {
  const { t } = useTranslation();

  return (
    <Section id="pricing" className="bg-muted/30">
      <SectionHeading title={t("pricing.sectionTitle")} subtitle={t("pricing.sectionSubtitle")} />

      <Card className="mx-auto mt-12 max-w-3xl overflow-hidden">
        <CardHeader>
          <CardTitle className="text-lg">{t("pricing.sectionTitle")}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <table className="w-full border-collapse text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/50 text-start">
                <th scope="col" className="p-4 text-start font-semibold text-foreground">
                  {t("common.name")}
                </th>
                <th scope="col" className="p-4 text-start font-semibold text-foreground">
                  {t("common.description")}
                </th>
                <th scope="col" className="p-4 text-end font-semibold text-foreground">
                  {t("common.price")}
                </th>
              </tr>
            </thead>
            <tbody>
              {ROWS.map(({ itemKey, nameKey, unitKey, price }) => (
                <tr key={itemKey} className="border-b border-border last:border-b-0">
                  <td className="p-4 align-top font-medium text-foreground">{t(nameKey)}</td>
                  <td className="p-4 align-top text-muted-foreground">
                    <p>{t(`pricing.items.${itemKey}.body`)}</p>
                    <p className="mt-1 text-xs">{t(unitKey)}</p>
                  </td>
                  <td className="p-4 text-end align-top font-mono font-semibold text-foreground">
                    {price}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>

      <p className="mx-auto mt-4 max-w-3xl text-center text-xs text-muted-foreground">
        {t("pricing.disclaimer")}
      </p>

      <div className="mx-auto mt-8 max-w-3xl rounded-lg border border-border bg-card p-5 text-sm text-muted-foreground shadow-sm">
        <p className="font-semibold text-foreground">{t("pricing.example.title")}</p>
        <p className="mt-2">{t("pricing.example.body", { amount: SAMPLE_INSTANCE_MONTHLY })}</p>
      </div>
    </Section>
  );
}
