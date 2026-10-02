/**
 * /admin/billing — admin billing overview with prices tab + users tab.
 *
 * Prices tab embeds AdminPricesCard (CRUD). Users tab lists every user in a
 * paginated, searchable table; "Manage billing" on a row opens
 * AdminUserActionsCard (top-up + refund + link to per-user ledger).
 *
 * Only platform admins reach this page (the _admin layout guard
 * redirects everyone else).
 */
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { AdminUser } from "@/features/admin/api";
import { UsersTable } from "@/features/admin/components/UsersTable";
import { AdminPricesCard } from "@/features/billing/components/AdminPricesCard";
import { AdminUserActionsCard } from "@/features/billing/components/AdminUserActionsCard";

export const Route = createFileRoute("/admin/billing/")({
  component: AdminBillingPage,
});

const TABS = ["prices", "users"] as const;
type AdminTab = (typeof TABS)[number];

function AdminBillingPage() {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<AdminUser | null>(null);

  const renderTab = (tab: AdminTab) => {
    switch (tab) {
      case "prices":
        return <AdminPricesCard />;
      case "users":
        return (
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{t("billing.admin.users.title")}</CardTitle>
              <p className="text-xs text-muted-foreground">{t("billing.admin.users.subtitle")}</p>
            </CardHeader>
            <CardContent className="space-y-4">
              {selected && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between gap-2 text-sm">
                    <span className="font-medium">{selected.email}</span>
                    <Button size="sm" variant="ghost" onClick={() => setSelected(null)}>
                      {t("common.close")}
                    </Button>
                  </div>
                  <AdminUserActionsCard userId={selected.id} />
                </div>
              )}
              <UsersTable
                selectedId={selected?.id}
                actionsLabel={t("billing.admin.users.columns.actions")}
                renderActions={(u) => (
                  <Button size="sm" variant="outline" onClick={() => setSelected(u)}>
                    {t("billing.admin.users.manage")}
                  </Button>
                )}
              />
            </CardContent>
          </Card>
        );
    }
  };

  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("billing.admin.title")}</h1>
      </header>
      <Tabs defaultValue={TABS[0]}>
        <TabsList>
          {TABS.map((tab) => (
            <TabsTrigger key={tab} value={tab}>
              {t(`billing.admin.tabs.${tab}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        {TABS.map((tab) => (
          <TabsContent key={tab} value={tab}>
            {renderTab(tab)}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}
