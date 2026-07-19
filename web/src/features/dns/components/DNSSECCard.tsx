/**
 * DNSSECCard — small card with the DNSSEC status + enable/disable
 * toggle. The action is gated by `dns.zone.update`; the confirm
 * dialog explains the implications.
 */
import * as React from "react";
import { useTranslation } from "react-i18next";
import { ShieldCheckIcon } from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
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
import { useSetDNSSEC } from "../api";

type DNSZone = components["schemas"]["DNSZone"];

interface Props {
  tenantId: string | null;
  zone: DNSZone | undefined;
}

export function DNSSECCard({ tenantId, zone }: Props) {
  const { t } = useTranslation();
  const setDnssec = useSetDNSSEC(tenantId, zone?.id);
  const { hasPerm } = usePerm("dns.zone.update");
  const [confirmOpen, setConfirmOpen] = React.useState(false);

  const enabled = zone?.isDnssecEnabled ?? false;

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2 text-base">
          <ShieldCheckIcon className="h-4 w-4" />
          {t("dns.detail.dnssec")}
        </CardTitle>
        <Badge variant={enabled ? "default" : "secondary"}>
          {enabled ? t("dns.dnssec.on") : t("dns.dnssec.off")}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-2">
        {hasPerm ? (
          <Button
            variant={enabled ? "destructive" : "default"}
            size="sm"
            disabled={setDnssec.isPending}
            onClick={() => setConfirmOpen(true)}
          >
            {enabled
              ? setDnssec.isPending
                ? t("dns.dnssec.disabling")
                : t("dns.dnssec.disable")
              : setDnssec.isPending
                ? t("dns.dnssec.enabling")
                : t("dns.dnssec.enable")}
          </Button>
        ) : (
          <p className="text-xs text-muted-foreground">{t("permission.denied")}</p>
        )}
      </CardContent>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {enabled ? t("dns.dnssec.disableConfirm.title") : t("dns.dnssec.enableConfirm.title")}
            </DialogTitle>
            <DialogDescription>
              {enabled
                ? t("dns.dnssec.disableConfirm.body")
                : t("dns.dnssec.enableConfirm.body")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant={enabled ? "destructive" : "default"}
              disabled={setDnssec.isPending}
              onClick={() => {
                setDnssec.mutate(
                  { action: enabled ? "disable" : "enable" },
                  { onSettled: () => setConfirmOpen(false) },
                );
              }}
            >
              {t("common.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
