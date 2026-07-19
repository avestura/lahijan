/**
 * /settings/security — MFA enrollment (TOTP, WebAuthn, recovery).
 */
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { TOTPCard } from "@/features/settings/components/TOTPCard";
import { WebAuthnCard } from "@/features/settings/components/WebAuthnCard";
import { RecoveryCodesCard } from "@/features/settings/components/RecoveryCodesCard";

export const Route = createFileRoute("/settings/security")({
  component: SettingsSecurityPage,
});

function SettingsSecurityPage() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("settings.security.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("settings.security.subtitle")}</p>
      </header>
      <div className="space-y-4">
        <TOTPCard />
        <RecoveryCodesCard />
        <WebAuthnCard />
      </div>
    </div>
  );
}
