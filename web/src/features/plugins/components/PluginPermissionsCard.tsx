/**
 * PluginPermissionsCard — list + grant + revoke.
 *
 * Reads the manifest's requested permissions from the plugin row
 * (the backend stores the parsed manifest verbatim, per WS-10a).
 * Granted permissions come from `plugin.permissions`.
 */
import { useTranslation } from "react-i18next";
import { ShieldCheckIcon } from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { usePerm } from "@/lib/perm";
import { useSetAdminPluginPermission } from "../api";

type AdminPlugin = components["schemas"]["AdminPlugin"];

interface Props {
  plugin: AdminPlugin;
}

export function PluginPermissionsCard({ plugin }: Props) {
  const { t } = useTranslation();
  const setPermission = useSetAdminPluginPermission();
  const { hasPerm } = usePerm("plugins.permission.approve");

  // Requested permissions live in the manifest; we treat them as the
  // canonical source of "what this plugin wants". The exact shape is
  // documented in internal/app/lahijan/wasm/manifest/manifest.go.
  const manifest = plugin.manifest as { permissions?: string[] } | undefined;
  const requested = manifest?.permissions ?? [];
  const granted = new Set(plugin.permissions ?? []);

  const all = Array.from(new Set([...requested, ...Array.from(granted)]));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <ShieldCheckIcon className="h-4 w-4" />
          {t("plugins.permissions.title")}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {all.length === 0 ? (
          <EmptyState icon={ShieldCheckIcon} title={t("plugins.permissions.none")} />
        ) : (
          <ul className="space-y-2">
            {all.map((perm) => {
              const isGranted = granted.has(perm);
              return (
                <li
                  key={perm}
                  className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2"
                >
                  <code className="font-mono text-xs">{perm}</code>
                  <div className="flex items-center gap-2">
                    <Badge variant={isGranted ? "default" : "secondary"}>
                      {isGranted
                        ? t("plugins.permissions.granted")
                        : t("plugins.permissions.requested")}
                    </Badge>
                    {hasPerm &&
                      (isGranted ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={setPermission.isPending}
                          onClick={() =>
                            setPermission.mutate({
                              pluginId: plugin.id,
                              permission: perm,
                              action: "revoke",
                            })
                          }
                        >
                          {t("plugins.actions.revoke")}
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={setPermission.isPending}
                          onClick={() =>
                            setPermission.mutate({
                              pluginId: plugin.id,
                              permission: perm,
                              action: "grant",
                            })
                          }
                        >
                          {t("plugins.actions.grant")}
                        </Button>
                      ))}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
