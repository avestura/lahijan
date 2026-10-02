/**
 * /admin/users — platform user management.
 *
 * Lists every user (paginated, searchable) and lets a platform admin create,
 * edit (profile, sign-in state, role per tenant) and delete users. Users that
 * came from an LDAP directory show their source. Connections to external
 * directories are managed at /admin/directory.
 */
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { NetworkIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { usePerm } from "@/lib/perm";
import { useSessionStore } from "@/lib/stores/session-store";
import type { AdminUser } from "@/features/admin/api";
import { UsersTable } from "@/features/admin/components/UsersTable";
import {
  CreateUserDialog,
  DeleteUserDialog,
  EditUserDialog,
} from "@/features/admin/components/UserDialogs";

export const Route = createFileRoute("/admin/users")({
  component: AdminUsersPage,
});

function AdminUsersPage() {
  const { t } = useTranslation();
  const manage = usePerm("platform.user.manage");
  const currentUserId = useSessionStore((s) => s.user?.id);
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<AdminUser | null>(null);
  const [deleting, setDeleting] = useState<AdminUser | null>(null);

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">{t("admin.users.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("admin.users.subtitle")}</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" asChild>
            <Link to="/admin/directory">
              <NetworkIcon className="h-4 w-4" />
              {t("admin.users.directories")}
            </Link>
          </Button>
          {manage.hasPerm && (
            <Button onClick={() => setCreateOpen(true)}>
              <PlusIcon className="h-4 w-4" />
              {t("admin.users.new")}
            </Button>
          )}
        </div>
      </header>

      <UsersTable
        actionsLabel={manage.hasPerm ? t("admin.users.columns.actions") : undefined}
        renderActions={(u) => (
          <>
            <Button size="sm" variant="outline" onClick={() => setEditing(u)}>
              <PencilIcon className="h-4 w-4" />
              {t("admin.users.edit.button")}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={u.id === currentUserId}
              aria-label={t("admin.users.delete.title")}
              onClick={() => setDeleting(u)}
            >
              <Trash2Icon className="h-4 w-4 text-muted-foreground" />
            </Button>
          </>
        )}
      />

      <CreateUserDialog open={createOpen} onOpenChange={setCreateOpen} />
      <EditUserDialog
        open={!!editing}
        onOpenChange={(o) => !o && setEditing(null)}
        user={editing}
        currentUserId={currentUserId}
      />
      <DeleteUserDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        user={deleting}
      />
    </div>
  );
}
