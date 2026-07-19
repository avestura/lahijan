/**
 * AdminUserActionsCard — per-user top-up + refund + ledger link.
 *
 * The form gates top-up behind billing.balance.adjust; refunds use
 * the same permission (per WS-17's resolution notes).
 */
import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { ArrowRightIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { usePerm } from "@/lib/perm";
import { useAdminRefundUser, useAdminTopupUser, useAdminUserBalance } from "../api";
import {
  formatMoney,
  refundSchema,
  topupSchema,
  type RefundValues,
  type TopupValues,
} from "../schemas";

interface Props {
  userId: string;
}

export function AdminUserActionsCard({ userId }: Props) {
  const { t } = useTranslation();
  const balance = useAdminUserBalance(userId);
  const topup = useAdminTopupUser(userId);
  const refund = useAdminRefundUser(userId);
  const navigate = useNavigate();
  const { hasPerm } = usePerm("billing.balance.adjust");
  const [topupOpen, setTopupOpen] = React.useState(false);
  const [refundOpen, setRefundOpen] = React.useState(false);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("billing.admin.users.title")}</CardTitle>
        <p className="text-xs text-muted-foreground">{t("billing.admin.users.subtitle")}</p>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">{t("billing.admin.users.columns.balance")}</span>
          <span className="font-mono text-xs">
            {balance.data ? formatMoney(balance.data.balanceCents, balance.data.currency) : "—"}
          </span>
        </div>
        <div className="flex flex-wrap gap-2">
          {hasPerm && (
            <>
              <Button size="sm" onClick={() => setTopupOpen(true)}>
                {t("billing.admin.users.topup.submit")}
              </Button>
              <Button size="sm" variant="outline" onClick={() => setRefundOpen(true)}>
                {t("billing.admin.users.refund.submit")}
              </Button>
            </>
          )}
          <Button
            size="sm"
            variant="ghost"
            onClick={() => navigate({ to: "/admin/billing/users/$userId", params: { userId } })}
          >
            {t("billing.admin.users.ledger.title")}
            <ArrowRightIcon className="h-4 w-4" />
          </Button>
        </div>
      </CardContent>

      <TopupDialog open={topupOpen} onOpenChange={setTopupOpen} topup={topup} />
      <RefundDialog open={refundOpen} onOpenChange={setRefundOpen} refund={refund} />
    </Card>
  );
}

function TopupDialog({
  open,
  onOpenChange,
  topup,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  topup: ReturnType<typeof useAdminTopupUser>;
}) {
  const { t } = useTranslation();
  const form = useForm<TopupValues>({
    resolver: zodResolver(topupSchema),
    defaultValues: { amountCents: 0, currency: "USD", reference: "" },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await topup.mutateAsync(values);
    onOpenChange(false);
    form.reset({ amountCents: 0, currency: "USD", reference: "" });
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("billing.admin.users.topup.title")}</DialogTitle>
          <DialogDescription>{t("billing.admin.users.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-3">
          <MoneyFields form={form} />
          <div className="space-y-2">
            <Label htmlFor="topup-ref">{t("billing.admin.users.topup.reference.label")}</Label>
            <Input id="topup-ref" {...form.register("reference")} />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={topup.isPending}>
              {topup.isPending
                ? t("billing.admin.users.topup.submitting")
                : t("billing.admin.users.topup.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RefundDialog({
  open,
  onOpenChange,
  refund,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  refund: ReturnType<typeof useAdminRefundUser>;
}) {
  const { t } = useTranslation();
  const form = useForm<RefundValues>({
    resolver: zodResolver(refundSchema),
    defaultValues: { amountCents: 0, currency: "USD", reference: "" },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await refund.mutateAsync(values);
    onOpenChange(false);
    form.reset({ amountCents: 0, currency: "USD", reference: "" });
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("billing.admin.users.refund.title")}</DialogTitle>
          <DialogDescription>{t("billing.admin.users.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-3">
          <MoneyFields form={form} />
          <div className="space-y-2">
            <Label htmlFor="refund-ref">{t("billing.admin.users.refund.reference.label")}</Label>
            <Input id="refund-ref" {...form.register("reference")} />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={refund.isPending}>
              {refund.isPending
                ? t("billing.admin.users.refund.submitting")
                : t("billing.admin.users.refund.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

interface MoneyProps {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  form: ReturnType<typeof useForm<any>>;
}

function MoneyFields({ form }: MoneyProps) {
  const { t } = useTranslation();
  return (
    <div className="grid grid-cols-2 gap-3">
      <div className="space-y-2">
        <Label htmlFor="amount">{t("billing.admin.users.topup.amountCents.label")}</Label>
        <Input
          id="amount"
          type="number"
          min={1}
          {...form.register("amountCents", { valueAsNumber: true })}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="currency">{t("billing.admin.users.topup.currency.label")}</Label>
        <Input id="currency" defaultValue="USD" {...form.register("currency")} />
      </div>
    </div>
  );
}
