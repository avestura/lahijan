/**
 * MarketplaceList — browse available plugins + install/upgrade.
 */
import * as React from "react";
import { useTranslation } from "react-i18next";
import { StoreIcon } from "lucide-react";

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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import {
  useAdminMarketplace,
  useAdminPlugins,
  useInstallFromMarketplace,
  useUpgradeFromMarketplace,
} from "../api";

export function MarketplaceList() {
  const { t } = useTranslation();
  const market = useAdminMarketplace();
  const installed = useAdminPlugins();
  const install = useInstallFromMarketplace();
  const upgrade = useUpgradeFromMarketplace();
  const { hasPerm } = usePerm("plugins.install");

  const [pending, setPending] = React.useState<
    | { kind: "install" | "upgrade"; name: string }
    | null
  >(null);

  const installedByName = new Map((installed.data ?? []).map((p) => [p.name, p]));

  if (market.isLoading) return <LoadingState rows={3} />;
  if (market.error) {
    return (
      <ErrorState
        message={t("common.error")}
        retryLabel={t("common.retry")}
        onRetry={() => void market.refetch()}
      />
    );
  }
  if ((market.data ?? []).length === 0) {
    return (
      <EmptyState
        icon={StoreIcon}
        title={t("plugins.marketplace.empty.title")}
        description={t("plugins.marketplace.empty.body")}
      />
    );
  }

  const onConfirm = async () => {
    if (!pending) return;
    if (pending.kind === "install") {
      await install.mutateAsync({ name: pending.name });
    } else {
      await upgrade.mutateAsync({ name: pending.name });
    }
    setPending(null);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("plugins.marketplace.title")}</CardTitle>
        <p className="text-xs text-muted-foreground">{t("plugins.marketplace.subtitle")}</p>
      </CardHeader>
      <CardContent>
        <div className="rounded-md border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("plugins.marketplace.columns.name")}</TableHead>
                <TableHead>{t("plugins.marketplace.columns.version")}</TableHead>
                <TableHead>{t("plugins.marketplace.columns.description")}</TableHead>
                <TableHead>{t("plugins.marketplace.columns.license")}</TableHead>
                <TableHead className="text-end">{t("plugins.marketplace.columns.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(market.data ?? []).map((entry) => {
                const installedRow = installedByName.get(entry.name);
                const canUpgrade =
                  !!installedRow && installedRow.version !== entry.version;
                return (
                  <TableRow key={entry.name}>
                    <TableCell className="font-medium">{entry.name}</TableCell>
                    <TableCell className="font-mono text-xs">{entry.version}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {entry.description ?? t("plugins.detail.noDescription")}
                    </TableCell>
                    <TableCell>
                      {entry.license ? (
                        <Badge variant="outline">{entry.license}</Badge>
                      ) : (
                        <span className="text-xs text-muted-foreground">{t("common.none")}</span>
                      )}
                    </TableCell>
                    <TableCell className="text-end">
                      {hasPerm && !installedRow && (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={install.isPending}
                          onClick={() =>
                            setPending({ kind: "install", name: entry.name })
                          }
                        >
                          {t("plugins.actions.install")}
                        </Button>
                      )}
                      {hasPerm && canUpgrade && (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={upgrade.isPending}
                          onClick={() =>
                            setPending({ kind: "upgrade", name: entry.name })
                          }
                        >
                          {t("plugins.actions.upgrade")}
                        </Button>
                      )}
                      {installedRow && !canUpgrade && (
                        <Badge variant="secondary">
                          {t("plugins.status.active")}
                        </Badge>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      </CardContent>

      <Dialog open={!!pending} onOpenChange={(o) => !o && setPending(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {pending?.kind === "upgrade"
                ? t("plugins.marketplace.upgradeConfirm.title", { name: pending?.name ?? "" })
                : t("plugins.marketplace.installConfirm.title", { name: pending?.name ?? "" })}
            </DialogTitle>
            <DialogDescription>
              {pending?.kind === "upgrade"
                ? t("plugins.marketplace.upgradeConfirm.body")
                : t("plugins.marketplace.installConfirm.body")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setPending(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              disabled={install.isPending || upgrade.isPending}
              onClick={onConfirm}
            >
              {pending?.kind === "upgrade"
                ? t("plugins.actions.upgrade")
                : t("plugins.actions.install")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
