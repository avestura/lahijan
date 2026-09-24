/**
 * IdentitiesCard — list + unlink OAuth/OIDC/SAML providers.
 *
 * The "link a new provider" flow lives on the login page (each
 * provider's /auth/oauth/<provider>/start endpoint is hit pre-session)
 * so this card is list + unlink only.
 */
import { useTranslation } from "react-i18next";
import { Link2OffIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { useIdentities, useUnlinkIdentity } from "../api";

export function IdentitiesCard() {
  const { t } = useTranslation();
  const query = useIdentities();
  const unlink = useUnlinkIdentity();

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("settings.identities.title")}</CardTitle>
      </CardHeader>
      <CardContent>
        {query.isLoading ? (
          <LoadingState rows={2} />
        ) : query.error ? (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        ) : (query.data ?? []).length === 0 ? (
          <EmptyState
            icon={Link2OffIcon}
            title={t("settings.identities.empty.title")}
            description={t("settings.identities.empty.body")}
          />
        ) : (
          <div className="border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("settings.identities.columns.provider")}</TableHead>
                  <TableHead>{t("settings.identities.columns.subject")}</TableHead>
                  <TableHead>{t("settings.identities.columns.scopes")}</TableHead>
                  <TableHead>{t("settings.identities.columns.created")}</TableHead>
                  <TableHead className="text-end">
                    {t("settings.identities.columns.provider")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(query.data ?? []).map((identity) => (
                  <TableRow key={identity.id}>
                    <TableCell className="font-mono text-xs">{identity.provider}</TableCell>
                    <TableCell className="font-mono text-xs">{identity.subject}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {(identity.scopes ?? []).map((s) => (
                          <Badge key={s} variant="outline">
                            {s}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {identity.createdAt ? new Date(identity.createdAt).toLocaleString() : ""}
                    </TableCell>
                    <TableCell className="text-end">
                      <Button
                        size="sm"
                        variant="destructive"
                        onClick={() => unlink.mutate({ identityId: identity.id })}
                        disabled={unlink.isPending}
                      >
                        {t("settings.identities.unlink")}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
