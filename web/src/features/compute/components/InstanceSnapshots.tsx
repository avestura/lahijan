/**
 * InstanceSnapshots — the Snapshots tab on the instance detail page.
 *
 * The backend (WS-14) lists `GET/POST /instances/{id}/snapshots` as
 * in-scope but does not implement them; they land with WS-25
 * (scheduled snapshots + backups). Rather than build a UI against an
 * endpoint that 404s, we surface the explicit "coming soon" copy
 * here so the user understands the gap.
 *
 * When WS-25 lands this panel will gain:
 *   - a list of existing snapshots (with restore/delete buttons)
 *   - a "Create snapshot" dialog (name + optional expiry)
 */
import { CameraIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Card, CardContent } from "@/components/ui/card";

export function InstanceSnapshots() {
  const { t } = useTranslation();
  return (
    <Card>
      <CardContent className="flex items-start gap-3 p-6">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <CameraIcon className="h-5 w-5" aria-hidden="true" />
        </div>
        <div className="space-y-1">
          <p className="text-sm font-medium">{t("compute.snapshots.title")}</p>
          <p className="text-sm text-muted-foreground">{t("compute.snapshots.comingSoon")}</p>
        </div>
      </CardContent>
    </Card>
  );
}
