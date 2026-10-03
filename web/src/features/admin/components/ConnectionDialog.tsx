/**
 * ConnectionDialog — create or edit an LDAP / SAML directory connection.
 *
 * The form is kind-specific. "Test connection" runs the saved check against
 * the draft (without saving); on an existing connection an empty bind
 * password keeps the stored one.
 */
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckCircle2Icon, PlugZapIcon, XCircleIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useToast } from "@/hooks/useToast";
import {
  useCreateDirectoryConnection,
  useTestDirectoryConnection,
  useUpdateDirectoryConnection,
  type DirectoryConnection,
  type DirectoryConnectionRequest,
  type DirectoryTestResult,
} from "../api";

type Kind = "ldap" | "saml";
const KINDS: readonly Kind[] = ["ldap", "saml"];

/**
 * Example values shown as input placeholders. They are protocol-level
 * identifiers (DNs, attribute names, URLs), identical in every language.
 */
const EXAMPLE = {
  name: "corp-ldap",
  url: "ldaps://ldap.example.com:636",
  bindDn: "cn=svc-lahijan,dc=example,dc=com",
  userBase: "ou=people,dc=example,dc=com",
  userFilter: "(objectClass=person)",
  emailAttr: "mail",
  nameAttr: "displayName",
  groupBase: "ou=groups,dc=example,dc=com",
  groupFilter: "(objectClass=groupOfNames)",
  groupName: "cn",
  groupMember: "member",
  metadataUrl: "https://idp.example.com/metadata",
  groupsAttr: "groups",
} as const;

/** The editable form state: every config field as a string / boolean. */
interface FormState {
  kind: Kind;
  name: string;
  enabled: boolean;
  bindPassword: string;
  // LDAP
  url: string;
  startTls: boolean;
  insecureSkipVerify: boolean;
  bindDn: string;
  userBaseDn: string;
  userFilter: string;
  emailAttr: string;
  nameAttr: string;
  groupBaseDn: string;
  groupFilter: string;
  groupNameAttr: string;
  groupMemberAttr: string;
  createUsersOnLogin: boolean;
  // SAML
  idpMetadataUrl: string;
  idpMetadataXml: string;
  entityId: string;
  emailAttribute: string;
  nameAttribute: string;
  groupsAttribute: string;
  allowIdpInitiated: boolean;
}

const EMPTY: FormState = {
  kind: "ldap",
  name: "",
  enabled: true,
  bindPassword: "",
  url: "",
  startTls: false,
  insecureSkipVerify: false,
  bindDn: "",
  userBaseDn: "",
  userFilter: "",
  emailAttr: "",
  nameAttr: "",
  groupBaseDn: "",
  groupFilter: "",
  groupNameAttr: "",
  groupMemberAttr: "",
  createUsersOnLogin: true,
  idpMetadataUrl: "",
  idpMetadataXml: "",
  entityId: "",
  emailAttribute: "",
  nameAttribute: "",
  groupsAttribute: "",
  allowIdpInitiated: false,
};

function fromConnection(c: DirectoryConnection): FormState {
  const cfg: Record<string, unknown> = c.config;
  const str = (k: string): string => {
    const v = cfg[k];
    return typeof v === "string" ? v : "";
  };
  const bool = (k: string) => cfg[k] === true;
  return {
    ...EMPTY,
    kind: c.kind,
    name: c.name,
    enabled: c.enabled,
    url: str("url"),
    startTls: bool("startTls"),
    insecureSkipVerify: bool("insecureSkipVerify"),
    bindDn: str("bindDn"),
    userBaseDn: str("userBaseDn"),
    userFilter: str("userFilter"),
    emailAttr: str("emailAttr"),
    nameAttr: str("nameAttr"),
    groupBaseDn: str("groupBaseDn"),
    groupFilter: str("groupFilter"),
    groupNameAttr: str("groupNameAttr"),
    groupMemberAttr: str("groupMemberAttr"),
    // Absent means on (the server default).
    createUsersOnLogin: cfg.createUsersOnLogin !== false,
    idpMetadataUrl: str("idpMetadataUrl"),
    idpMetadataXml: str("idpMetadataXml"),
    entityId: str("entityId"),
    emailAttribute: str("emailAttribute"),
    nameAttribute: str("nameAttribute"),
    groupsAttribute: str("groupsAttribute"),
    allowIdpInitiated: bool("allowIdpInitiated"),
  };
}

function toRequest(f: FormState): DirectoryConnectionRequest {
  const config: Record<string, unknown> =
    f.kind === "ldap"
      ? {
          url: f.url.trim(),
          startTls: f.startTls,
          insecureSkipVerify: f.insecureSkipVerify,
          bindDn: f.bindDn.trim(),
          userBaseDn: f.userBaseDn.trim(),
          userFilter: f.userFilter.trim(),
          emailAttr: f.emailAttr.trim(),
          nameAttr: f.nameAttr.trim(),
          groupBaseDn: f.groupBaseDn.trim(),
          groupFilter: f.groupFilter.trim(),
          groupNameAttr: f.groupNameAttr.trim(),
          groupMemberAttr: f.groupMemberAttr.trim(),
          createUsersOnLogin: f.createUsersOnLogin,
        }
      : {
          idpMetadataUrl: f.idpMetadataUrl.trim(),
          idpMetadataXml: f.idpMetadataXml.trim(),
          entityId: f.entityId.trim(),
          emailAttribute: f.emailAttribute.trim(),
          nameAttribute: f.nameAttribute.trim(),
          groupsAttribute: f.groupsAttribute.trim(),
          allowIdpInitiated: f.allowIdpInitiated,
        };
  const req: DirectoryConnectionRequest = {
    kind: f.kind,
    name: f.name.trim(),
    enabled: f.enabled,
    config,
  };
  // Only send a password when the admin typed one; omitting it keeps the stored value.
  if (f.kind === "ldap" && f.bindPassword) req.bindPassword = f.bindPassword;
  return req;
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Connection to edit; null creates a new one. */
  connection: DirectoryConnection | null;
}

export function ConnectionDialog({ open, onOpenChange, connection }: Props) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const create = useCreateDirectoryConnection();
  const update = useUpdateDirectoryConnection();
  const test = useTestDirectoryConnection();
  const [form, setForm] = useState<FormState>(EMPTY);
  const [result, setResult] = useState<DirectoryTestResult | "error" | null>(null);
  const [failed, setFailed] = useState(false);
  const editing = connection !== null;

  useEffect(() => {
    if (!open) return;
    setForm(connection ? fromConnection(connection) : EMPTY);
    setResult(null);
    setFailed(false);
  }, [open, connection]);

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (create.isPending || update.isPending) return;
    setFailed(false);
    const body = toRequest(form);
    const done = {
      onSuccess: (saved: DirectoryConnection) => {
        if (saved.activationError) {
          // Saved, but the SAML provider could not be switched on: say why.
          toast({
            title: t("admin.directory.savedInactive"),
            description: saved.activationError,
            variant: "destructive",
          });
        } else {
          toast({ title: t("admin.directory.saved"), variant: "success" });
        }
        onOpenChange(false);
      },
      onError: () => setFailed(true),
    };
    if (connection) update.mutate({ id: connection.id, body }, done);
    else create.mutate(body, done);
  }

  function onTest() {
    setResult(null);
    test.mutate(
      { connection: toRequest(form), connectionId: connection?.id },
      { onSuccess: setResult, onError: () => setResult("error") },
    );
  }

  const isLdap = form.kind === "ldap";
  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const acsUrl = `${origin}/api/v1/auth/saml/${form.name || "<name>"}/acs`;
  const metadataUrl = `${origin}/api/v1/auth/saml/metadata`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {editing ? t("admin.directory.edit.title") : t("admin.directory.create.title")}
          </DialogTitle>
          <DialogDescription>{t("admin.directory.dialogDescription")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="dc-kind">{t("admin.directory.fields.kind")}</Label>
              <Select
                value={form.kind}
                onValueChange={(v) => set("kind", v as Kind)}
                disabled={editing}
              >
                <SelectTrigger id="dc-kind">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KINDS.map((k) => (
                    <SelectItem key={k} value={k}>
                      {t(`admin.directory.kind.${k}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="dc-name">{t("admin.directory.fields.name")}</Label>
              <Input
                id="dc-name"
                value={form.name}
                onChange={(e) => set("name", e.target.value)}
                placeholder={EXAMPLE.name}
                pattern="[a-z0-9][a-z0-9_\-]{0,62}"
                required
              />
              <p className="text-xs text-muted-foreground">
                {t("admin.directory.fields.nameHint")}
              </p>
            </div>
          </div>

          {isLdap ? (
            <fieldset className="space-y-3">
              <legend className="mb-1 text-sm font-medium">
                {t("admin.directory.sections.server")}
              </legend>
              <Field
                id="dc-url"
                label={t("admin.directory.fields.url")}
                hint={t("admin.directory.fields.urlHint")}
              >
                <Input
                  id="dc-url"
                  value={form.url}
                  onChange={(e) => set("url", e.target.value)}
                  placeholder={EXAMPLE.url}
                  required
                />
              </Field>
              <div className="flex flex-wrap gap-x-6 gap-y-2">
                <Check
                  id="dc-starttls"
                  label={t("admin.directory.fields.startTls")}
                  checked={form.startTls}
                  onChange={(v) => set("startTls", v)}
                />
                <Check
                  id="dc-insecure"
                  label={t("admin.directory.fields.insecureSkipVerify")}
                  checked={form.insecureSkipVerify}
                  onChange={(v) => set("insecureSkipVerify", v)}
                />
              </div>
              {form.insecureSkipVerify && (
                <p role="note" className="text-xs text-destructive">
                  {t("admin.directory.fields.insecureWarning")}
                </p>
              )}
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field id="dc-binddn" label={t("admin.directory.fields.bindDn")}>
                  <Input
                    id="dc-binddn"
                    value={form.bindDn}
                    onChange={(e) => set("bindDn", e.target.value)}
                    placeholder={EXAMPLE.bindDn}
                  />
                </Field>
                <Field
                  id="dc-bindpw"
                  label={t("admin.directory.fields.bindPassword")}
                  hint={
                    editing && connection?.hasSecret
                      ? t("admin.directory.fields.bindPasswordKeep")
                      : undefined
                  }
                >
                  <Input
                    id="dc-bindpw"
                    type="password"
                    autoComplete="new-password"
                    value={form.bindPassword}
                    onChange={(e) => set("bindPassword", e.target.value)}
                  />
                </Field>
              </div>

              <h3 className="pt-2 text-sm font-medium">{t("admin.directory.sections.users")}</h3>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field id="dc-ubase" label={t("admin.directory.fields.userBaseDn")}>
                  <Input
                    id="dc-ubase"
                    value={form.userBaseDn}
                    onChange={(e) => set("userBaseDn", e.target.value)}
                    placeholder={EXAMPLE.userBase}
                    required
                  />
                </Field>
                <Field id="dc-ufilter" label={t("admin.directory.fields.userFilter")}>
                  <Input
                    id="dc-ufilter"
                    value={form.userFilter}
                    onChange={(e) => set("userFilter", e.target.value)}
                    placeholder={EXAMPLE.userFilter}
                  />
                </Field>
                <Field id="dc-email" label={t("admin.directory.fields.emailAttr")}>
                  <Input
                    id="dc-email"
                    value={form.emailAttr}
                    onChange={(e) => set("emailAttr", e.target.value)}
                    placeholder={EXAMPLE.emailAttr}
                  />
                </Field>
                <Field id="dc-uname" label={t("admin.directory.fields.nameAttr")}>
                  <Input
                    id="dc-uname"
                    value={form.nameAttr}
                    onChange={(e) => set("nameAttr", e.target.value)}
                    placeholder={EXAMPLE.nameAttr}
                  />
                </Field>
              </div>

              <h3 className="pt-2 text-sm font-medium">{t("admin.directory.sections.groups")}</h3>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field
                  id="dc-gbase"
                  label={t("admin.directory.fields.groupBaseDn")}
                  hint={t("admin.directory.fields.groupBaseDnHint")}
                >
                  <Input
                    id="dc-gbase"
                    value={form.groupBaseDn}
                    onChange={(e) => set("groupBaseDn", e.target.value)}
                    placeholder={EXAMPLE.groupBase}
                  />
                </Field>
                <Field id="dc-gfilter" label={t("admin.directory.fields.groupFilter")}>
                  <Input
                    id="dc-gfilter"
                    value={form.groupFilter}
                    onChange={(e) => set("groupFilter", e.target.value)}
                    placeholder={EXAMPLE.groupFilter}
                  />
                </Field>
                <Field id="dc-gname" label={t("admin.directory.fields.groupNameAttr")}>
                  <Input
                    id="dc-gname"
                    value={form.groupNameAttr}
                    onChange={(e) => set("groupNameAttr", e.target.value)}
                    placeholder={EXAMPLE.groupName}
                  />
                </Field>
                <Field id="dc-gmember" label={t("admin.directory.fields.groupMemberAttr")}>
                  <Input
                    id="dc-gmember"
                    value={form.groupMemberAttr}
                    onChange={(e) => set("groupMemberAttr", e.target.value)}
                    placeholder={EXAMPLE.groupMember}
                  />
                </Field>
              </div>

              <h3 className="pt-2 text-sm font-medium">{t("admin.directory.sections.signIn")}</h3>
              <Check
                id="dc-createonlogin"
                label={t("admin.directory.fields.createUsersOnLogin")}
                checked={form.createUsersOnLogin}
                onChange={(v) => set("createUsersOnLogin", v)}
              />
              <p className="text-xs text-muted-foreground">
                {t("admin.directory.fields.createUsersOnLoginHint")}
              </p>
            </fieldset>
          ) : (
            <fieldset className="space-y-3">
              <legend className="mb-1 text-sm font-medium">
                {t("admin.directory.sections.idp")}
              </legend>
              <Field id="dc-metaurl" label={t("admin.directory.fields.idpMetadataUrl")}>
                <Input
                  id="dc-metaurl"
                  value={form.idpMetadataUrl}
                  onChange={(e) => set("idpMetadataUrl", e.target.value)}
                  placeholder={EXAMPLE.metadataUrl}
                />
              </Field>
              <Field
                id="dc-metaxml"
                label={t("admin.directory.fields.idpMetadataXml")}
                hint={t("admin.directory.fields.idpMetadataXmlHint")}
              >
                <Textarea
                  id="dc-metaxml"
                  rows={4}
                  className="font-mono text-xs"
                  value={form.idpMetadataXml}
                  onChange={(e) => set("idpMetadataXml", e.target.value)}
                />
              </Field>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field
                  id="dc-entity"
                  label={t("admin.directory.fields.entityId")}
                  hint={t("admin.directory.fields.entityIdHint")}
                >
                  <Input
                    id="dc-entity"
                    value={form.entityId}
                    onChange={(e) => set("entityId", e.target.value)}
                  />
                </Field>
                <Field id="dc-sem" label={t("admin.directory.fields.emailAttribute")}>
                  <Input
                    id="dc-sem"
                    value={form.emailAttribute}
                    onChange={(e) => set("emailAttribute", e.target.value)}
                  />
                </Field>
                <Field id="dc-snm" label={t("admin.directory.fields.nameAttribute")}>
                  <Input
                    id="dc-snm"
                    value={form.nameAttribute}
                    onChange={(e) => set("nameAttribute", e.target.value)}
                  />
                </Field>
                <Field id="dc-sgr" label={t("admin.directory.fields.groupsAttribute")}>
                  <Input
                    id="dc-sgr"
                    value={form.groupsAttribute}
                    onChange={(e) => set("groupsAttribute", e.target.value)}
                    placeholder={EXAMPLE.groupsAttr}
                  />
                </Field>
              </div>
              <Check
                id="dc-idpinit"
                label={t("admin.directory.fields.allowIdpInitiated")}
                checked={form.allowIdpInitiated}
                onChange={(v) => set("allowIdpInitiated", v)}
              />
              <div className="bg-muted/30 space-y-1 border border-border p-3 text-xs">
                <p className="font-medium">{t("admin.directory.spInfo.title")}</p>
                <p className="text-muted-foreground">{t("admin.directory.spInfo.body")}</p>
                <p className="font-mono" dir="ltr">
                  {t("admin.directory.spInfo.acs")}: {acsUrl}
                </p>
                <p className="font-mono" dir="ltr">
                  {t("admin.directory.spInfo.metadata")}: {metadataUrl}
                </p>
              </div>
            </fieldset>
          )}

          <Check
            id="dc-enabled"
            label={t("admin.directory.fields.enabled")}
            checked={form.enabled}
            onChange={(v) => set("enabled", v)}
          />

          <div className="flex flex-wrap items-center gap-3">
            <Button type="button" variant="outline" disabled={test.isPending} onClick={onTest}>
              <PlugZapIcon className="h-4 w-4" />
              {test.isPending
                ? t("admin.directory.test.testing")
                : t("admin.directory.test.button")}
            </Button>
            <TestOutcome result={result} />
          </div>

          {failed && (
            <p role="alert" className="text-sm text-destructive">
              {t("admin.directory.saveFailed")}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={create.isPending || update.isPending}>
              {t("common.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Field({
  id,
  label,
  hint,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

function Check({
  id,
  label,
  checked,
  onChange,
}: {
  id: string;
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Checkbox id={id} checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      <Label htmlFor={id} className="cursor-pointer">
        {label}
      </Label>
    </div>
  );
}

/** TestOutcome renders the result of a connection test. */
export function TestOutcome({ result }: { result: DirectoryTestResult | "error" | null }) {
  const { t } = useTranslation();
  if (!result) return null;
  if (result === "error") {
    return (
      <span role="status" className="flex items-center gap-1 text-xs text-destructive">
        <XCircleIcon className="h-4 w-4" />
        {t("admin.directory.test.requestFailed")}
      </span>
    );
  }
  return (
    <div role="status" className="space-y-1 text-xs">
      <span
        className={`flex items-center gap-1 ${result.ok ? "text-success" : "text-destructive"}`}
      >
        {result.ok ? <CheckCircle2Icon className="h-4 w-4" /> : <XCircleIcon className="h-4 w-4" />}
        {t(`admin.directory.test.code.${result.code}`)}
      </span>
      {result.ok && result.entityId === undefined && (
        <span className="block text-muted-foreground">
          {t("admin.directory.test.found", {
            users: result.users ?? 0,
            groups: result.groups ?? 0,
          })}
        </span>
      )}
      {result.ok && result.entityId !== undefined && (
        <span className="block font-mono text-muted-foreground" dir="ltr">
          {result.entityId}
        </span>
      )}
      {!result.ok && result.detail && (
        <span className="block break-all font-mono text-muted-foreground" dir="ltr">
          {result.detail}
        </span>
      )}
    </div>
  );
}
