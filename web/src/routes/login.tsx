/**
 * `/login` — public sign-in route.
 *
 * Renders the marketing-light layout (centered card). If the session store
 * already has a user, redirect to /dashboard.
 */
import { createFileRoute, redirect } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { LoginForm } from "@/features/auth/LoginForm";
import { Card, CardContent, CardDescription, CardTitle } from "@/components/ui/card";
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
  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
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
