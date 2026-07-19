/**
 * CreateInstanceWizard — 3-step "New instance" form.
 *
 * Steps:
 *   1. Image — pick the source image (from the tenant's catalog).
 *   2. Size  — pick vCPUs / RAM / disk + name + type.
 *   3. Review — confirm and submit.
 *
 * Advanced options (raw config YAML, custom profile) live in a
 * collapsible panel on step 2, per the WS-20 doc's open question
 * default ("3-step (image → size → review) for the simple path;
 * advanced mode in a collapsible panel").
 *
 * Drives `useCreateInstance`; on success we navigate to the new
 * instance's detail page.
 */
import { useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { ChevronDownIcon, ChevronUpIcon } from "lucide-react";

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
import { Card, CardContent } from "@/components/ui/card";
import { useTenant } from "@/hooks/useTenant";
import {
  useComputeImages,
  useComputeProfiles,
  useCreateInstance,
} from "../api";
import { createInstanceSchema, type CreateInstanceValues } from "../schemas";

type Step = "image" | "size" | "review";

const STEP_ORDER: Step[] = ["image", "size", "review"];

// Separator between step indicator pills. The character is RTL-safe
// (it renders correctly in both `ltr` and `rtl` flow). Computed at
// module load so the i18n lint rule doesn't trip on a JSX literal.
const SEP = "›";

// The instance type values mirror the OpenAPI enum exactly. The
// dropdown label is fetched from `compute.types.<value>` or
// `compute.create.type.<short>`.
const INSTANCE_TYPES = ["container", "virtual-machine"] as const;

export function CreateInstanceWizard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const images = useComputeImages(tenantId);
  const profiles = useComputeProfiles(tenantId);
  const create = useCreateInstance(tenantId);

  const [step, setStep] = useState<Step>("image");
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const form = useForm<CreateInstanceValues>({
    resolver: zodResolver(createInstanceSchema),
    defaultValues: {
      name: "",
      description: "",
      type: "container",
      imageAlias: "",
      profile: "default",
      cpu: 1,
      memoryMiB: 512,
      diskGiB: 10,
      configYAML: "",
    },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    const inst = await create.mutateAsync(values);
    await navigate({ to: "/compute/$id", params: { id: inst.id } });
  });

  const next = async () => {
    // Validate the current step's slice before advancing.
    const fields: Record<Step, (keyof CreateInstanceValues)[]> = {
      image: ["imageAlias"],
      size: ["name", "type", "cpu", "memoryMiB", "diskGiB"],
      review: [],
    };
    const ok = await form.trigger(fields[step]);
    if (ok) {
      const idx = STEP_ORDER.indexOf(step);
      if (idx < STEP_ORDER.length - 1) setStep(STEP_ORDER[idx + 1] ?? "review");
    }
  };

  const prev = () => {
    const idx = STEP_ORDER.indexOf(step);
    if (idx > 0) setStep(STEP_ORDER[idx - 1] ?? "image");
  };

  const stepIndex = STEP_ORDER.indexOf(step);

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold">{t("compute.create.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("compute.create.subtitle")}</p>
      </header>

      {/* Step indicator */}
      <ol className="flex items-center gap-2 text-sm">
        {STEP_ORDER.map((s, i) => (
          <li
            key={s}
            className={[
              "flex items-center gap-2",
              i < stepIndex ? "text-muted-foreground" : "",
              i === stepIndex ? "font-semibold" : "",
            ].join(" ")}
          >
            <span
              className={[
                "flex h-6 w-6 items-center justify-center rounded-full border",
                i < stepIndex ? "border-success bg-success/10 text-success" : "",
                i === stepIndex ? "border-primary text-primary" : "",
                i > stepIndex ? "border-border text-muted-foreground" : "",
              ].join(" ")}
            >
              {i + 1}
            </span>
            {t(`compute.create.steps.${s}`)}
            {i < STEP_ORDER.length - 1 ? (
              <span className="mx-2 text-muted-foreground" aria-hidden="true">
                {SEP}
              </span>
            ) : null}
          </li>
        ))}
      </ol>

      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        {step === "image" && (
          <Card>
            <CardContent className="space-y-4 p-6">
              <div className="space-y-2">
                <Label htmlFor="imageAlias">{t("compute.create.image.label")}</Label>
                {images.isLoading ? (
                  <p className="text-sm text-muted-foreground">{t("common.loading")}</p>
                ) : images.error ? (
                  <p className="text-sm text-destructive">{t("common.error")}</p>
                ) : (images.data ?? []).length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    {t("compute.create.image.empty")}
                  </p>
                ) : (
                  <Select
                    value={form.watch("imageAlias")}
                    onValueChange={(v) => form.setValue("imageAlias", v, { shouldValidate: true })}
                  >
                    <SelectTrigger id="imageAlias">
                      <SelectValue placeholder={t("compute.create.image.label")} />
                    </SelectTrigger>
                    <SelectContent>
                      {(images.data ?? []).map((img) => (
                        <SelectItem key={img.id} value={img.alias}>
                          {img.alias}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
                {form.formState.errors.imageAlias && (
                  <p className="text-xs text-destructive">
                    {form.formState.errors.imageAlias.message}
                  </p>
                )}
              </div>
            </CardContent>
          </Card>
        )}

        {step === "size" && (
          <Card>
            <CardContent className="space-y-4 p-6">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="name">{t("compute.create.name.label")}</Label>
                  <Input
                    id="name"
                    placeholder={t("compute.create.name.placeholder")}
                    {...form.register("name")}
                  />
                  <p className="text-xs text-muted-foreground">
                    {t("compute.create.name.hint")}
                  </p>
                  {form.formState.errors.name && (
                    <p className="text-xs text-destructive">
                      {form.formState.errors.name.message}
                    </p>
                  )}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="type">{t("compute.create.type.label")}</Label>
                  <Select
                    value={form.watch("type")}
                    onValueChange={(v) =>
                      form.setValue("type", v as "container" | "virtual-machine", {
                        shouldValidate: true,
                      })
                    }
                  >
                    <SelectTrigger id="type">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {INSTANCE_TYPES.map((v) => (
                        <SelectItem key={v} value={v}>
                          {v === "container"
                            ? t("compute.create.type.container")
                            : t("compute.create.type.vm")}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="cpu">{t("compute.create.cpu.label")}</Label>
                  <Input
                    id="cpu"
                    type="number"
                    min={1}
                    max={64}
                    {...form.register("cpu", { valueAsNumber: true })}
                  />
                  <p className="text-xs text-muted-foreground">{t("compute.create.cpu.hint")}</p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="memory">{t("compute.create.memory.label")}</Label>
                  <Input
                    id="memory"
                    type="number"
                    min={64}
                    {...form.register("memoryMiB", { valueAsNumber: true })}
                  />
                  <p className="text-xs text-muted-foreground">
                    {t("compute.create.memory.hint")}
                  </p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="disk">{t("compute.create.disk.label")}</Label>
                  <Input
                    id="disk"
                    type="number"
                    min={1}
                    {...form.register("diskGiB", { valueAsNumber: true })}
                  />
                  <p className="text-xs text-muted-foreground">{t("compute.create.disk.hint")}</p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="description">{t("compute.create.description.label")}</Label>
                  <Input
                    id="description"
                    placeholder={t("compute.create.description.placeholder")}
                    {...form.register("description")}
                  />
                </div>
              </div>

              <div className="rounded-md border border-border">
                <button
                  type="button"
                  className="flex w-full items-center justify-between px-4 py-2 text-sm font-medium"
                  onClick={() => setAdvancedOpen((o) => !o)}
                >
                  {t("compute.create.advanced.label")}
                  {advancedOpen ? (
                    <ChevronUpIcon className="h-4 w-4" />
                  ) : (
                    <ChevronDownIcon className="h-4 w-4" />
                  )}
                </button>
                {advancedOpen && (
                  <div className="space-y-4 border-t border-border p-4">
                    <div className="space-y-2">
                      <Label htmlFor="profile">{t("compute.create.profile.label")}</Label>
                      {(profiles.data ?? []).length === 0 ? (
                        <p className="text-sm text-muted-foreground">
                          {t("compute.create.profile.empty")}
                        </p>
                      ) : (
                        <Select
                          value={form.watch("profile")}
                          onValueChange={(v) => form.setValue("profile", v)}
                        >
                          <SelectTrigger id="profile">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {(profiles.data ?? []).map((p) => (
                              <SelectItem key={p.id} value={p.name}>
                                {p.name}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      )}
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="config">{t("compute.create.advanced.config")}</Label>
                      <Textarea id="config" rows={4} {...form.register("configYAML")} />
                    </div>
              </div>
                )}
              </div>
            </CardContent>
          </Card>
        )}

        {step === "review" && (
          <Card>
            <CardContent className="space-y-3 p-6">
              <Review label={t("compute.create.name.label")}>
                {form.getValues("name")}
              </Review>
              <Review label={t("compute.create.image.label")}>
                {form.getValues("imageAlias")}
              </Review>
              <Review label={t("compute.create.type.label")}>
                {t(`compute.types.${form.getValues("type")}`)}
              </Review>
              <Review label={t("compute.create.cpu.label")}>
                {String(form.getValues("cpu"))}
              </Review>
              <Review label={t("compute.create.memory.label")}>
                {`${form.getValues("memoryMiB")} MiB`}
              </Review>
              <Review label={t("compute.create.disk.label")}>
                {`${form.getValues("diskGiB")} GiB`}
              </Review>
            </CardContent>
          </Card>
        )}

        <div className="flex items-center justify-between">
          <Button
            type="button"
            variant="ghost"
            onClick={prev}
            disabled={stepIndex === 0 || create.isPending}
          >
            {t("common.previous")}
          </Button>
          {step === "review" ? (
            <Button
              type="submit"
              disabled={create.isPending || Object.keys(form.formState.errors).length > 0}
            >
              {create.isPending ? t("compute.create.submitting") : t("compute.create.submit")}
            </Button>
          ) : (
            <Button type="button" onClick={next} disabled={create.isPending}>
              {t("common.next")}
            </Button>
          )}
        </div>
        {create.error && (
          <p role="alert" className="text-sm text-destructive">
            {t("common.error")}
          </p>
        )}
      </form>
    </div>
  );
}

function Review({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between border-b border-border pb-2 last:border-b-0 last:pb-0">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium">{children}</dd>
    </div>
  );
}
