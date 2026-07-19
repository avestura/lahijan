/**
 * DNSRecordList — table of records in a zone with inline-create + edit
 * + bulk-delete. The record type filter dropdown is driven from the
 * RECORD_TYPES constant (mirrors the OpenAPI enum).
 *
 * Per WS-15: every privileged action is gated by `usePerm` here and
 * enforced server-side.
 */
import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { FileTextIcon, Trash2Icon } from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import {
  useCreateDNSRecord,
  useDeleteDNSRecord,
  useDNSRecords,
  useUpdateDNSRecord,
} from "../api";
import {
  createRecordSchema,
  updateRecordSchema,
  RECORD_TYPES,
  type CreateRecordValues,
  type DNSRecordType,
  type UpdateRecordValues,
} from "../schemas";

type DNSRecord = components["schemas"]["DNSRecord"];

interface Props {
  tenantId: string | null;
  zoneId: string | undefined;
  nameFilter: string;
  typeFilter: DNSRecordType | "all";
}

export function DNSRecordList({ tenantId, zoneId, nameFilter, typeFilter }: Props) {
  const { t } = useTranslation();
  const query = useDNSRecords(tenantId, zoneId);
  const destroy = useDeleteDNSRecord(tenantId, zoneId);

  const [selected, setSelected] = React.useState<Set<string>>(new Set());
  const [createOpen, setCreateOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<DNSRecord | null>(null);

  const { hasPerm: canCreate } = usePerm("dns.record.create");
  const { hasPerm: canUpdate } = usePerm("dns.record.update");
  const { hasPerm: canDelete } = usePerm("dns.record.delete");

  const filtered = React.useMemo(() => {
    const list = query.data ?? [];
    const lower = nameFilter.trim().toLowerCase();
    return list.filter((rec) => {
      const matchesText =
        !lower ||
        rec.name.toLowerCase().includes(lower) ||
        rec.content.toLowerCase().includes(lower);
      const matchesType = typeFilter === "all" || rec.type === typeFilter;
      return matchesText && matchesType;
    });
  }, [query.data, nameFilter, typeFilter]);

  if (query.isLoading) {
    return <LoadingState rows={4} />;
  }
  if (query.error) {
    return (
      <ErrorState
        message={t("common.error")}
        retryLabel={t("common.retry")}
        onRetry={() => void query.refetch()}
      />
    );
  }
  if ((query.data ?? []).length === 0) {
    return (
      <EmptyState
        icon={FileTextIcon}
        title={t("dns.records.empty.title")}
        description={t("dns.records.empty.body")}
        action={
          canCreate ? (
            <Button onClick={() => setCreateOpen(true)}>{t("dns.records.new")}</Button>
          ) : null
        }
      />
    );
  }

  const toggleRow = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const allSelected = filtered.length > 0 && selected.size === filtered.length;
  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(filtered.map((r) => r.id)));
  };

  return (
    <div className="space-y-3">
      {selected.size > 0 && (
        <div className="flex items-center justify-between rounded-md border border-border bg-muted/30 px-3 py-2 text-sm">
          <span>{t("dns.records.selected", { count: selected.size })}</span>
          {canDelete && (
            <Button
              size="sm"
              variant="destructive"
              onClick={() => {
                selected.forEach((id) => destroy.mutate({ recordId: id }));
                setSelected(new Set());
              }}
            >
              <Trash2Icon className="h-4 w-4" />
              {t("dns.records.bulkDelete")}
            </Button>
          )}
        </div>
      )}

      <div className="flex items-center justify-end">
        {canCreate && (
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            {t("dns.records.new")}
          </Button>
        )}
      </div>

      <div className="rounded-md border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[40px]">
                <Checkbox
                  checked={allSelected}
                  onCheckedChange={toggleAll}
                  aria-label={t("common.selectAll")}
                />
              </TableHead>
              <TableHead>{t("dns.records.columns.name")}</TableHead>
              <TableHead>{t("dns.records.columns.type")}</TableHead>
              <TableHead>{t("dns.records.columns.content")}</TableHead>
              <TableHead>{t("dns.records.columns.ttl")}</TableHead>
              <TableHead>{t("dns.records.columns.disabled")}</TableHead>
              <TableHead className="text-end">{t("dns.records.columns.actions")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((rec) => (
              <TableRow key={rec.id}>
                <TableCell>
                  <Checkbox
                    checked={selected.has(rec.id)}
                    onCheckedChange={() => toggleRow(rec.id)}
                    aria-label={`select-${rec.id}`}
                  />
                </TableCell>
                <TableCell className="font-mono text-xs">{rec.name}</TableCell>
                <TableCell>
                  <Badge variant="outline">{rec.type}</Badge>
                </TableCell>
                <TableCell className="font-mono text-xs break-all">{rec.content}</TableCell>
                <TableCell className="text-xs">{rec.ttl}</TableCell>
                <TableCell>
                  {rec.disabled ? (
                    <Badge variant="secondary">{t("common.yes")}</Badge>
                  ) : (
                    <Badge variant="outline">{t("common.no")}</Badge>
                  )}
                </TableCell>
                <TableCell className="text-end">
                  <div className="flex items-center justify-end gap-1">
                    {canUpdate && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => setEditing(rec)}
                      >
                        {t("common.save")}
                      </Button>
                    )}
                    {canDelete && (
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={t("common.delete")}
                        onClick={() => destroy.mutate({ recordId: rec.id })}
                      >
                        <Trash2Icon className="h-4 w-4" />
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <CreateDNSRecordDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        tenantId={tenantId}
        zoneId={zoneId}
      />
      <EditDNSRecordDialog
        record={editing}
        onClose={() => setEditing(null)}
        tenantId={tenantId}
        zoneId={zoneId}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Inline dialogs
// ---------------------------------------------------------------------------

interface CreateProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tenantId: string | null;
  zoneId: string | undefined;
}

function CreateDNSRecordDialog({ open, onOpenChange, tenantId, zoneId }: CreateProps) {
  const { t } = useTranslation();
  const create = useCreateDNSRecord(tenantId, zoneId);

  const form = useForm<CreateRecordValues>({
    resolver: zodResolver(createRecordSchema),
    defaultValues: {
      name: "",
      type: "A",
      content: "",
      ttl: 3600,
      disabled: false,
    },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await create.mutateAsync(values);
    onOpenChange(false);
    form.reset({ name: "", type: "A", content: "", ttl: 3600, disabled: false });
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("dns.records.create.title")}</DialogTitle>
          <DialogDescription>{t("dns.records.empty.body")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="dns-rec-name">{t("dns.records.create.name.label")}</Label>
            <Input id="dns-rec-name" {...form.register("name")} />
            {form.formState.errors.name && (
              <p className="text-xs text-destructive">{form.formState.errors.name.message}</p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="dns-rec-type">{t("dns.records.create.type.label")}</Label>
            <Select
              value={form.watch("type")}
              onValueChange={(v) => form.setValue("type", v as DNSRecordType)}
            >
              <SelectTrigger id="dns-rec-type">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RECORD_TYPES.map((rt) => (
                  <SelectItem key={rt} value={rt}>
                    {rt}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="dns-rec-content">{t("dns.records.create.content.label")}</Label>
            <Input
              id="dns-rec-content"
              placeholder={t("dns.records.create.content.placeholder")}
              {...form.register("content")}
            />
            {form.formState.errors.content && (
              <p className="text-xs text-destructive">{form.formState.errors.content.message}</p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="dns-rec-ttl">{t("dns.records.create.ttl.label")}</Label>
            <Input id="dns-rec-ttl" type="number" min={300} max={86400} {...form.register("ttl", { valueAsNumber: true })} />
            {form.formState.errors.ttl && (
              <p className="text-xs text-destructive">{form.formState.errors.ttl.message}</p>
            )}
          </div>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={form.watch("disabled")}
              onCheckedChange={(v) => form.setValue("disabled", v === true)}
            />
            {t("dns.records.create.disabled.label")}
          </label>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? t("dns.records.create.submitting") : t("dns.records.create.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

interface EditProps {
  record: DNSRecord | null;
  onClose: () => void;
  tenantId: string | null;
  zoneId: string | undefined;
}

function EditDNSRecordDialog({ record, onClose, tenantId, zoneId }: EditProps) {
  const { t } = useTranslation();
  const update = useUpdateDNSRecord(tenantId, zoneId);

  const form = useForm<UpdateRecordValues>({
    resolver: zodResolver(updateRecordSchema),
    defaultValues: { content: "", ttl: 3600, disabled: false },
    mode: "onChange",
  });

  React.useEffect(() => {
    if (record) {
      form.reset({
        content: record.content,
        ttl: record.ttl,
        disabled: record.disabled ?? false,
      });
    }
  }, [record, form]);

  const onSubmit = form.handleSubmit(async (values) => {
    if (!record) return;
    await update.mutateAsync({ recordId: record.id, values });
    onClose();
  });

  return (
    <Dialog open={!!record} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("dns.records.edit.title")}</DialogTitle>
          <DialogDescription>
            {record?.name} · {record?.type}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="dns-rec-edit-content">{t("dns.records.edit.content.label")}</Label>
            <Input id="dns-rec-edit-content" {...form.register("content")} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="dns-rec-edit-ttl">{t("dns.records.edit.ttl.label")}</Label>
            <Input
              id="dns-rec-edit-ttl"
              type="number"
              min={300}
              max={86400}
              {...form.register("ttl", { valueAsNumber: true })}
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={form.watch("disabled")}
              onCheckedChange={(v) => form.setValue("disabled", v === true)}
            />
            {t("dns.records.edit.disabled.label")}
          </label>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={update.isPending}>
              {update.isPending ? t("dns.records.edit.submitting") : t("dns.records.edit.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
