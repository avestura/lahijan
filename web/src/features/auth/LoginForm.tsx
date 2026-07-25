/**
 * LoginForm — email + password, react-hook-form + zod.
 *
 * Wires against the WS-06 /api/v1/auth/login endpoint via useSignIn. On
 * success the parent route redirects to /dashboard. On 401 the form shows
 * the localized "invalid email or password" message.
 */
import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useSignIn, MFARequiredError } from "./api";
import { loginSchema, type LoginValues } from "./schemas";

export function LoginForm() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const signIn = useSignIn();
  const [submitError, setSubmitError] = useState<string | null>(null);

  const form = useForm<LoginValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "" },
  });

  const onSubmit = form.handleSubmit((values) => {
    setSubmitError(null);
    void signIn
      .mutateAsync(values)
      .then(async () => {
        await navigate({ to: "/dashboard" });
      })
      .catch((err: unknown) => {
        if (err instanceof MFARequiredError) {
          // WS-18 only ships the password flow; full MFA UI lands with a
          // follow-up UI WS. Surface the localized message for now.
          setSubmitError(t("auth.login.error.mfaRequired"));
          return;
        }
        const message =
          err instanceof Error && err.message.includes("401")
            ? t("auth.login.error.invalid")
            : t("auth.login.error.network");
        setSubmitError(message);
      });
  });

  const {
    register,
    formState: { errors, isSubmitting },
  } = form;

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate data-testid="login-form">
      <div className="space-y-2">
        <Label htmlFor="email">{t("auth.login.email.label")}</Label>
        <Input
          id="email"
          type="email"
          autoComplete="email"
          placeholder={t("auth.login.email.placeholder")}
          aria-invalid={!!errors.email}
          data-testid="login-email"
          {...register("email")}
        />
        {errors.email && (
          <p className="text-sm text-destructive">
            {errors.email.type === "required"
              ? t("auth.login.email.required")
              : t("auth.login.email.invalid")}
          </p>
        )}
      </div>

      <div className="space-y-2">
        <Label htmlFor="password">{t("auth.login.password.label")}</Label>
        <Input
          id="password"
          type="password"
          autoComplete="current-password"
          placeholder={t("auth.login.password.placeholder")}
          aria-invalid={!!errors.password}
          data-testid="login-password"
          {...register("password")}
        />
        {errors.password && (
          <p className="text-sm text-destructive">
            {errors.password.type === "required"
              ? t("auth.login.password.required")
              : t("auth.login.password.min")}
          </p>
        )}
      </div>

      {submitError && (
        <p role="alert" data-testid="login-error" className="text-sm text-destructive">
          {submitError}
        </p>
      )}

      <Button type="submit" className="w-full" disabled={isSubmitting} data-testid="login-submit">
        {isSubmitting ? t("auth.login.submitting") : t("auth.login.submit")}
      </Button>
    </form>
  );
}
