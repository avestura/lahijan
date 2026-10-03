/**
 * PlatformSettingsPanel — the platform settings an administrator changes at
 * runtime (today: whether anyone may register). The configured default is only
 * the starting point; a saved choice wins until it is reset.
 */
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { useToast } from "@/hooks/useToast";
import { usePerm } from "@/lib/perm";
import { usePlatformSettings, useUpdatePlatformSettings } from "../api";

export function PlatformSettingsPanel() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const manage = usePerm("platform.settings.manage");
  const query = usePlatformSettings();
  const update = useUpdatePlatformSettings();

  const save = (body: Parameters<typeof update.mutate>[0]) =>
    update.mutate(body, {
      onSuccess: () => toast({ title: t("admin.settings.saved"), variant: "success" }),
      onError: () => toast({ title: t("admin.settings.saveFailed"), variant: "destructive" }),
    });

  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("admin.settings.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("admin.settings.subtitle")}</p>
      </header>

      {query.isLoading ? (
        <LoadingState rows={2} />
      ) : query.error || !query.data ? (
        <ErrorState
          message={t("common.error")}
          retryLabel={t("common.retry")}
          onRetry={() => void query.refetch()}
        />
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2 text-base">
              {t("admin.settings.registration.title")}
              <Badge variant={query.data.registrationEnabled ? "success" : "warning"}>
                {query.data.registrationEnabled
                  ? t("admin.settings.registration.open")
                  : t("admin.settings.registration.closed")}
              </Badge>
            </CardTitle>
            <CardDescription>{t("admin.settings.registration.description")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-start gap-2">
              <Checkbox
                id="registration-enabled"
                checked={query.data.registrationEnabled}
                disabled={!manage.hasPerm || update.isPending}
                onCheckedChange={(v) => save({ registrationEnabled: v === true })}
              />
              <div className="space-y-1">
                <Label htmlFor="registration-enabled" className="cursor-pointer">
                  {t("admin.settings.registration.toggle")}
                </Label>
                <p className="text-xs text-muted-foreground">
                  {t("admin.settings.registration.toggleHint")}
                </p>
              </div>
            </div>

            <div className="space-y-2 border-t border-border pt-3 text-xs text-muted-foreground">
              <p>
                {t("admin.settings.registration.defaultIs", {
                  value: query.data.registrationDefault
                    ? t("admin.settings.registration.open")
                    : t("admin.settings.registration.closed"),
                })}
              </p>
              {query.data.registrationOverridden ? (
                <div className="flex flex-wrap items-center gap-2">
                  <span>{t("admin.settings.registration.overridden")}</span>
                  {manage.hasPerm && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={update.isPending}
                      onClick={() => save({ resetRegistration: true })}
                    >
                      {t("admin.settings.registration.reset")}
                    </Button>
                  )}
                </div>
              ) : (
                <p>{t("admin.settings.registration.usingDefault")}</p>
              )}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
