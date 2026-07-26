/**
 * BillingBalanceCard — prominent balance readout + a compact recent
 * ledger table. The balance is the single number users care about; the
 * table gives the last few movements (top-ups / charges / refunds).
 */
import { useTranslation } from "react-i18next";
import { ArrowDownRightIcon, ArrowUpRightIcon } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { FeatureDisabledState } from "@/components/layout/FeatureDisabledState";
import { useMyBalance, useMyLedger } from "@/features/billing/api";
import { isFeatureDisabledError } from "@/lib/api-errors";
import { formatCurrency } from "@/features/dashboard/format";

export function BillingBalanceCard() {
  const { t } = useTranslation();
  const balance = useMyBalance();
  const ledger = useMyLedger(0, 5);

  const disabled = isFeatureDisabledError(balance.error) || isFeatureDisabledError(ledger.error);
  if (disabled) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("billing.user.balance.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          <FeatureDisabledState
            title={t("common.featureDisabled.title")}
            description={t("common.featureDisabled.description")}
          />
        </CardContent>
      </Card>
    );
  }

  const recent = (ledger.data?.items ?? []).slice(0, 5);
  const isCredit = (s: string) => s === "credit";

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("dashboard.charts.balance")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-1">
          {balance.isLoading ? (
            <Skeleton className="h-9 w-32" />
          ) : (
            <p
              className="text-3xl font-semibold tabular-nums"
              data-testid="dashboard-balance-value"
            >
              {balance.data
                ? formatCurrency(balance.data.balanceCents, balance.data.currency)
                : "—"}
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            {balance.data
              ? t("billing.user.balance.updated", {
                  at: new Date(balance.data.updatedAt).toLocaleString(),
                })
              : ""}
          </p>
        </div>

        <div className="space-y-1">
          <p className="text-xs uppercase tracking-wider text-muted-foreground">
            {t("billing.user.ledger.title")}
          </p>
          {ledger.isLoading ? (
            <Skeleton className="h-24 w-full" />
          ) : recent.length === 0 ? (
            <p className="py-3 text-sm text-muted-foreground">
              {t("billing.user.ledger.empty.body")}
            </p>
          ) : (
            <ul className="divide-y divide-border">
              {recent.map((e) => {
                const credit = isCredit(e.type);
                return (
                  <li key={e.id} className="flex items-center gap-3 py-2">
                    <span
                      className={
                        "flex h-7 w-7 shrink-0 items-center justify-center rounded-full " +
                        (credit
                          ? "bg-success/10 text-success"
                          : "bg-destructive/10 text-destructive")
                      }
                      aria-hidden="true"
                    >
                      {credit ? (
                        <ArrowDownRightIcon className="h-4 w-4" />
                      ) : (
                        <ArrowUpRightIcon className="h-4 w-4" />
                      )}
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">
                        {t(`billing.user.ledger.source.${e.source}`)}
                      </p>
                      <p className="truncate text-xs text-muted-foreground">
                        {new Date(e.createdAt).toLocaleString()}
                      </p>
                    </div>
                    <span
                      className={
                        "shrink-0 text-sm font-semibold tabular-nums " +
                        (credit ? "text-success" : "text-destructive")
                      }
                    >
                      {credit ? "+" : "−"}
                      {formatCurrency(e.amountCents, e.currency)}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
