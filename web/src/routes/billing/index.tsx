/**
 * /billing — user-facing billing overview.
 *
 * Top: Balance card. Middle: Usage chart + Receipts list.
 * Bottom: Ledger table.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { BalanceCard } from "@/features/billing/components/BalanceCard";
import { UsagePanel } from "@/features/billing/components/UsagePanel";
import { LedgerTable } from "@/features/billing/components/LedgerTable";
import { ReceiptsCard } from "@/features/billing/components/ReceiptsCard";

export const Route = createFileRoute("/billing/")({
  component: BillingPage,
});

function BillingPage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("billing.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("billing.subtitle")}</p>
      </header>
      <BalanceCard />
      <div className="grid gap-4 md:grid-cols-2">
        <UsagePanel />
        <ReceiptsCard />
      </div>
      <LedgerTable />
    </div>
  );
}
