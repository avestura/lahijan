/**
 * AdminPricesCard — admin price catalog with upsert.
 *
 * Per WS-17: every upsert emits an audit event. The UI gates the
 * action behind billing.price_catalog.update.
 */
import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { PlusIcon, TagIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
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
import { useAdminPrices, useUpsertAdminPrice } from "../api";
import { formatMoney, priceUpsertSchema, type PriceUpsertValues } from "../schemas";

export function AdminPricesCard() {
  const { t } = useTranslation();
  const query = useAdminPrices();
  const { hasPerm } = usePerm("billing.price_catalog.update");
  const [open, setOpen] = React.useState(false);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div className="space-y-1">
          <CardTitle className="text-base">{t("billing.admin.prices.title")}</CardTitle>
          <p className="text-xs text-muted-foreground">{t("billing.admin.prices.subtitle")}</p>
        </div>
        {hasPerm && (
          <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
            <PlusIcon className="h-4 w-4" />
            {t("billing.admin.prices.new")}
          </Button>
        )}
      </CardHeader>
      <CardContent>
        {query.isLoading ? (
          <LoadingState rows={4} />
        ) : query.error ? (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        ) : (query.data ?? []).length === 0 ? (
          <EmptyState
            icon={TagIcon}
            title={t("billing.admin.prices.empty.title")}
            description={t("billing.admin.prices.empty.body")}
          />
        ) : (
          <div className="border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("billing.admin.prices.columns.resourceType")}</TableHead>
                  <TableHead>{t("billing.admin.prices.columns.unit")}</TableHead>
                  <TableHead>{t("billing.admin.prices.columns.priceCents")}</TableHead>
                  <TableHead>{t("billing.admin.prices.columns.currency")}</TableHead>
                  <TableHead>{t("billing.admin.prices.columns.effectiveFrom")}</TableHead>
                  <TableHead>{t("billing.admin.prices.columns.effectiveTo")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(query.data ?? []).map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="font-mono text-xs">{p.resourceType}</TableCell>
                    <TableCell className="text-xs">{p.unit}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {formatMoney(p.priceCents, p.currency)}
                    </TableCell>
                    <TableCell className="text-xs">{p.currency}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(p.effectiveFrom).toLocaleDateString()}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {p.effectiveTo ? new Date(p.effectiveTo).toLocaleDateString() : "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      <UpsertPriceDialog open={open} onOpenChange={setOpen} />
    </Card>
  );
}

function UpsertPriceDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const upsert = useUpsertAdminPrice();

  const form = useForm<PriceUpsertValues>({
    resolver: zodResolver(priceUpsertSchema),
    defaultValues: {
      resourceType: "",
      unit: "",
      priceCents: 0,
      currency: "USD",
      effectiveFrom: undefined,
    },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await upsert.mutateAsync(values);
    onOpenChange(false);
    form.reset({
      resourceType: "",
      unit: "",
      priceCents: 0,
      currency: "USD",
      effectiveFrom: undefined,
    });
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("billing.admin.prices.upsert.title")}</DialogTitle>
          <DialogDescription>{t("billing.admin.prices.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="price-rt">{t("billing.admin.prices.upsert.resourceType.label")}</Label>
            <Input
              id="price-rt"
              placeholder={t("billing.admin.prices.upsert.resourceType.placeholder")}
              {...form.register("resourceType")}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="price-unit">{t("billing.admin.prices.upsert.unit.label")}</Label>
            <Input
              id="price-unit"
              placeholder={t("billing.admin.prices.upsert.unit.placeholder")}
              {...form.register("unit")}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <Label htmlFor="price-amount">
                {t("billing.admin.prices.upsert.priceCents.label")}
              </Label>
              <Input
                id="price-amount"
                type="number"
                min={0}
                {...form.register("priceCents", { valueAsNumber: true })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="price-currency">
                {t("billing.admin.prices.upsert.currency.label")}
              </Label>
              <Input id="price-currency" defaultValue="USD" {...form.register("currency")} />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="price-from">
              {t("billing.admin.prices.upsert.effectiveFrom.label")}
            </Label>
            <Input id="price-from" type="datetime-local" {...form.register("effectiveFrom")} />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={upsert.isPending}>
              {upsert.isPending
                ? t("billing.admin.prices.upsert.submitting")
                : t("billing.admin.prices.upsert.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
