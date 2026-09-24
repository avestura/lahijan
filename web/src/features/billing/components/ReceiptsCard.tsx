/**
 * ReceiptsCard — list + on-demand generate + download PDF.
 */
import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { DownloadIcon, FileTextIcon, PlusIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import { downloadReceiptPDF, useGenerateReceipt, useMyReceipts } from "../api";
import { formatMoney, generateReceiptSchema, type GenerateReceiptValues } from "../schemas";

export function ReceiptsCard() {
  const { t } = useTranslation();
  const query = useMyReceipts();
  const generate = useGenerateReceipt();
  const [open, setOpen] = React.useState(false);
  const { hasPerm } = usePerm("billing.receipt.create");

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div className="space-y-1">
          <CardTitle className="text-base">{t("billing.user.receipts.title")}</CardTitle>
          <p className="text-xs text-muted-foreground">{t("billing.user.receipts.subtitle")}</p>
        </div>
        {hasPerm && (
          <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
            <PlusIcon className="h-4 w-4" />
            {t("billing.user.receipts.generate.title")}
          </Button>
        )}
      </CardHeader>
      <CardContent>
        {query.isLoading ? (
          <LoadingState rows={3} />
        ) : query.error ? (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        ) : (query.data ?? []).length === 0 ? (
          <EmptyState
            icon={FileTextIcon}
            title={t("billing.user.receipts.empty.title")}
            description={t("billing.user.receipts.empty.body")}
          />
        ) : (
          <div className="border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("billing.user.receipts.columns.period")}</TableHead>
                  <TableHead>{t("billing.user.receipts.columns.total")}</TableHead>
                  <TableHead>{t("billing.user.receipts.columns.status")}</TableHead>
                  <TableHead className="text-end">
                    {t("billing.user.receipts.columns.actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(query.data ?? []).map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className="text-xs">
                      {new Date(r.periodStart).toLocaleDateString()} —{" "}
                      {new Date(r.periodEnd).toLocaleDateString()}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {formatMoney(r.totalCents, r.currency)}
                    </TableCell>
                    <TableCell>
                      <Badge variant={r.status === "ready" ? "default" : "secondary"}>
                        {t(`billing.user.receipts.status.${r.status}`)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-end">
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={r.status !== "ready"}
                        onClick={() => downloadReceiptPDF(r.id)}
                        aria-label={t("billing.user.receipts.download")}
                      >
                        <DownloadIcon className="h-4 w-4" />
                        {t("billing.user.receipts.download")}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      <GenerateReceiptDialog open={open} onOpenChange={setOpen} generate={generate} />
    </Card>
  );
}

interface GenProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  generate: ReturnType<typeof useGenerateReceipt>;
}

function GenerateReceiptDialog({ open, onOpenChange, generate }: GenProps) {
  const { t } = useTranslation();
  const form = useForm<GenerateReceiptValues>({
    resolver: zodResolver(generateReceiptSchema),
    defaultValues: { periodStart: "", periodEnd: "" },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await generate.mutateAsync(values);
    onOpenChange(false);
    form.reset({ periodStart: "", periodEnd: "" });
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("billing.user.receipts.generate.title")}</DialogTitle>
          <DialogDescription>{t("billing.user.receipts.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="rec-ps">{t("billing.user.receipts.generate.periodStart.label")}</Label>
            <Input id="rec-ps" type="date" {...form.register("periodStart")} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="rec-pe">{t("billing.user.receipts.generate.periodEnd.label")}</Label>
            <Input id="rec-pe" type="date" {...form.register("periodEnd")} />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={generate.isPending}>
              {generate.isPending
                ? t("billing.user.receipts.generate.submitting")
                : t("billing.user.receipts.generate.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
