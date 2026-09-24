/**
 * /admin/plugins/$pluginId — plugin detail with status, permissions,
 * manifest, and enable/disable/delete actions.
 */
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { ArrowLeftIcon, PowerIcon, PowerOffIcon, Trash2Icon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import {
  useAdminPlugin,
  useDeleteAdminPlugin,
  useDisableAdminPlugin,
  useEnableAdminPlugin,
} from "@/features/plugins/api";
import { PluginStatusBadge } from "@/features/plugins/components/PluginStatusBadge";
import { PluginPermissionsCard } from "@/features/plugins/components/PluginPermissionsCard";

export const Route = createFileRoute("/admin/plugins/$pluginId")({
  component: PluginDetailPage,
});

const routeApi = getRouteApi("/admin/plugins/$pluginId");

function usePluginId(): string {
  const params = routeApi.useParams() as unknown as { pluginId: string };
  return params.pluginId;
}

function PluginDetailPage() {
  const { t } = useTranslation();
  const pluginId = usePluginId();
  const query = useAdminPlugin(pluginId);
  const enable = useEnableAdminPlugin();
  const disable = useDisableAdminPlugin();
  const destroy = useDeleteAdminPlugin();
  const { hasPerm: canInstall } = usePerm("plugins.install");
  const { hasPerm: canUninstall } = usePerm("plugins.uninstall");

  if (query.isLoading) return <LoadingState rows={4} />;
  if (query.error) {
    return (
      <ErrorState
        message={t("plugins.detail.notFound")}
        retryLabel={t("common.retry")}
        onRetry={() => void query.refetch()}
      />
    );
  }
  const plugin = query.data;
  if (!plugin) return null;

  return (
    <div className="space-y-4">
      <div>
        <Button asChild variant="ghost" size="sm" className="mb-2">
          <Link to="/admin/plugins">
            <ArrowLeftIcon className="h-4 w-4" />
            {t("common.back")}
          </Link>
        </Button>
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-1">
            <h1 className="text-2xl font-semibold">{plugin.name}</h1>
            <p className="text-sm text-muted-foreground">
              {plugin.description ?? t("plugins.detail.noDescription")}
            </p>
            <div className="flex items-center gap-2 pt-1">
              <PluginStatusBadge status={plugin.status} />
              <span className="font-mono text-xs">{`v${plugin.version}`}</span>
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            {canInstall && plugin.status !== "active" && (
              <Button onClick={() => enable.mutate({ pluginId })} disabled={enable.isPending}>
                <PowerIcon className="h-4 w-4" />
                {t("plugins.actions.enable")}
              </Button>
            )}
            {canInstall && plugin.status === "active" && (
              <Button
                variant="outline"
                onClick={() => disable.mutate({ pluginId })}
                disabled={disable.isPending}
              >
                <PowerOffIcon className="h-4 w-4" />
                {t("plugins.actions.disable")}
              </Button>
            )}
            {canUninstall && (
              <Button
                variant="destructive"
                onClick={() => destroy.mutate({ pluginId })}
                disabled={destroy.isPending}
              >
                <Trash2Icon className="h-4 w-4" />
                {t("plugins.actions.delete")}
              </Button>
            )}
          </div>
        </header>
      </div>

      <PluginPermissionsCard plugin={plugin} />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("plugins.detail.manifest")}</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="overflow-x-auto border border-border bg-surface-sunken p-3 font-mono text-xs">
            {JSON.stringify(plugin.manifest, null, 2)}
          </pre>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("plugins.detail.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <DetailRow label={t("plugins.detail.id")} value={plugin.id} />
          <DetailRow label={t("plugins.detail.name")} value={plugin.name} />
          <DetailRow label={t("plugins.detail.version")} value={plugin.version} />
          {plugin.tenantId && (
            <DetailRow label={t("plugins.detail.tenant")} value={plugin.tenantId} />
          )}
          <DetailRow label={t("plugins.detail.wasmHash")} value={plugin.wasmHash} />
          <DetailRow label={t("plugins.detail.wasmSize")} value={`${plugin.wasmSize} bytes`} />
        </CardContent>
      </Card>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[140px_1fr] items-center gap-2">
      <span className="text-xs uppercase tracking-wider text-muted-foreground">{label}</span>
      <span className="break-all font-mono text-xs">{value}</span>
    </div>
  );
}
