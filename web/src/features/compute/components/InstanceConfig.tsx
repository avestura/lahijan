/**
 * InstanceConfig — the Config tab on the instance detail page.
 *
 * Mirrors the compute backend's configuration model:
 *   - Effective configuration: every key after profiles are applied, with
 *     its origin (set on the instance, inherited from a profile, or
 *     daemon-managed "system" keys such as volatile.* / image.*).
 *   - Instance configuration (editable with compute.instance.update): the
 *     keys set on the instance itself. Saving PATCHes the instance's own
 *     config; system keys are carried over untouched.
 *   - Instance devices (editable): devices attached directly to the
 *     instance (profile devices are listed in Network/Storage). Add any
 *     device type the backend supports (disk, nic, proxy, gpu, ...).
 *
 * Project restrictions still apply server-side: keys or devices the tenant
 * may not use are rejected and the reason is shown in a toast.
 */
import { useEffect, useMemo, useState } from "react";
import type { components } from "@api-schema";
import { useFieldArray, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslation } from "react-i18next";
import { PlusIcon, Trash2Icon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { usePerm } from "@/lib/perm";
import { useUpdateInstance } from "../api";
import { configOrigin } from "../format";
import { RuntimeSection } from "./RuntimeSection";

type Runtime = components["schemas"]["ComputeInstanceRuntime"];

interface Props {
  instanceId: string;
  tenantId: string | null;
  runtime: Runtime;
}

/** Keys the daemon owns; shown read-only and always sent back unchanged. */
function isSystemKey(key: string): boolean {
  return key.startsWith("volatile.") || key.startsWith("image.");
}

const KEY_PATTERN = /^[a-z0-9][a-z0-9._-]*$/i;

const configSchema = z.object({
  rows: z.array(
    z.object({
      key: z.string().trim().regex(KEY_PATTERN),
      value: z.string(),
    }),
  ),
});
type ConfigForm = z.infer<typeof configSchema>;

const DEVICE_TYPES = [
  "disk",
  "nic",
  "proxy",
  "unix-char",
  "unix-block",
  "usb",
  "gpu",
  "tpm",
  "pci",
  "infiniband",
] as const;

const deviceSchema = z.object({
  name: z.string().trim().regex(KEY_PATTERN),
  type: z.enum(DEVICE_TYPES),
  // One key=value per line.
  properties: z.string(),
});
type DeviceForm = z.infer<typeof deviceSchema>;

/** parseProperties turns "a=b\nc=d" into a map; returns null on a bad line. */
function parseProperties(text: string): Record<string, string> | null {
  const out: Record<string, string> = {};
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (line === "") continue;
    const eq = line.indexOf("=");
    if (eq <= 0) return null;
    out[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
  }
  return out;
}

export function InstanceConfig({ instanceId, tenantId, runtime }: Props) {
  const { t } = useTranslation();
  const { hasPerm: canEdit } = usePerm("compute.instance.update");
  const update = useUpdateInstance(tenantId, instanceId);
  const [filter, setFilter] = useState("");

  const effective = useMemo(
    () =>
      Object.entries(runtime.expandedConfig)
        .filter(([k, v]) => {
          const f = filter.trim().toLowerCase();
          return f === "" || k.toLowerCase().includes(f) || v.toLowerCase().includes(f);
        })
        .sort(([a], [b]) => a.localeCompare(b)),
    [runtime.expandedConfig, filter],
  );

  // --- Instance config editor ------------------------------------------
  const ownRows = useMemo(
    () =>
      Object.entries(runtime.config)
        .filter(([k]) => !isSystemKey(k))
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([key, value]) => ({ key, value })),
    [runtime.config],
  );
  const form = useForm<ConfigForm>({
    resolver: zodResolver(configSchema),
    defaultValues: { rows: ownRows },
  });
  const rows = useFieldArray({ control: form.control, name: "rows" });
  // Re-sync after a save or a poll when the user has not started editing.
  useEffect(() => {
    if (!form.formState.isDirty) form.reset({ rows: ownRows });
  }, [ownRows, form]);

  const saveConfig = form.handleSubmit(async (values) => {
    const next: Record<string, string> = {};
    for (const [k, v] of Object.entries(runtime.config)) {
      if (isSystemKey(k)) next[k] = v;
    }
    for (const r of values.rows) next[r.key.trim()] = r.value;
    await update.mutateAsync({ config: next });
    form.reset({ rows: values.rows });
  });

  // --- Instance devices editor -----------------------------------------
  const ownDevices = Object.entries(runtime.devices).sort(([a], [b]) => a.localeCompare(b));
  const deviceForm = useForm<DeviceForm>({
    resolver: zodResolver(deviceSchema),
    defaultValues: { name: "", type: "disk", properties: "" },
  });
  const [propsError, setPropsError] = useState(false);
  const addDevice = deviceForm.handleSubmit(async (values) => {
    const props = parseProperties(values.properties);
    if (!props) {
      setPropsError(true);
      return;
    }
    setPropsError(false);
    await update.mutateAsync({
      devices: { ...runtime.devices, [values.name]: { ...props, type: values.type } },
    });
    deviceForm.reset({ name: "", type: values.type, properties: "" });
  });
  const removeDevice = async (name: string) => {
    const next = { ...runtime.devices };
    delete next[name];
    await update.mutateAsync({ devices: next });
  };

  return (
    <div className="space-y-6">
      <RuntimeSection
        title={t("compute.config.effective")}
        description={t("compute.config.effectiveHint")}
        actions={
          <Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder={t("compute.config.filter")}
            aria-label={t("compute.config.filter")}
            className="h-8 w-56"
          />
        }
      >
        <Table data-testid="instance-effective-config">
          <TableHeader>
            <TableRow>
              <TableHead>{t("compute.config.columns.key")}</TableHead>
              <TableHead>{t("compute.config.columns.value")}</TableHead>
              <TableHead>{t("compute.devices.columns.source")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {effective.length === 0 ? (
              <TableRow>
                <TableCell colSpan={3} className="text-sm text-muted-foreground">
                  {t("common.none")}
                </TableCell>
              </TableRow>
            ) : (
              effective.map(([k, v]) => (
                <TableRow key={k}>
                  <TableCell className="font-mono text-xs">{k}</TableCell>
                  <TableCell className="max-w-md break-all font-mono text-xs">{v}</TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {t(`compute.origin.${configOrigin(runtime, k)}`)}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </RuntimeSection>

      <RuntimeSection title={t("compute.config.own")} description={t("compute.config.ownHint")}>
        <form onSubmit={saveConfig} className="space-y-3 p-4" data-testid="instance-config-form">
          {rows.fields.length === 0 && (
            <p className="text-sm text-muted-foreground">{t("compute.config.noOwnKeys")}</p>
          )}
          {rows.fields.map((field, index) => (
            <div key={field.id} className="flex items-start gap-2">
              <Input
                {...form.register(`rows.${index}.key`)}
                placeholder="limits.cpu"
                aria-label={t("compute.config.columns.key")}
                className="font-mono text-xs"
                disabled={!canEdit}
              />
              <Input
                {...form.register(`rows.${index}.value`)}
                placeholder="2"
                aria-label={t("compute.config.columns.value")}
                className="font-mono text-xs"
                disabled={!canEdit}
              />
              {canEdit && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={() => rows.remove(index)}
                  aria-label={t("common.delete")}
                >
                  <Trash2Icon className="h-4 w-4" />
                </Button>
              )}
            </div>
          ))}
          {form.formState.errors.rows && (
            <p role="alert" className="text-xs text-destructive">
              {t("compute.config.invalidKey")}
            </p>
          )}
          {canEdit && (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => rows.append({ key: "", value: "" })}
              >
                <PlusIcon className="h-4 w-4" />
                {t("compute.config.addKey")}
              </Button>
              <Button
                type="submit"
                size="sm"
                disabled={update.isPending || !form.formState.isDirty}
              >
                {t("common.save")}
              </Button>
              {form.formState.isDirty && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => form.reset({ rows: ownRows })}
                >
                  {t("common.reset")}
                </Button>
              )}
            </div>
          )}
        </form>
      </RuntimeSection>

      <RuntimeSection
        title={t("compute.config.devices")}
        description={t("compute.config.devicesHint")}
      >
        <Table data-testid="instance-own-devices">
          <TableHeader>
            <TableRow>
              <TableHead>{t("compute.devices.columns.name")}</TableHead>
              <TableHead>{t("compute.devices.columns.type")}</TableHead>
              <TableHead>{t("compute.devices.columns.settings")}</TableHead>
              {canEdit && <TableHead className="w-12" />}
            </TableRow>
          </TableHeader>
          <TableBody>
            {ownDevices.length === 0 ? (
              <TableRow>
                <TableCell colSpan={canEdit ? 4 : 3} className="text-sm text-muted-foreground">
                  {t("compute.config.noOwnDevices")}
                </TableCell>
              </TableRow>
            ) : (
              ownDevices.map(([name, props]) => (
                <TableRow key={name}>
                  <TableCell className="font-mono text-xs">{name}</TableCell>
                  <TableCell className="font-mono text-xs">{props.type ?? "—"}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {Object.entries(props)
                      .filter(([k]) => k !== "type")
                      .map(([k, v]) => (
                        <div key={k}>{`${k}=${v}`}</div>
                      ))}
                  </TableCell>
                  {canEdit && (
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={update.isPending}
                        onClick={() => void removeDevice(name)}
                        aria-label={t("compute.config.removeDevice", { name })}
                      >
                        <Trash2Icon className="h-4 w-4" />
                      </Button>
                    </TableCell>
                  )}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        {canEdit && (
          <form
            onSubmit={addDevice}
            className="grid gap-3 border-t border-border p-4 sm:grid-cols-[1fr_12rem]"
            data-testid="instance-add-device-form"
          >
            <div className="space-y-2">
              <Label htmlFor="device-name">{t("compute.devices.columns.name")}</Label>
              <Input
                id="device-name"
                {...deviceForm.register("name")}
                className="font-mono text-xs"
              />
            </div>
            <div className="space-y-2">
              <Label>{t("compute.devices.columns.type")}</Label>
              <Select
                value={deviceForm.watch("type")}
                onValueChange={(v) => deviceForm.setValue("type", v as DeviceForm["type"])}
              >
                <SelectTrigger aria-label={t("compute.devices.columns.type")}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {DEVICE_TYPES.map((dt) => (
                    <SelectItem key={dt} value={dt}>
                      {dt}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="device-props">{t("compute.config.properties")}</Label>
              <Textarea
                id="device-props"
                rows={3}
                {...deviceForm.register("properties")}
                placeholder={t(`compute.config.propertiesExample.${deviceForm.watch("type")}`, {
                  defaultValue: "key=value",
                })}
                className="font-mono text-xs"
              />
              <p className="text-xs text-muted-foreground">{t("compute.config.propertiesHint")}</p>
              {(propsError || deviceForm.formState.errors.name) && (
                <p role="alert" className="text-xs text-destructive">
                  {t("compute.config.invalidDevice")}
                </p>
              )}
            </div>
            <div className="sm:col-span-2">
              <Button type="submit" size="sm" disabled={update.isPending}>
                <PlusIcon className="h-4 w-4" />
                {t("compute.config.addDevice")}
              </Button>
            </div>
          </form>
        )}
      </RuntimeSection>
    </div>
  );
}
