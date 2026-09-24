/**
 * RecoveryCodesCard — show recovery code metadata + regenerate.
 *
 * Recovery codes are shown ONCE at generation time. We surface the
 * remaining count here and a "Regenerate" button that hits
 * POST /me/mfa/recovery to roll a fresh batch (shown in a one-shot
 * dialog).
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { LoadingState } from "@/components/layout/LoadingState";
import { useRegenerateRecoveryCodes, useRecoveryCodesMeta } from "../api";

export function RecoveryCodesCard() {
  const { t } = useTranslation();
  const meta = useRecoveryCodesMeta();
  const regenerate = useRegenerateRecoveryCodes();
  const [revealOpen, setRevealOpen] = useState(false);

  const onRegenerate = () => {
    regenerate.mutate(undefined, {
      onSuccess: () => setRevealOpen(true),
    });
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{t("settings.security.recovery.title")}</CardTitle>
        <Button variant="outline" size="sm" onClick={onRegenerate} disabled={regenerate.isPending}>
          {t("settings.security.recovery.regenerate")}
        </Button>
      </CardHeader>
      <CardContent>
        {meta.isLoading ? (
          <LoadingState rows={1} />
        ) : meta.data ? (
          <p className="text-sm text-muted-foreground">
            {t("settings.security.recovery.remaining", {
              count: meta.data.remaining,
              total: meta.data.total,
            })}
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">{t("common.none")}</p>
        )}
      </CardContent>

      <Dialog open={revealOpen} onOpenChange={setRevealOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("settings.security.recovery.title")}</DialogTitle>
            <DialogDescription>{t("settings.security.recovery.warning")}</DialogDescription>
          </DialogHeader>
          {regenerate.data && (
            <ul className="grid grid-cols-2 gap-1 border border-border bg-surface-sunken p-3 font-mono text-xs">
              {regenerate.data.codes.map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ul>
          )}
          <DialogFooter>
            <Button onClick={() => setRevealOpen(false)}>{t("common.close")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
