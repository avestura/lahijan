/**
 * BalanceCard — cached per-user balance banner.
 */
import { useTranslation } from "react-i18next";
import { WalletIcon } from "lucide-react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { useMyBalance } from "../api";
import { formatMoney } from "../schemas";

export function BalanceCard() {
  const { t } = useTranslation();
  const query = useMyBalance();

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2 text-base">
          <WalletIcon className="h-4 w-4" />
          {t("billing.user.balance.title")}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {query.isLoading ? (
          <LoadingState rows={1} />
        ) : query.error ? (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        ) : query.data ? (
          <div className="space-y-1">
            <p className="font-mono text-3xl font-semibold">
              {formatMoney(query.data.balanceCents, query.data.currency)}
            </p>
            <CardDescription>
              {t("billing.user.balance.lastEntry", {
                at: new Date(query.data.lastEntryAt).toLocaleString(),
              })}
            </CardDescription>
            <p className="text-xs text-muted-foreground">
              {t("billing.user.balance.updated", {
                at: new Date(query.data.updatedAt).toLocaleString(),
              })}
            </p>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
