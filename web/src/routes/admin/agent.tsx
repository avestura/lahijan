/**
 * /admin/agent — the tenant's agent policy (WS-31).
 *
 * Tenant admins set the limits that shape every agent turn in the tenant:
 * the model allowlist, the per-window message rate cap, the spend cap, the
 * force-admin-models toggle (disables user BYOK), and the tool denylist.
 * The form is seeded from GET /api/v1/agent/policy and saved via PUT.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ShieldIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useTenant } from "@/hooks/useTenant";
import { usePerm } from "@/lib/perm";
import { useAgentPolicy, useUpdateAgentPolicy } from "@/features/agent/api";
import type { AgentPolicy } from "@/features/agent/api";

export const Route = createFileRoute("/admin/agent")({
  component: AdminAgentPolicyPage,
});

function AdminAgentPolicyPage() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const canManage = usePerm("agent.policy.manage");

  const policy = useAgentPolicy(tenantId);
  const update = useUpdateAgentPolicy(tenantId);

  const [allowModels, setAllowModels] = useState("");
  const [forceAdmin, setForceAdmin] = useState(false);
  const [maxMessages, setMaxMessages] = useState("0");
  const [windowSeconds, setWindowSeconds] = useState("60");
  const [spendCap, setSpendCap] = useState("0");
  const [denyTools, setDenyTools] = useState("");

  // Seed the form from the server value once it loads. useAgentPolicy
  // returns the permissive default when no row is set, so the form is
  // always editable. Arrays are guarded because the backend marshals an
  // unset slice as JSON null.
  useEffect(() => {
    if (!policy.data) return;
    setAllowModels((policy.data.allowModels ?? []).join(", "));
    setForceAdmin(policy.data.forceAdminModels);
    setMaxMessages(String(policy.data.maxMessagesPerWindow));
    setWindowSeconds(String(policy.data.windowSeconds));
    setSpendCap(String(policy.data.spendCapCredits));
    setDenyTools((policy.data.denyTools ?? []).join(", "));
  }, [policy.data]);

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (update.isPending) return;
    const next: AgentPolicy = {
      allowModels: splitCsv(allowModels),
      forceAdminModels: forceAdmin,
      maxMessagesPerWindow: Number(maxMessages) || 0,
      windowSeconds: Number(windowSeconds) || 60,
      spendCapCredits: Number(spendCap) || 0,
      denyTools: splitCsv(denyTools),
    };
    update.mutate(next);
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
          <ShieldIcon className="h-6 w-6" />
          {t("agent.policy.title")}
        </h1>
        <p className="text-sm text-muted-foreground">{t("agent.policy.subtitle")}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t("agent.policy.limitsTitle")}</CardTitle>
          <CardDescription>{t("agent.policy.limitsDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid grid-cols-1 gap-4 sm:grid-cols-2" onSubmit={onSubmit}>
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="ap-allow">{t("agent.policy.allowModels")}</Label>
              <Input
                id="ap-allow"
                value={allowModels}
                onChange={(e) => setAllowModels(e.target.value)}
                placeholder={t("agent.policy.allowModelsPlaceholder")}
                disabled={!canManage.hasPerm}
              />
              <p className="text-xs text-muted-foreground">{t("agent.policy.allowModelsHint")}</p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="ap-max">{t("agent.policy.maxMessages")}</Label>
              <Input
                id="ap-max"
                type="number"
                min={0}
                value={maxMessages}
                onChange={(e) => setMaxMessages(e.target.value)}
                disabled={!canManage.hasPerm}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ap-window">{t("agent.policy.windowSeconds")}</Label>
              <Input
                id="ap-window"
                type="number"
                min={1}
                value={windowSeconds}
                onChange={(e) => setWindowSeconds(e.target.value)}
                disabled={!canManage.hasPerm}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ap-spend">{t("agent.policy.spendCap")}</Label>
              <Input
                id="ap-spend"
                type="number"
                min={0}
                value={spendCap}
                onChange={(e) => setSpendCap(e.target.value)}
                disabled={!canManage.hasPerm}
              />
            </div>
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="ap-deny">{t("agent.policy.denyTools")}</Label>
              <Input
                id="ap-deny"
                value={denyTools}
                onChange={(e) => setDenyTools(e.target.value)}
                placeholder={t("agent.policy.denyToolsPlaceholder")}
                disabled={!canManage.hasPerm}
              />
            </div>

            <div className="flex items-center gap-2 sm:col-span-2">
              <Checkbox
                id="ap-force"
                checked={forceAdmin}
                onCheckedChange={(v) => setForceAdmin(v === true)}
                disabled={!canManage.hasPerm}
              />
              <Label htmlFor="ap-force" className="cursor-pointer">
                {t("agent.policy.forceAdmin")}
              </Label>
            </div>

            <div className="sm:col-span-2">
              <Button type="submit" disabled={!canManage.hasPerm || update.isPending}>
                {t("agent.policy.save")}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

/** splitCsv turns the comma-separated input into a clean string array. */
function splitCsv(input: string): string[] {
  return input
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}
