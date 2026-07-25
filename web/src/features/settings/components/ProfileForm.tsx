/**
 * ProfileForm — manages the user's account-level fields.
 *
 * Surfaces: displayName, locale, email change, password change. The
 * shape maps to PATCH /api/v1/auth/me; we send only the fields the
 * user actually edited.
 */
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useToast } from "@/hooks/useToast";
import { useSessionStore } from "@/lib/stores/session-store";
import { useUpdateProfile } from "../api";
import { updateProfileSchema, type UpdateProfileValues } from "../schemas";

// Locale codes used internally; the dropdown label is fetched from
// `locale.<code>`.
const LOCALES = ["en", "fa"] as const;

export function ProfileForm() {
  const { t } = useTranslation();
  const user = useSessionStore((s) => s.user);
  const updateProfile = useUpdateProfile();
  const { toast } = useToast();
  const [pwdMismatch, setPwdMismatch] = useState(false);

  const form = useForm<UpdateProfileValues>({
    resolver: zodResolver(updateProfileSchema),
    defaultValues: {
      displayName: "",
      locale: "en",
      currentPassword: "",
      newPassword: "",
      newEmail: "",
    },
    mode: "onChange",
  });

  // Re-sync the form whenever the user data lands (the bootstrap query
  // fires on AppShell mount; this component may render before it
  // resolves, leaving the form blank). `form.reset()` is the documented
  // way to push new values into an already-mounted form.
  useEffect(() => {
    if (!user) return;
    form.reset({
      displayName: user.displayName ?? "",
      locale: "en",
      currentPassword: "",
      newPassword: "",
      newEmail: "",
    });
  }, [user, form]);

  // Sync the locale field with the i18n-side active locale so the dropdown
  // shows what the user is actually seeing, not a hardcoded "en".
  const { i18n } = useTranslation();
  useEffect(() => {
    const active = (i18n.language ?? "en").split(/[-_]/)[0] ?? "en";
    if (active === "en" || active === "fa") {
      if (form.getValues("locale") !== active) {
        form.setValue("locale", active);
      }
    }
  }, [i18n.language, form]);

  const onSubmit = form.handleSubmit((values) => {
    setPwdMismatch(false);
    // Only include fields the user actually edited; sending an empty
    // currentPassword would 400 the patch.
    const patch: UpdateProfileValues = {};
    if ((values.displayName ?? "") !== (user?.displayName ?? "")) {
      patch.displayName = values.displayName;
    }
    if (values.locale && values.locale !== "en") patch.locale = values.locale;
    if (values.newPassword) {
      if (!values.currentPassword) {
        setPwdMismatch(true);
        return;
      }
      patch.currentPassword = values.currentPassword;
      patch.newPassword = values.newPassword;
    }
    if (values.newEmail) patch.newEmail = values.newEmail;

    if (Object.keys(patch).length === 0) {
      toast({ title: t("toast.saved") });
      return;
    }
    void updateProfile.mutateAsync(patch);
  });

  const {
    register,
    formState: { errors, isSubmitting },
  } = form;

  return (
    <form onSubmit={onSubmit} className="space-y-6" noValidate>
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.profile.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="email">{t("settings.profile.email.label")}</Label>
              <Input id="email" value={user?.email ?? ""} disabled />
            </div>
            <div className="space-y-2">
              <Label htmlFor="displayName">{t("settings.profile.displayName.label")}</Label>
              <Input
                id="displayName"
                placeholder={t("settings.profile.displayName.placeholder")}
                {...register("displayName")}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="locale">{t("settings.profile.locale.label")}</Label>
              <Select
                value={form.watch("locale") ?? "en"}
                onValueChange={(v) => form.setValue("locale", v as "en" | "fa")}
              >
                <SelectTrigger id="locale">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {LOCALES.map((code) => (
                    <SelectItem key={code} value={code}>
                      {t(`locale.${code}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="flex justify-end">
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? t("settings.profile.submitting") : t("settings.profile.submit")}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.profile.changePassword.title")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="space-y-2">
              <Label htmlFor="currentPassword">
                {t("settings.profile.changePassword.current.label")}
              </Label>
              <Input
                id="currentPassword"
                type="password"
                autoComplete="current-password"
                placeholder={t("settings.profile.changePassword.current.placeholder")}
                {...register("currentPassword")}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="newPassword">{t("settings.profile.changePassword.new.label")}</Label>
              <Input
                id="newPassword"
                type="password"
                autoComplete="new-password"
                placeholder={t("settings.profile.changePassword.new.placeholder")}
                {...register("newPassword")}
              />
              {errors.newPassword && (
                <p className="text-xs text-destructive">{errors.newPassword.message}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="confirmPassword">
                {t("settings.profile.changePassword.confirm.label")}
              </Label>
              <Input
                id="confirmPassword"
                type="password"
                autoComplete="new-password"
                placeholder={t("settings.profile.changePassword.confirm.placeholder")}
                onChange={(e) => {
                  const v = e.target.value;
                  setPwdMismatch(
                    !!form.getValues("newPassword") && v !== form.getValues("newPassword"),
                  );
                }}
              />
              {pwdMismatch && (
                <p className="text-xs text-destructive">
                  {t("settings.profile.changePassword.mismatch")}
                </p>
              )}
            </div>
          </div>
          <div className="flex justify-end">
            <Button type="submit" variant="outline" disabled={isSubmitting}>
              {t("settings.profile.changePassword.submit")}
            </Button>
          </div>
        </CardContent>
      </Card>
    </form>
  );
}
