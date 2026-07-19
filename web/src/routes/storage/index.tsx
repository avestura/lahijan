/**
 * /storage — buckets list page.
 *
 * Renders the StorageBucketList with text filter + create button.
 * The "New bucket" button is gated by `s3.bucket.create`.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { PlusIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { usePerm } from "@/lib/perm";
import { useTenant } from "@/hooks/useTenant";
import { useStorageBuckets } from "@/features/storage/api";
import { StorageBucketList } from "@/features/storage/components/StorageBucketList";
import { CreateStorageBucketDialog } from "@/features/storage/components/CreateStorageBucketDialog";

export const Route = createFileRoute("/storage/")({
  component: StorageListPage,
});

function StorageListPage() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const { hasPerm } = usePerm("s3.bucket.create");

  const query = useStorageBuckets(tenantId);

  const [filter, setFilter] = useState("");
  const [createOpen, setCreateOpen] = useState(false);

  const filtered = useMemo(() => {
    const list = query.data ?? [];
    const lower = filter.trim().toLowerCase();
    return list.filter(
      (b) =>
        !lower ||
        b.name.toLowerCase().includes(lower) ||
        b.slug.toLowerCase().includes(lower) ||
        (b.label ?? "").toLowerCase().includes(lower),
    );
  }, [query.data, filter]);

  return (
    <div className="space-y-4">
      <header className="flex items-center justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">{t("storage.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("storage.subtitle")}</p>
        </div>
        {hasPerm && (
          <Button onClick={() => setCreateOpen(true)}>
            <PlusIcon className="h-4 w-4" />
            {t("storage.list.new")}
          </Button>
        )}
      </header>

      <Input
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder={t("storage.list.filter.placeholder")}
        className="max-w-sm"
        aria-label={t("common.search")}
      />

      <StorageBucketList
        buckets={filtered}
        isLoading={query.isLoading}
        error={query.error}
        onRetry={() => void query.refetch()}
      />

      <CreateStorageBucketDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        tenantId={tenantId}
      />
    </div>
  );
}
