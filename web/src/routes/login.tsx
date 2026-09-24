/**
 * `/login` — public sign-in route.
 *
 * Renders the marketing-light layout (centered card). If the session store
 * already has a user, redirect to /dashboard.
 */
import { useEffect } from "react";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { LoginForm } from "@/features/auth/LoginForm";
import { Card, CardContent, CardDescription, CardTitle } from "@/components/ui/card";
import { useAuth } from "@/hooks/useAuth";
import { useSessionStore } from "@/lib/stores/session-store";

export const Route = createFileRoute("/login")({
  beforeLoad: () => {
    // beforeLoad runs outside React; read directly from the store.
    const { user } = useSessionStore.getState();
    if (user) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error
      throw redirect({ to: "/dashboard" });
    }
  },
  component: LoginRoute,
});

function LoginRoute() {
  const { t } = useTranslation();
  // /login renders outside AppShell, which is where the /auth/me bootstrap
  // normally fires; run it here too so a signed-in visitor is recognised.
  useAuth();
  const status = useSessionStore((s) => s.status);
  const navigate = useNavigate();
  // beforeLoad only sees the store as it is at navigation time. On a full
  // page load of /login the /auth/me bootstrap has not resolved yet, so an
  // already-signed-in user would be left on the login form; redirect once
  // the session resolves.
  useEffect(() => {
    if (status === "authenticated") {
      void navigate({ to: "/dashboard", replace: true });
    }
  }, [status, navigate]);
  return (
    <div
      className="flex min-h-screen items-center justify-center bg-background p-4"
      data-testid="login-page"
    >
      <Card className="w-full max-w-md">
        <CardContent className="space-y-6 p-8">
          <div className="space-y-2 text-center">
            <CardTitle className="text-2xl">{t("auth.login.title")}</CardTitle>
            <CardDescription>{t("auth.login.subtitle")}</CardDescription>
          </div>
          <LoginForm />
        </CardContent>
      </Card>
    </div>
  );
}
