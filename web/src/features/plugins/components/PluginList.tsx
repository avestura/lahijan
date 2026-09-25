/**
 * PluginList — table of installed plugins with enable/disable/delete
 * actions + a kebab with detail navigation.
 */
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { EyeIcon, MoreHorizontalIcon, PlugIcon, Trash2Icon } from "lucide-react";
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
import { useDeleteAdminPlugin, useDisableAdminPlugin, useEnableAdminPlugin } from "../api";
import { PluginStatusBadge } from "./PluginStatusBadge";

type AdminPlugin = components["schemas"]["AdminPlugin"];

interface Props {
  plugins: AdminPlugin[] | undefined;
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
}

export function PluginList({ plugins, isLoading, error, onRetry }: Props) {
  const { t } = useTranslation();
  const enable = useEnableAdminPlugin();
  const disable = useDisableAdminPlugin();
  const destroy = useDeleteAdminPlugin();
  const { hasPerm: canInstall } = usePerm("plugins.install");
  const { hasPerm: canUninstall } = usePerm("plugins.uninstall");

  if (isLoading) return <LoadingState rows={4} />;
  if (error) {
    return (
      <ErrorState message={t("common.error")} retryLabel={t("common.retry")} onRetry={onRetry} />
    );
  }
  if (!plugins || plugins.length === 0) {
    return (
      <EmptyState
        icon={PlugIcon}
        title={t("plugins.list.empty.title")}
        description={t("plugins.list.empty.body")}
      />
    );
  }

  return (
    <div className="border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("plugins.list.columns.name")}</TableHead>
            <TableHead>{t("plugins.list.columns.version")}</TableHead>
            <TableHead>{t("plugins.list.columns.status")}</TableHead>
            <TableHead>{t("plugins.list.columns.permissions")}</TableHead>
            <TableHead>{t("plugins.list.columns.updated")}</TableHead>
            <TableHead className="text-end">{t("plugins.list.columns.actions")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {plugins.map((p) => (
            <TableRow key={p.id}>
              <TableCell>
                <Link
                  to="/admin/plugins/$pluginId"
                  params={{ pluginId: p.id }}
                  className="font-medium hover:underline"
                >
                  {p.name}
                </Link>
                {p.description ? (
                  <p className="line-clamp-1 text-xs text-muted-foreground">{p.description}</p>
                ) : null}
              </TableCell>
              <TableCell className="font-mono text-xs">{p.version}</TableCell>
              <TableCell>
                <PluginStatusBadge status={p.status} />
              </TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-1">
                  {(p.permissions ?? []).slice(0, 3).map((perm) => (
                    <Badge key={perm} variant="outline">
                      <code className="font-mono text-[10px]">{perm}</code>
                    </Badge>
                  ))}
                  {(p.permissions ?? []).length > 3 && (
                    <Badge variant="secondary">{`+${(p.permissions ?? []).length - 3}`}</Badge>
                  )}
                  {(p.permissions ?? []).length === 0 && (
                    <span className="text-xs text-muted-foreground">
                      {t("plugins.permissions.none")}
                    </span>
                  )}
                </div>
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {new Date(p.updatedAt).toLocaleDateString()}
              </TableCell>
              <TableCell className="text-end">
                <div className="flex items-center justify-end gap-1">
                  {canInstall && p.status !== "active" && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={enable.isPending}
                      onClick={() => enable.mutate({ pluginId: p.id })}
                    >
                      {t("plugins.actions.enable")}
                    </Button>
                  )}
                  {canInstall && p.status === "active" && (
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={disable.isPending}
                      onClick={() => disable.mutate({ pluginId: p.id })}
                    >
                      {t("plugins.actions.disable")}
                    </Button>
                  )}
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={t("plugins.list.columns.actions")}
                      >
                        <MoreHorizontalIcon className="h-4 w-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem asChild>
                        <Link to="/admin/plugins/$pluginId" params={{ pluginId: p.id }}>
                          <EyeIcon />
                          {t("plugins.actions.viewDetail")}
                        </Link>
                      </DropdownMenuItem>
                      {canUninstall && (
                        <>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            variant="destructive"
                            onClick={() => destroy.mutate({ pluginId: p.id })}
                          >
                            <Trash2Icon />
                            {t("plugins.actions.delete")}
                          </DropdownMenuItem>
                        </>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
