/**
 * Dialogs for /admin/users: create a user, edit one (profile, sign-in state and
 * the role in each tenant) and confirm deletion.
 */
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useToast } from "@/hooks/useToast";
import {
  useCreateAdminUser,
  useDeleteAdminUser,
  useSetAdminUserRole,
  useUpdateAdminUser,
  type AdminUser,
} from "../api";
import { ROLE_OPTIONS, roleKey } from "../roles";

/** Languages are listed by their own name, whatever the UI language is. */
const LOCALES = [
  { value: "en", label: "English" },
  { value: "fa", label: "فارسی" },
] as const;

interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

export function CreateUserDialog({ open, onOpenChange }: DialogProps) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const create = useCreateAdminUser();
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [locale, setLocale] = useState<"en" | "fa">("en");
  const [active, setActive] = useState(true);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (open) {
      setEmail("");
      setDisplayName("");
      setPassword("");
      setLocale("en");
      setActive(true);
      setFailed(false);
    }
  }, [open]);

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!email.trim() || create.isPending) return;
    setFailed(false);
    create.mutate(
      {
        email: email.trim(),
        displayName: displayName.trim() || undefined,
        password: password || undefined,
        locale,
        isActive: active,
      },
      {
        onSuccess: () => {
          toast({ title: t("admin.users.created"), variant: "success" });
          onOpenChange(false);
        },
        onError: () => setFailed(true),
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("admin.users.create.title")}</DialogTitle>
          <DialogDescription>{t("admin.users.create.description")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="au-email">{t("admin.users.fields.email")}</Label>
            <Input
              id="au-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="off"
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="au-name">{t("admin.users.fields.displayName")}</Label>
            <Input
              id="au-name"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="au-password">{t("admin.users.fields.password")}</Label>
            <Input
              id="au-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
            />
            <p className="text-xs text-muted-foreground">{t("admin.users.fields.passwordHint")}</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="au-locale">{t("admin.users.fields.locale")}</Label>
            <Select value={locale} onValueChange={(v) => setLocale(v as "en" | "fa")}>
              <SelectTrigger id="au-locale">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {LOCALES.map((l) => (
                  <SelectItem key={l.value} value={l.value}>
                    {l.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex items-center gap-2">
            <Checkbox
              id="au-active"
              checked={active}
              onCheckedChange={(v) => setActive(v === true)}
            />
            <Label htmlFor="au-active" className="cursor-pointer">
              {t("admin.users.fields.active")}
            </Label>
          </div>
          {failed && (
            <p role="alert" className="text-sm text-destructive">
              {t("admin.users.create.failed")}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={create.isPending || !email.trim()}>
              {t("admin.users.create.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Edit
// ---------------------------------------------------------------------------

interface EditProps extends DialogProps {
  user: AdminUser | null;
  /** The signed-in admin; they cannot disable themselves. */
  currentUserId: string | undefined;
}

export function EditUserDialog({ open, onOpenChange, user, currentUserId }: EditProps) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const update = useUpdateAdminUser();
  const setRole = useSetAdminUserRole();
  const [displayName, setDisplayName] = useState("");
  const [active, setActive] = useState(true);
  const [roles, setRoles] = useState<Record<string, string>>({});
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!user) return;
    setDisplayName(user.displayName ?? "");
    setActive(user.isActive);
    setRoles(Object.fromEntries(user.memberships.map((m) => [m.tenantId, m.role])));
    setFailed(false);
  }, [user]);

  if (!user) return null;
  const isSelf = user.id === currentUserId;
  const busy = update.isPending || setRole.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!user || busy) return;
    setFailed(false);
    try {
      const body: { displayName?: string; isActive?: boolean } = {};
      if (displayName.trim() !== (user.displayName ?? "")) body.displayName = displayName.trim();
      if (active !== user.isActive) body.isActive = active;
      if (Object.keys(body).length > 0) {
        await update.mutateAsync({ userId: user.id, body });
      }
      for (const m of user.memberships) {
        const next = roles[m.tenantId];
        if (next && next !== m.role) {
          await setRole.mutateAsync({ userId: user.id, tenantId: m.tenantId, role: next });
        }
      }
      toast({ title: t("admin.users.saved"), variant: "success" });
      onOpenChange(false);
    } catch {
      setFailed(true);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("admin.users.edit.title")}</DialogTitle>
          <DialogDescription>{user.email}</DialogDescription>
        </DialogHeader>
        <form onSubmit={(e) => void onSubmit(e)} className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="eu-name">{t("admin.users.fields.displayName")}</Label>
            <Input
              id="eu-name"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </div>
          <div className="flex items-center gap-2">
            <Checkbox
              id="eu-active"
              checked={active}
              disabled={isSelf}
              onCheckedChange={(v) => setActive(v === true)}
            />
            <Label htmlFor="eu-active" className="cursor-pointer">
              {t("admin.users.fields.active")}
            </Label>
          </div>
          {isSelf && (
            <p className="text-xs text-muted-foreground">{t("admin.users.edit.selfHint")}</p>
          )}

          <div className="space-y-2">
            <Label>{t("admin.users.edit.memberships")}</Label>
            {user.memberships.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("admin.users.edit.noMemberships")}</p>
            ) : (
              <ul className="space-y-2">
                {user.memberships.map((m) => (
                  <li key={m.tenantId} className="flex items-center justify-between gap-3">
                    <span className="min-w-0 truncate text-sm">{m.tenantName}</span>
                    <Select
                      value={roles[m.tenantId] ?? m.role}
                      onValueChange={(v) => setRoles((r) => ({ ...r, [m.tenantId]: v }))}
                    >
                      <SelectTrigger
                        className="w-48"
                        aria-label={t("admin.users.edit.roleIn", { tenant: m.tenantName })}
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {ROLE_OPTIONS.map((r) => (
                          <SelectItem key={r.value} value={r.value}>
                            {t(`admin.roles.${roleKey(r.value)}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {failed && (
            <p role="alert" className="text-sm text-destructive">
              {t("admin.users.edit.failed")}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={busy}>
              {t("common.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

export function DeleteUserDialog({
  open,
  onOpenChange,
  user,
}: DialogProps & { user: AdminUser | null }) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const del = useDeleteAdminUser();
  if (!user) return null;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("admin.users.delete.title")}</DialogTitle>
          <DialogDescription>
            {t("admin.users.delete.body", { email: user.email })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={del.isPending}
            onClick={() =>
              del.mutate(user.id, {
                onSuccess: () => {
                  toast({ title: t("admin.users.deleted"), variant: "success" });
                  onOpenChange(false);
                },
                onError: () =>
                  toast({ title: t("admin.users.delete.failed"), variant: "destructive" }),
              })
            }
          >
            {t("admin.users.delete.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
