/**
 * InstanceList — table of compute instances in the active tenant.
 *
 * Renders the loading, empty, error, and populated states per the
 * design system. The lifecycle buttons (start / stop / restart /
 * delete) are gated by `usePerm`; the row's kebab menu shows the
 * full set including freeze/unfreeze for running/frozen instances.
 */
import * as React from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import {
  MoreHorizontalIcon,
  PlayIcon,
  RotateCwIcon,
  SquareIcon,
  SnowflakeIcon,
  FlameIcon,
  Trash2Icon,
  CloudIcon,
} from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
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
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import { useTenant } from "@/hooks/useTenant";
import { useDeleteInstance, useLifecycle, classifyStatus } from "../api";
import { InstanceStatusBadge } from "./InstanceStatusBadge";

type Instance = components["schemas"]["ComputeInstance"];

interface Props {
  instances: Instance[] | undefined;
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
  /** Optional callback for the parent to track the selection state. */
  onSelectChange?: (selectedIds: string[]) => void;
}

/**
 * InstanceList — pure component given the data + handlers; the parent
 * page owns the query + filter state.
 */
export function InstanceList({ instances, isLoading, error, onRetry }: Props) {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const lifecycle = useLifecycle(tenantId);
  const destroy = useDeleteInstance(tenantId);
  const [selected, setSelected] = React.useState<Set<string>>(new Set());

  const { hasPerm: canStart } = usePerm("compute.instance.start");
  const { hasPerm: canStop } = usePerm("compute.instance.stop");
  const { hasPerm: canRestart } = usePerm("compute.instance.restart");
  const { hasPerm: canDelete } = usePerm("compute.instance.delete");

  if (isLoading) {
    return <LoadingState rows={4} />;
  }
  if (error) {
    return (
      <ErrorState message={t("common.error")} retryLabel={t("common.retry")} onRetry={onRetry} />
    );
  }
  if (!instances || instances.length === 0) {
    return (
      <EmptyState
        icon={CloudIcon}
        title={t("compute.list.empty.title")}
        description={t("compute.list.empty.body")}
      />
    );
  }

  const toggleRow = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const allSelected = instances.length > 0 && selected.size === instances.length;
  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(instances.map((i) => i.id)));
  };

  return (
    <div className="space-y-3">
      {selected.size > 0 && (
        <div className="flex items-center justify-between rounded-md border border-border bg-muted/30 px-3 py-2 text-sm">
          <span>{t("compute.list.bulkActions.selected", { count: selected.size })}</span>
          <div className="flex gap-2">
            {canStart && (
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  selected.forEach((id) =>
                    lifecycle.mutate({ instanceId: id, action: "start" }),
                  )
                }
              >
                <PlayIcon className="h-4 w-4" />
                {t("compute.list.bulkActions.start")}
              </Button>
            )}
            {canStop && (
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  selected.forEach((id) =>
                    lifecycle.mutate({ instanceId: id, action: "stop" }),
                  )
                }
              >
                <SquareIcon className="h-4 w-4" />
                {t("compute.list.bulkActions.stop")}
              </Button>
            )}
            {canRestart && (
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  selected.forEach((id) =>
                    lifecycle.mutate({ instanceId: id, action: "restart" }),
                  )
                }
              >
                <RotateCwIcon className="h-4 w-4" />
                {t("compute.list.bulkActions.restart")}
              </Button>
            )}
            {canDelete && (
              <Button
                size="sm"
                variant="destructive"
                onClick={() =>
                  selected.forEach((id) => destroy.mutate({ instanceId: id, force: false }))
                }
              >
                <Trash2Icon className="h-4 w-4" />
                {t("compute.list.bulkActions.delete")}
              </Button>
            )}
          </div>
        </div>
      )}

      <div className="rounded-md border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[40px]">
                <Checkbox
                  checked={allSelected}
                  onCheckedChange={toggleAll}
                  aria-label={t("common.selectAll")}
                />
              </TableHead>
              <TableHead>{t("compute.list.columns.name")}</TableHead>
              <TableHead>{t("compute.list.columns.status")}</TableHead>
              <TableHead>{t("compute.list.columns.image")}</TableHead>
              <TableHead>{t("compute.list.columns.type")}</TableHead>
              <TableHead className="text-end">{t("compute.list.columns.actions")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {instances.map((inst) => {
              const bucket = classifyStatus(inst.status);
              const canRunNow = bucket === "stopped";
              const canStopNow = bucket === "running";
              const canFreezeNow = bucket === "running";
              const canUnfreezeNow = bucket === "frozen";
              return (
                <TableRow key={inst.id}>
                  <TableCell>
                    <Checkbox
                      checked={selected.has(inst.id)}
                      onCheckedChange={() => toggleRow(inst.id)}
                      aria-label={`select-${inst.id}`}
                    />
                  </TableCell>
                  <TableCell>
                    <Link
                      to="/compute/$id"
                      params={{ id: inst.id }}
                      className="font-medium hover:underline"
                    >
                      {inst.name}
                    </Link>
                    {inst.description ? (
                      <p className="line-clamp-1 text-xs text-muted-foreground">
                        {inst.description}
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell>
                    <InstanceStatusBadge status={inst.status} />
                  </TableCell>
                  <TableCell className="font-mono text-xs">{inst.imageAlias}</TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {t(`compute.types.${inst.type ?? "container"}`)}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-end">
                    <div className="flex items-center justify-end gap-1">
                      {canStart && canRunNow && (
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={t("compute.actions.start")}
                          onClick={() =>
                            lifecycle.mutate({ instanceId: inst.id, action: "start" })
                          }
                        >
                          <PlayIcon className="h-4 w-4" />
                        </Button>
                      )}
                      {canStop && canStopNow && (
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={t("compute.actions.stop")}
                          onClick={() =>
                            lifecycle.mutate({ instanceId: inst.id, action: "stop" })
                          }
                        >
                          <SquareIcon className="h-4 w-4" />
                        </Button>
                      )}
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label={t("compute.actions.viewDetails")}
                          >
                            <MoreHorizontalIcon className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuLabel>{t("compute.title")}</DropdownMenuLabel>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem asChild>
                            <Link to="/compute/$id" params={{ id: inst.id }}>
                              {t("compute.actions.viewDetails")}
                            </Link>
                          </DropdownMenuItem>
                          {canRestart && (
                            <DropdownMenuItem
                              onClick={() =>
                                lifecycle.mutate({ instanceId: inst.id, action: "restart" })
                              }
                            >
                              <RotateCwIcon className="me-2 h-4 w-4" />
                              {t("compute.actions.restart")}
                            </DropdownMenuItem>
                          )}
                          {canStop && canFreezeNow && (
                            <DropdownMenuItem
                              onClick={() =>
                                lifecycle.mutate({ instanceId: inst.id, action: "freeze" })
                              }
                            >
                              <SnowflakeIcon className="me-2 h-4 w-4" />
                              {t("compute.actions.freeze")}
                            </DropdownMenuItem>
                          )}
                          {canStart && canUnfreezeNow && (
                            <DropdownMenuItem
                              onClick={() =>
                                lifecycle.mutate({ instanceId: inst.id, action: "unfreeze" })
                              }
                            >
                              <FlameIcon className="me-2 h-4 w-4" />
                              {t("compute.actions.unfreeze")}
                            </DropdownMenuItem>
                          )}
                          <DropdownMenuSeparator />
                          {canDelete && (
                            <DropdownMenuItem
                              className="text-destructive focus:text-destructive"
                              onClick={() =>
                                destroy.mutate({ instanceId: inst.id, force: false })
                              }
                            >
                              <Trash2Icon className="me-2 h-4 w-4" />
                              {t("compute.actions.delete")}
                            </DropdownMenuItem>
                          )}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
