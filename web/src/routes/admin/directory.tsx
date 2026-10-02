/**
 * /admin/directory — connections to external user directories.
 *
 * LDAP connections can be tested and synced to import users and groups.
 * SAML connections are identity providers (single sign-on); they can be
 * tested (IdP metadata check) but have nothing to sync. Platform admins only.
 */
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowLeftIcon,
  NetworkIcon,
  PencilIcon,
  PlugZapIcon,
  PlusIcon,
  RefreshCwIcon,
  Trash2Icon,
  UsersIcon,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Pagination } from "@/components/ui/pagination";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { FeatureDisabledState } from "@/components/layout/FeatureDisabledState";
import { LoadingState } from "@/components/layout/LoadingState";
import { useToast } from "@/hooks/useToast";
import { isFeatureDisabledError } from "@/lib/api-errors";
import { usePerm } from "@/lib/perm";
import {
  useDeleteDirectoryConnection,
  useDirectoryConnections,
  useDirectoryGroups,
  useSyncDirectoryConnection,
  useTestDirectoryConnection,
  type DirectoryConnection,
  type DirectoryTestResult,
} from "@/features/admin/api";
import { ConnectionDialog, TestOutcome } from "@/features/admin/components/ConnectionDialog";

export const Route = createFileRoute("/admin/directory")({
  component: AdminDirectoryPage,
});

function AdminDirectoryPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const manage = usePerm("platform.directory.manage");
  const query = useDirectoryConnections();
  const sync = useSyncDirectoryConnection();
  const test = useTestDirectoryConnection();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<DirectoryConnection | null>(null);
  const [deleting, setDeleting] = useState<DirectoryConnection | null>(null);
  const [groupsFor, setGroupsFor] = useState<DirectoryConnection | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [tests, setTests] = useState<Record<string, DirectoryTestResult | "error">>({});

  function openCreate() {
    setEditing(null);
    setDialogOpen(true);
  }
  function openEdit(c: DirectoryConnection) {
    setEditing(c);
    setDialogOpen(true);
  }

  function runTest(c: DirectoryConnection) {
    setBusyId(c.id);
    test.mutate(
      { connectionId: c.id },
      {
        onSuccess: (res) => setTests((r) => ({ ...r, [c.id]: res })),
        onError: () => setTests((r) => ({ ...r, [c.id]: "error" })),
        onSettled: () => setBusyId(null),
      },
    );
  }

  function runSync(c: DirectoryConnection) {
    setBusyId(c.id);
    sync.mutate(c.id, {
      onSuccess: (res) =>
        toast({
          title: t("admin.directory.sync.done"),
          description: t("admin.directory.sync.summary", res),
          variant: "success",
        }),
      onError: () => toast({ title: t("admin.directory.sync.failed"), variant: "destructive" }),
      onSettled: () => setBusyId(null),
    });
  }

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">{t("admin.directory.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("admin.directory.subtitle")}</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" asChild>
            <Link to="/admin/users">
              <ArrowLeftIcon className="h-4 w-4 rtl:rotate-180" />
              {t("admin.directory.backToUsers")}
            </Link>
          </Button>
          {manage.hasPerm && (
            <Button onClick={openCreate}>
              <PlusIcon className="h-4 w-4" />
              {t("admin.directory.new")}
            </Button>
          )}
        </div>
      </header>

      {query.isLoading ? (
        <LoadingState rows={3} />
      ) : query.error ? (
        isFeatureDisabledError(query.error) ? (
          <FeatureDisabledState
            title={t("common.featureDisabled.title")}
            description={t("common.featureDisabled.body")}
          />
        ) : (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        )
      ) : (query.data ?? []).length === 0 ? (
        <EmptyState
          icon={NetworkIcon}
          title={t("admin.directory.empty.title")}
          description={t("admin.directory.empty.body")}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("admin.directory.columns.name")}</TableHead>
                  <TableHead>{t("admin.directory.columns.kind")}</TableHead>
                  <TableHead>{t("admin.directory.columns.status")}</TableHead>
                  <TableHead>{t("admin.directory.columns.lastSync")}</TableHead>
                  <TableHead>{t("admin.directory.columns.actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(query.data ?? []).map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-mono text-xs">{c.name}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{t(`admin.directory.kind.${c.kind}`)}</Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        <Badge variant={c.enabled ? "success" : "muted"}>
                          {c.enabled ? t("admin.directory.enabled") : t("admin.directory.disabled")}
                        </Badge>
                        {c.enabled && !c.active && (
                          <Badge variant="warning">{t("admin.directory.inactive")}</Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs">
                      {c.kind === "saml" ? (
                        <span className="text-muted-foreground">{t("admin.directory.noSync")}</span>
                      ) : c.lastSyncAt ? (
                        <div className="space-y-0.5">
                          <div
                            className={
                              c.lastSyncStatus === "error" ? "text-destructive" : undefined
                            }
                          >
                            {new Date(c.lastSyncAt).toLocaleString()} ·{" "}
                            {t(`admin.directory.syncStatus.${c.lastSyncStatus ?? "ok"}`)}
                          </div>
                          <div className="text-muted-foreground">
                            {t("admin.directory.sync.counts", {
                              users: c.lastSyncUsers ?? 0,
                              groups: c.lastSyncGroups ?? 0,
                            })}
                          </div>
                          {c.lastSyncStatus === "error" && c.lastSyncMessage && (
                            <div className="break-all font-mono text-muted-foreground" dir="ltr">
                              {c.lastSyncMessage}
                            </div>
                          )}
                        </div>
                      ) : (
                        <span className="text-muted-foreground">
                          {t("admin.directory.neverSynced")}
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busyId === c.id}
                          onClick={() => runTest(c)}
                        >
                          <PlugZapIcon className="h-4 w-4" />
                          {t("admin.directory.test.button")}
                        </Button>
                        {manage.hasPerm && c.kind === "ldap" && (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busyId === c.id || !c.enabled}
                            onClick={() => runSync(c)}
                          >
                            <RefreshCwIcon className="h-4 w-4" />
                            {t("admin.directory.sync.button")}
                          </Button>
                        )}
                        <Button size="sm" variant="ghost" onClick={() => setGroupsFor(c)}>
                          <UsersIcon className="h-4 w-4" />
                          {t("admin.directory.groups.button")}
                        </Button>
                        {manage.hasPerm && (
                          <>
                            <Button
                              size="sm"
                              variant="ghost"
                              aria-label={t("admin.directory.edit.title")}
                              onClick={() => openEdit(c)}
                            >
                              <PencilIcon className="h-4 w-4" />
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              aria-label={t("admin.directory.delete.title")}
                              onClick={() => setDeleting(c)}
                            >
                              <Trash2Icon className="h-4 w-4 text-muted-foreground" />
                            </Button>
                          </>
                        )}
                      </div>
                      <div className="mt-2">
                        <TestOutcome result={tests[c.id] ?? null} />
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <ConnectionDialog open={dialogOpen} onOpenChange={setDialogOpen} connection={editing} />
      <DeleteConnectionDialog connection={deleting} onClose={() => setDeleting(null)} />
      <GroupsDialog connection={groupsFor} onClose={() => setGroupsFor(null)} />
    </div>
  );
}

function DeleteConnectionDialog({
  connection,
  onClose,
}: {
  connection: DirectoryConnection | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const del = useDeleteDirectoryConnection();
  if (!connection) return null;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("admin.directory.delete.title")}</DialogTitle>
          <DialogDescription>
            {t("admin.directory.delete.body", { name: connection.name })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={del.isPending}
            onClick={() =>
              del.mutate(connection.id, {
                onSuccess: () => {
                  toast({ title: t("admin.directory.deleted"), variant: "success" });
                  onClose();
                },
                onError: () =>
                  toast({ title: t("admin.directory.delete.failed"), variant: "destructive" }),
              })
            }
          >
            {t("admin.directory.delete.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const GROUPS_PAGE = 10;

function GroupsDialog({
  connection,
  onClose,
}: {
  connection: DirectoryConnection | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [offset, setOffset] = useState(0);
  const query = useDirectoryGroups(connection?.id, offset, GROUPS_PAGE);
  if (!connection) return null;
  const items = query.data?.items ?? [];
  return (
    <Dialog
      open
      onOpenChange={(o) => {
        if (!o) {
          setOffset(0);
          onClose();
        }
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("admin.directory.groups.title", { name: connection.name })}</DialogTitle>
          <DialogDescription>{t("admin.directory.groups.description")}</DialogDescription>
        </DialogHeader>
        {query.isLoading ? (
          <LoadingState rows={4} />
        ) : query.error ? (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        ) : items.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("admin.directory.groups.empty")}</p>
        ) : (
          <div className="space-y-3">
            <div className="border border-border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("admin.directory.groups.name")}</TableHead>
                    <TableHead>{t("admin.directory.groups.members")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((g) => (
                    <TableRow key={g.id}>
                      <TableCell>
                        <div className="text-sm">{g.name}</div>
                        {g.description && (
                          <div className="text-xs text-muted-foreground">{g.description}</div>
                        )}
                      </TableCell>
                      <TableCell className="text-sm">{g.memberCount}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <Pagination
              offset={offset}
              limit={GROUPS_PAGE}
              total={query.data?.total ?? 0}
              onChange={setOffset}
              disabled={query.isFetching}
            />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
