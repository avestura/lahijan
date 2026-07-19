/**
 * LedgerTable — paginated ledger entries.
 *
 * Renders the latest page + a "Load more" button. No filter UI —
 * the ledger is append-only so newest-first is all the user needs.
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ReceiptIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { useMyLedger } from "../api";
import { formatMoney } from "../schemas";

const PAGE_SIZE = 25;

export function LedgerTable() {
  const { t } = useTranslation();
  const [offset, setOffset] = useState(0);
  const query = useMyLedger(offset, PAGE_SIZE);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("billing.user.ledger.title")}</CardTitle>
        <p className="text-xs text-muted-foreground">{t("billing.user.ledger.subtitle")}</p>
      </CardHeader>
      <CardContent className="space-y-3">
        {query.isLoading ? (
          <LoadingState rows={5} />
        ) : query.error ? (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        ) : (query.data?.items ?? []).length === 0 ? (
          <EmptyState
            icon={ReceiptIcon}
            title={t("billing.user.ledger.empty.title")}
            description={t("billing.user.ledger.empty.body")}
          />
        ) : (
          <>
            <div className="rounded-md border border-border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("billing.user.ledger.columns.type")}</TableHead>
                    <TableHead>{t("billing.user.ledger.columns.source")}</TableHead>
                    <TableHead>{t("billing.user.ledger.columns.amount")}</TableHead>
                    <TableHead>{t("billing.user.ledger.columns.reference")}</TableHead>
                    <TableHead>{t("billing.user.ledger.columns.created")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(query.data?.items ?? []).map((row) => (
                    <TableRow key={row.id}>
                      <TableCell>
                        <Badge
                          variant={row.type === "credit" ? "default" : "secondary"}
                        >
                          {t(`billing.user.ledger.type.${row.type}`)}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs">
                        {t(`billing.user.ledger.source.${row.source}`)}
                      </TableCell>
                      <TableCell
                        className={`font-mono text-xs ${
                          row.type === "credit" ? "text-primary" : "text-destructive"
                        }`}
                      >
                        {row.type === "credit" ? "+" : "−"}
                        {formatMoney(row.amountCents, row.currency)}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {row.reference ?? t("common.none")}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {new Date(row.createdAt).toLocaleString()}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            {offset + PAGE_SIZE < (query.data?.total ?? 0) && (
              <div className="flex justify-end">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setOffset((o) => o + PAGE_SIZE)}
                >
                  {t("billing.user.ledger.loadMore")}
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
