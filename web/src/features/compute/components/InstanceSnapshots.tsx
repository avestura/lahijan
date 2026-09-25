/**
 * InstanceSnapshots — the Snapshots tab on the instance detail page (WS-25).
 *
 * Replaces the WS-20 placeholder that pointed at "coming soon". The
 * panel renders:
 *   - the list of existing snapshots (with restore + delete triggers)
 *   - a "Create snapshot" form (name + optional description + stateful)
 *
 * Every privileged UI action is gated by usePerm so a tenant viewer
 * sees a read-only panel; the server enforces too (defense in depth).
 *
 * All copy is i18n'd (en + fa); the panel uses logical Tailwind
 * properties (ms-*, me-*) so RTL flips correctly.
 */
import { useState } from "react";
import { CameraIcon, RotateCcwIcon, Trash2Icon } from "lucide-react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { usePerm } from "@/lib/perm";
import {
  useComputeSnapshots,
  useCreateComputeSnapshot,
  useDeleteComputeSnapshot,
  useRestoreComputeSnapshot,
} from "../api";

interface SnapshotFormValues {
  name: string;
  description: string;
  stateful: boolean;
}

interface InstanceSnapshotsProps {
  instanceId: string;
  tenantId: string | null;
}

export function InstanceSnapshots({ instanceId, tenantId }: InstanceSnapshotsProps) {
  const { t } = useTranslation();
  const canCreate = usePerm("compute.snapshot.create").hasPerm;
  const canDelete = usePerm("compute.snapshot.delete").hasPerm;
  const canRestore = usePerm("compute.instance.update").hasPerm;

  const { data: snapshots, isLoading } = useComputeSnapshots(tenantId, instanceId);
  const createMut = useCreateComputeSnapshot(tenantId, instanceId);
  const deleteMut = useDeleteComputeSnapshot(tenantId, instanceId);
  const restoreMut = useRestoreComputeSnapshot(tenantId, instanceId);

  const [confirmingDelete, setConfirmingDelete] = useState<string | null>(null);
  const [confirmingRestore, setConfirmingRestore] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors },
  } = useForm<SnapshotFormValues>({
    defaultValues: { name: "", description: "", stateful: false },
  });
  const stateful = watch("stateful");

  function onSubmit(values: SnapshotFormValues) {
    createMut.mutate(
      { name: values.name, description: values.description, stateful: values.stateful },
      {
        onSuccess: () => {
          reset({ name: "", description: "", stateful: false });
        },
      },
    );
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center gap-3">
        <div className="flex h-9 w-9 items-center justify-center bg-muted text-muted-foreground">
          <CameraIcon className="h-4 w-4" aria-hidden="true" />
        </div>
        <CardTitle className="text-base">{t("compute.snapshots.title")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {canCreate && (
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-3 border p-3">
            <div className="grid gap-2 md:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="snap-name">{t("compute.snapshots.name.label")}</Label>
                <Input
                  id="snap-name"
                  placeholder={t("compute.snapshots.name.placeholder")}
                  aria-invalid={!!errors.name}
                  {...register("name", { required: true, maxLength: 63 })}
                />
                {errors.name && (
                  <p className="text-xs text-destructive" role="alert">
                    {t("common.validation.required")}
                  </p>
                )}
              </div>
              <div className="space-y-1">
                <Label htmlFor="snap-desc">{t("compute.snapshots.description.label")}</Label>
                <Input
                  id="snap-desc"
                  placeholder={t("compute.snapshots.description.placeholder")}
                  {...register("description", { maxLength: 255 })}
                />
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Checkbox
                id="snap-stateful"
                checked={stateful}
                onCheckedChange={(v) =>
                  register("stateful").onChange({ target: { value: v === true } })
                }
              />
              <Label htmlFor="snap-stateful" className="text-sm font-normal">
                {t("compute.snapshots.stateful")}
              </Label>
            </div>
            <Button type="submit" disabled={createMut.isPending} className="ms-auto block">
              {createMut.isPending && <Spinner className="me-2" />}
              {t("compute.snapshots.create")}
            </Button>
          </form>
        )}

        {isLoading ? (
          <div className="flex items-center justify-center py-6 text-sm text-muted-foreground">
            <Spinner className="me-2" />
            {t("common.loading")}
          </div>
        ) : !snapshots || snapshots.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {t("compute.snapshots.empty")}
          </p>
        ) : (
          <ul className="divide-y border">
            {snapshots.map((snap) => {
              const id = snap.id;
              const isConfirmingDelete = confirmingDelete === id;
              const isConfirmingRestore = confirmingRestore === id;
              return (
                <li
                  key={id}
                  className="flex flex-col gap-2 p-3 md:flex-row md:items-center md:justify-between"
                >
                  <div className="space-y-0.5">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{snap.name}</span>
                      {snap.policyId ? (
                        <span className="bg-blue-500/10 px-2 py-0.5 text-xs text-blue-700 dark:text-blue-300">
                          {t("compute.snapshots.bySchedule")}
                        </span>
                      ) : (
                        <span className="bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                          {t("compute.snapshots.manual")}
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {snap.description && <span className="me-2">{snap.description}</span>}
                      {snap.stateful && (
                        <span className="me-2">{t("compute.snapshots.stateful")}</span>
                      )}
                      {snap.sizeBytes && snap.sizeBytes > 0 && (
                        <span className="me-2">
                          {t("compute.snapshots.sizeBytes")}: {formatBytes(snap.sizeBytes)}
                        </span>
                      )}
                      {snap.createdAt && (
                        <span>
                          {t("compute.snapshots.tookAt")}:{" "}
                          {new Date(snap.createdAt).toLocaleString()}
                        </span>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    {canRestore &&
                      (isConfirmingRestore ? (
                        <>
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => {
                              restoreMut.mutate(id, {
                                onSettled: () => setConfirmingRestore(null),
                              });
                            }}
                            disabled={restoreMut.isPending}
                          >
                            {restoreMut.isPending && <Spinner size="sm" className="me-2" />}
                            {t("common.confirm")}
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setConfirmingRestore(null)}
                          >
                            {t("common.cancel")}
                          </Button>
                        </>
                      ) : (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setConfirmingRestore(id)}
                          title={t("compute.snapshots.restoreConfirm")}
                        >
                          <RotateCcwIcon className="me-2 h-3 w-3" aria-hidden="true" />
                          {t("compute.snapshots.restore")}
                        </Button>
                      ))}
                    {canDelete &&
                      (isConfirmingDelete ? (
                        <>
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => {
                              deleteMut.mutate(id, {
                                onSettled: () => setConfirmingDelete(null),
                              });
                            }}
                            disabled={deleteMut.isPending}
                          >
                            {deleteMut.isPending && <Spinner size="sm" className="me-2" />}
                            {t("common.confirm")}
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setConfirmingDelete(null)}
                          >
                            {t("common.cancel")}
                          </Button>
                        </>
                      ) : (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setConfirmingDelete(id)}
                          title={t("compute.snapshots.deleteConfirm")}
                        >
                          <Trash2Icon className="h-3 w-3" aria-hidden="true" />
                          <span className="sr-only">{t("compute.snapshots.delete")}</span>
                        </Button>
                      ))}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

// formatBytes renders a byte count in a human-friendly unit. Kept local
// so the panel does not pull in a new dependency.
function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  return `${value.toFixed(1)} ${units[unitIndex]}`;
}
