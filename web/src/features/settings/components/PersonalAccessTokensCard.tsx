/**
 * PersonalAccessTokensCard — list + create + revoke.
 *
 * On create, the response carries the raw token ONCE. We surface a
 * one-shot "Copy now" dialog; after the user dismisses it the token
 * value is gone forever (the API never returns it again).
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { KeyIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { useToast } from "@/hooks/useToast";
import {
  useCreatePersonalAccessToken,
  usePersonalAccessTokens,
  useRevokePersonalAccessToken,
} from "../api";

export function PersonalAccessTokensCard() {
  const { t } = useTranslation();
  const query = usePersonalAccessTokens();
  const create = useCreatePersonalAccessToken();
  const revoke = useRevokePersonalAccessToken();
  const { toast } = useToast();
  const [createOpen, setCreateOpen] = useState(false);
  const [revealOpen, setRevealOpen] = useState(false);
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState("");

  const onCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    const scopeList = scopes
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    create.mutate(
      { name: name.trim(), scopes: scopeList },
      {
        onSuccess: () => {
          setCreateOpen(false);
          setRevealOpen(true);
          setName("");
          setScopes("");
        },
      },
    );
  };

  const onCopyToken = async () => {
    if (!create.data?.token) return;
    try {
      await navigator.clipboard.writeText(create.data.token);
      toast({ title: t("common.copied"), variant: "success" });
    } catch {
      toast({ title: t("common.error"), variant: "destructive" });
    }
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{t("settings.tokens.title")}</CardTitle>
        <Button variant="outline" size="sm" onClick={() => setCreateOpen(true)}>
          {t("settings.tokens.new")}
        </Button>
      </CardHeader>
      <CardContent>
        {query.isLoading ? (
          <LoadingState rows={3} />
        ) : query.error ? (
          <ErrorState
            message={t("common.error")}
            retryLabel={t("common.retry")}
            onRetry={() => void query.refetch()}
          />
        ) : (query.data ?? []).length === 0 ? (
          <EmptyState
            icon={KeyIcon}
            title={t("settings.tokens.empty.title")}
            description={t("settings.tokens.empty.body")}
          />
        ) : (
          <div className="rounded-md border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("settings.tokens.columns.name")}</TableHead>
                  <TableHead>{t("settings.tokens.columns.scopes")}</TableHead>
                  <TableHead>{t("settings.tokens.columns.lastUsed")}</TableHead>
                  <TableHead>{t("settings.tokens.columns.expires")}</TableHead>
                  <TableHead className="text-end">
                    {t("settings.tokens.columns.actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(query.data ?? []).map((tok) => (
                  <TableRow key={tok.id}>
                    <TableCell className="font-medium">{tok.name}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {(tok.scopes ?? []).map((s) => (
                          <Badge key={s} variant="outline">
                            {s}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {tok.lastUsedAt
                        ? new Date(tok.lastUsedAt).toLocaleString()
                        : t("common.none")}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {tok.expiresAt ? new Date(tok.expiresAt).toLocaleString() : t("settings.tokens.create.expiry.never")}
                    </TableCell>
                    <TableCell className="text-end">
                      <Button
                        size="sm"
                        variant="destructive"
                        onClick={() => revoke.mutate({ tokenId: tok.id })}
                        disabled={revoke.isPending}
                      >
                        {t("settings.tokens.revoke")}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("settings.tokens.create.title")}</DialogTitle>
            <DialogDescription>{t("settings.tokens.subtitle")}</DialogDescription>
          </DialogHeader>
          <form onSubmit={onCreate} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="pat-name">{t("settings.tokens.create.name.label")}</Label>
              <Input
                id="pat-name"
                placeholder={t("settings.tokens.create.name.placeholder")}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pat-scopes">{t("settings.tokens.create.scopes.label")}</Label>
              <Input
                id="pat-scopes"
                placeholder={t("settings.tokens.create.scopes.placeholder")}
                value={scopes}
                onChange={(e) => setScopes(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                {t("settings.tokens.create.scopes.hint")}
              </p>
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setCreateOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={create.isPending || !name.trim()}>
                {create.isPending
                  ? t("settings.tokens.create.submitting")
                  : t("settings.tokens.create.submit")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={revealOpen} onOpenChange={setRevealOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("settings.tokens.reveal.title")}</DialogTitle>
            <DialogDescription>{t("settings.tokens.reveal.body")}</DialogDescription>
          </DialogHeader>
          {create.data?.token && (
            <div className="rounded-md border border-border bg-muted/50 p-3">
              <code className="block break-all font-mono text-xs">{create.data.token}</code>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={onCopyToken}>
              {t("settings.tokens.reveal.copy")}
            </Button>
            <Button onClick={() => setRevealOpen(false)}>
              {t("settings.tokens.reveal.done")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
