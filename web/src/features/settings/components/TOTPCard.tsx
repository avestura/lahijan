/**
 * TOTPCard — enroll / disable TOTP via QR code.
 *
 * Flow:
 *   1. User clicks "Set up" → POST /me/mfa/totp/enroll → secret + URI.
 *   2. We render the URI as a QR via qrcode.react + show the raw secret.
 *   3. User types a 6-digit code → POST /me/mfa/totp/verify.
 *   4. On success the response carries the recovery codes batch (shown
 *      once via the RecoveryCodesCard).
 *
 * Disabling TOTP requires the current password (per ADR-0021 +
 * WS-07c); we surface a small re-auth dialog.
 */
import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { QRCodeSVG } from "qrcode.react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { LoadingState } from "@/components/layout/LoadingState";
import { useDisableTOTP, useEnrollTOTP, useRecoveryCodesMeta, useVerifyTOTP } from "../api";
import { disableTotpSchema, verifyTotpSchema, type VerifyTotpValues } from "../schemas";

export function TOTPCard() {
  const { t } = useTranslation();
  const enroll = useEnrollTOTP();
  const verify = useVerifyTOTP();
  const disable = useDisableTOTP();
  const recoveryMeta = useRecoveryCodesMeta();
  const [disableOpen, setDisableOpen] = useState(false);

  const enrolled = (recoveryMeta.data?.total ?? 0) > 0;

  const verifyForm = useForm<VerifyTotpValues>({
    resolver: zodResolver(verifyTotpSchema),
    defaultValues: { code: "" },
  });

  const disableForm = useForm<{ currentPassword: string }>({
    resolver: zodResolver(disableTotpSchema),
    defaultValues: { currentPassword: "" },
  });

  const onVerify = verifyForm.handleSubmit((values) =>
    verify.mutate(values, {
      onSuccess: () => {
        enroll.reset();
        verifyForm.reset({ code: "" });
      },
    }),
  );

  const onDisable = disableForm.handleSubmit((values) =>
    disable.mutate(values, {
      onSuccess: () => {
        setDisableOpen(false);
        disableForm.reset({ currentPassword: "" });
      },
    }),
  );

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{t("settings.security.totp.title")}</CardTitle>
        <Badge variant={enrolled ? "success" : "muted"}>
          {enrolled
            ? t("settings.security.totp.enrolled")
            : t("settings.security.totp.notEnrolled")}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-4">
        {!enrolled && !enroll.data && (
          <Button onClick={() => enroll.mutate()} disabled={enroll.isPending}>
            {t("settings.security.totp.enroll")}
          </Button>
        )}

        {enroll.isPending && <LoadingState rows={1} />}

        {enroll.data && (
          <div className="space-y-4">
            <div className="flex flex-col items-center gap-2 border border-border p-4">
              <p className="text-sm text-muted-foreground">{t("settings.security.totp.qr.alt")}</p>
              <QRCodeSVG value={enroll.data.provisioningUri} size={160} />
              <div className="space-y-1 text-center">
                <p className="text-xs text-muted-foreground">
                  {t("settings.security.totp.qr.cannotScan")}
                </p>
                <code className="block bg-muted px-2 py-1 font-mono text-xs">
                  {enroll.data.secret}
                </code>
              </div>
            </div>

            <form onSubmit={onVerify} className="flex items-end gap-2">
              <div className="flex-1 space-y-2">
                <Label htmlFor="totp-code">{t("settings.security.totp.verify.label")}</Label>
                <Input
                  id="totp-code"
                  inputMode="numeric"
                  maxLength={6}
                  placeholder={t("settings.security.totp.verify.placeholder")}
                  aria-invalid={!!verifyForm.formState.errors.code}
                  {...verifyForm.register("code")}
                />
                {verifyForm.formState.errors.code && (
                  <p className="text-xs text-destructive">
                    {t("settings.security.totp.verify.invalid")}
                  </p>
                )}
              </div>
              <Button type="submit" disabled={verify.isPending}>
                {t("settings.security.totp.verify.submit")}
              </Button>
            </form>

            {verify.data && (
              <div className="border border-success bg-success-soft p-3 text-sm">
                <p className="font-medium text-success">{t("settings.security.recovery.title")}</p>
                <ul className="mt-2 grid grid-cols-2 gap-1 font-mono text-xs">
                  {verify.data.codes.map((c) => (
                    <li key={c}>{c}</li>
                  ))}
                </ul>
                <p className="mt-2 text-xs text-muted-foreground">
                  {t("settings.security.recovery.warning")}
                </p>
              </div>
            )}
          </div>
        )}

        {enrolled && (
          <Button variant="outline" onClick={() => setDisableOpen(true)}>
            {t("settings.security.totp.disable")}
          </Button>
        )}

        <Dialog open={disableOpen} onOpenChange={setDisableOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("settings.security.totp.disableConfirm.title")}</DialogTitle>
              <DialogDescription>
                {t("settings.security.totp.disableConfirm.body")}
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={onDisable} className="space-y-3">
              <div className="space-y-2">
                <Label htmlFor="disable-pwd">
                  {t("settings.security.totp.disableConfirm.currentPassword")}
                </Label>
                <Input
                  id="disable-pwd"
                  type="password"
                  autoComplete="current-password"
                  {...disableForm.register("currentPassword")}
                />
              </div>
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => setDisableOpen(false)}>
                  {t("common.cancel")}
                </Button>
                <Button type="submit" variant="destructive" disabled={disable.isPending}>
                  {t("settings.security.totp.disable")}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  );
}
