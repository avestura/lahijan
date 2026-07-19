/**
 * StorageCredentialsCard — list + mint + reveal-once + revoke.
 *
 * Mirrors the WS-06 PAT reveal-once pattern: the plaintext secret
 * is shown exactly once in a follow-up dialog; the user must copy
 * it before dismissing.
 */
import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { KeyIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
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
import { usePerm } from "@/lib/perm";
import { useToast } from "@/hooks/useToast";
import {
  useCreateStorageCredential,
  useRevokeStorageCredential,
  useStorageCredentials,
} from "../api";
import {
  CREDENTIAL_ACTIONS,
  createCredentialSchema,
  type CreateCredentialValues,
  type CredentialAction,
} from "../schemas";

interface Props {
  tenantId: string | null;
  bucketId: string | undefined;
}

export function StorageCredentialsCard({ tenantId, bucketId }: Props) {
  const { t } = useTranslation();
  const query = useStorageCredentials(tenantId, bucketId);
  const { toast } = useToast();
  const { hasPerm: canCreate } = usePerm("s3.credentials.create");
  const { hasPerm: canRevoke } = usePerm("s3.credentials.revoke");
  const [createOpen, setCreateOpen] = React.useState(false);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div className="space-y-1">
          <CardTitle className="text-base">{t("storage.credentials.title")}</CardTitle>
          <p className="text-xs text-muted-foreground">{t("storage.credentials.subtitle")}</p>
        </div>
        {canCreate && (
          <Button variant="outline" size="sm" onClick={() => setCreateOpen(true)}>
            {t("storage.credentials.new")}
          </Button>
        )}
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
            title={t("storage.credentials.empty.title")}
            description={t("storage.credentials.empty.body")}
          />
        ) : (
          <div className="rounded-md border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("storage.credentials.columns.accessKeyId")}</TableHead>
                  <TableHead>{t("storage.credentials.columns.label")}</TableHead>
                  <TableHead>{t("storage.credentials.columns.lastUsed")}</TableHead>
                  <TableHead>{t("storage.credentials.columns.expires")}</TableHead>
                  <TableHead className="text-end">
                    {t("storage.credentials.columns.actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(query.data ?? []).map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-mono text-xs">{c.accessKeyId}</TableCell>
                    <TableCell className="text-xs">{c.label ?? t("common.none")}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {c.lastUsedAt ? new Date(c.lastUsedAt).toLocaleString() : t("common.none")}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {c.expiresAt ? new Date(c.expiresAt).toLocaleString() : t("common.none")}
                    </TableCell>
                    <TableCell className="text-end">
                      {canRevoke && (
                        <RevokeCredential
                          credentialId={c.id}
                          tenantId={tenantId}
                          bucketId={bucketId}
                        />
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      <CreateCredentialDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        tenantId={tenantId}
        bucketId={bucketId}
        onCopied={() => toast({ title: t("common.copied"), variant: "success" })}
      />
    </Card>
  );
}

function RevokeCredential({
  credentialId,
  tenantId,
  bucketId,
}: {
  credentialId: string;
  tenantId: string | null;
  bucketId: string | undefined;
}) {
  const { t } = useTranslation();
  const revoke = useRevokeStorageCredential(tenantId, bucketId);
  const [open, setOpen] = React.useState(false);

  return (
    <>
      <Button size="sm" variant="destructive" onClick={() => setOpen(true)}>
        {t("storage.credentials.revoke")}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("storage.credentials.revokeConfirm.title")}</DialogTitle>
            <DialogDescription>{t("storage.credentials.revokeConfirm.body")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={revoke.isPending}
              onClick={() => {
                revoke.mutate({ credentialId }, { onSettled: () => setOpen(false) });
              }}
            >
              {t("storage.credentials.revoke")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

interface CreateProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tenantId: string | null;
  bucketId: string | undefined;
  onCopied: () => void;
}

function CreateCredentialDialog({ open, onOpenChange, tenantId, bucketId, onCopied }: CreateProps) {
  const { t } = useTranslation();
  const create = useCreateStorageCredential(tenantId, bucketId);
  const [revealOpen, setRevealOpen] = React.useState(false);

  const form = useForm<CreateCredentialValues>({
    resolver: zodResolver(createCredentialSchema),
    defaultValues: { label: "", actions: ["Read"], expiresInSeconds: undefined },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await create.mutateAsync(values);
    onOpenChange(false);
    setRevealOpen(true);
    form.reset({ label: "", actions: ["Read"], expiresInSeconds: undefined });
  });

  const selectedActions = form.watch("actions");
  const toggleAction = (a: CredentialAction) => {
    const next = selectedActions.includes(a)
      ? selectedActions.filter((x) => x !== a)
      : [...selectedActions, a];
    form.setValue("actions", next, { shouldValidate: true });
  };

  const copySecret = async () => {
    if (!create.data?.secretKey) return;
    try {
      await navigator.clipboard.writeText(create.data.secretKey);
      onCopied();
    } catch {
      /* ignore — clipboard may be unavailable in tests/sandbox */
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("storage.credentials.create.title")}</DialogTitle>
            <DialogDescription>{t("storage.credentials.subtitle")}</DialogDescription>
          </DialogHeader>
          <form onSubmit={onSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="cred-label">{t("storage.credentials.create.label.label")}</Label>
              <Input
                id="cred-label"
                placeholder={t("storage.credentials.create.label.placeholder")}
                {...form.register("label")}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("storage.credentials.create.actions.label")}</Label>
              <div className="flex flex-wrap gap-3">
                {CREDENTIAL_ACTIONS.map((a) => (
                  <label key={a} className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={selectedActions.includes(a)}
                      onCheckedChange={() => toggleAction(a)}
                    />
                    {t(`storage.credentials.create.action.${a}`)}
                  </label>
                ))}
              </div>
              {form.formState.errors.actions && (
                <p className="text-xs text-destructive">{t("common.required")}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="cred-exp">
                {t("storage.credentials.create.expiresInSeconds.label")}
              </Label>
              <Input
                id="cred-exp"
                type="number"
                min={1}
                placeholder="86400"
                {...form.register("expiresInSeconds", {
                  setValueAs: (v) => {
                    if (v === "" || v === null || v === undefined) return undefined;
                    const n = Number(v);
                    return Number.isFinite(n) ? n : undefined;
                  },
                })}
              />
              <p className="text-xs text-muted-foreground">
                {t("storage.credentials.create.expiresInSeconds.hint")}
              </p>
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={create.isPending}>
                {create.isPending
                  ? t("storage.credentials.create.submitting")
                  : t("storage.credentials.create.submit")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={revealOpen} onOpenChange={setRevealOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("storage.credentials.reveal.title")}</DialogTitle>
            <DialogDescription>{t("storage.credentials.reveal.warning")}</DialogDescription>
          </DialogHeader>
          {create.data && (
            <div className="space-y-2">
              <div>
                <Label>{t("storage.credentials.reveal.accessKeyId")}</Label>
                <div className="rounded-md border border-border bg-muted/50 p-3">
                  <code className="block break-all font-mono text-xs">
                    {create.data.credential.accessKeyId}
                  </code>
                </div>
              </div>
              <div>
                <Label>{t("storage.credentials.reveal.secretKey")}</Label>
                <div className="rounded-md border border-border bg-muted/50 p-3">
                  <code className="block break-all font-mono text-xs">{create.data.secretKey}</code>
                </div>
              </div>
              <div className="flex flex-wrap gap-1">
                {create.data.credential.actions.map((a) => (
                  <Badge key={a} variant="outline">
                    {t(`storage.credentials.create.action.${a}`)}
                  </Badge>
                ))}
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={copySecret}>
              {t("storage.credentials.reveal.copySecret")}
            </Button>
            <Button onClick={() => setRevealOpen(false)}>
              {t("storage.credentials.reveal.done")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
