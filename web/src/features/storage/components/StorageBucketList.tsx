/**
 * StorageBucketList — table of buckets in the active tenant.
 *
 * Each row links to the bucket detail page. Delete is gated by
 * `s3.bucket.delete`; the delete action only appears in the kebab
 * menu (keeps the row tidy).
 */
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { DatabaseIcon, EyeIcon, MoreHorizontalIcon, Trash2Icon } from "lucide-react";
import type { components } from "@api-schema";

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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import { useDeleteStorageBucket } from "../api";
import { formatBytes } from "../format";

type StorageBucket = components["schemas"]["StorageBucket"];

interface Props {
  buckets: StorageBucket[] | undefined;
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
}

export function StorageBucketList({ buckets, isLoading, error, onRetry }: Props) {
  const { t } = useTranslation();
  const destroy = useDeleteStorageBucket(null);
  const { hasPerm: canDelete } = usePerm("s3.bucket.delete");

  if (isLoading) return <LoadingState rows={4} />;
  if (error) {
    return (
      <ErrorState message={t("common.error")} retryLabel={t("common.retry")} onRetry={onRetry} />
    );
  }
  if (!buckets || buckets.length === 0) {
    return (
      <EmptyState
        icon={DatabaseIcon}
        title={t("storage.list.empty.title")}
        description={t("storage.list.empty.body")}
      />
    );
  }

  return (
    <div className="border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("storage.list.columns.name")}</TableHead>
            <TableHead>{t("storage.list.columns.usage")}</TableHead>
            <TableHead>{t("storage.list.columns.objects")}</TableHead>
            <TableHead>{t("storage.list.columns.quota")}</TableHead>
            <TableHead>{t("storage.list.columns.created")}</TableHead>
            <TableHead className="text-end">{t("storage.list.columns.actions")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {buckets.map((b) => (
            <TableRow key={b.id}>
              <TableCell>
                <Link
                  to="/storage/$bucketId"
                  params={{ bucketId: b.id }}
                  className="font-medium hover:underline"
                >
                  {b.name}
                </Link>
                <p className="font-mono text-xs text-muted-foreground">{b.slug}</p>
              </TableCell>
              <TableCell className="text-xs">{formatBytes(b.bytesUsed)}</TableCell>
              <TableCell className="text-xs">{b.objectsUsed.toLocaleString()}</TableCell>
              <TableCell>
                {b.quotaBytes > 0 || b.quotaObjects > 0 ? (
                  <Badge variant="outline">
                    {b.quotaBytes > 0 ? formatBytes(b.quotaBytes) : "∞"}
                    {b.quotaObjects > 0 ? ` · ${b.quotaObjects}` : ""}
                  </Badge>
                ) : (
                  <Badge variant="secondary">{t("storage.usage.unlimited")}</Badge>
                )}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {new Date(b.createdAt).toLocaleDateString()}
              </TableCell>
              <TableCell className="text-end">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={t("storage.list.columns.actions")}
                    >
                      <MoreHorizontalIcon className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem asChild>
                      <Link to="/storage/$bucketId" params={{ bucketId: b.id }}>
                        <EyeIcon />
                        {t("storage.detail.tabs.overview")}
                      </Link>
                    </DropdownMenuItem>
                    {canDelete && (
                      <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          variant="destructive"
                          onClick={() => destroy.mutate({ bucketId: b.id })}
                        >
                          <Trash2Icon />
                          {t("common.delete")}
                        </DropdownMenuItem>
                      </>
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
