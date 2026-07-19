/**
 * /admin/plugins — installed plugins list + upload.
 *
 * Platform-admin only (the _admin layout guard enforces this). The
 * "Upload plugin" button is gated by `plugins.install`.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { PlusIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { usePerm } from "@/lib/perm";
import { PluginList } from "@/features/plugins/components/PluginList";
import { UploadPluginDialog } from "@/features/plugins/components/UploadPluginDialog";
import { useAdminPlugins } from "@/features/plugins/api";

export const Route = createFileRoute("/admin/plugins/")({
  component: PluginsListPage,
});

function PluginsListPage() {
  const { t } = useTranslation();
  const { hasPerm } = usePerm("plugins.install");
  const query = useAdminPlugins();
  const [uploadOpen, setUploadOpen] = useState(false);

  return (
    <div className="space-y-4">
      <header className="flex items-center justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">{t("plugins.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("plugins.subtitle")}</p>
        </div>
        {hasPerm && (
          <Button onClick={() => setUploadOpen(true)}>
            <PlusIcon className="h-4 w-4" />
            {t("plugins.list.new")}
          </Button>
        )}
      </header>

      <PluginList
        plugins={query.data}
        isLoading={query.isLoading}
        error={query.error}
        onRetry={() => void query.refetch()}
      />

      <UploadPluginDialog open={uploadOpen} onOpenChange={setUploadOpen} />
    </div>
  );
}
