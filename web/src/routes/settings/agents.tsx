/**
 * /settings/agents — BYOK model-provider configuration for the agent (WS-31).
 *
 * The user adds their own API key + model here; the key is encrypted at rest
 * (AES-256-GCM) on the backend and never returned. The agent resolves the
 * first enabled config at turn time and drives the LLM with it. Without a
 * configured provider the agent replies with a "configure a provider" notice.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  CheckCircle2Icon,
  KeyRoundIcon,
  PlugZapIcon,
  PlusIcon,
  Trash2Icon,
  XCircleIcon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useTenant } from "@/hooks/useTenant";
import { usePerm } from "@/lib/perm";
import {
  useAgentProviders,
  useDeleteAgentProvider,
  useSaveAgentProvider,
  useTestAgentProvider,
  type AgentProviderTestResult,
} from "@/features/agent/api";

export const Route = createFileRoute("/settings/agents")({
  component: AgentSettingsPage,
});

/**
 * Providers we know how to default a base URL for.
 *
 * MUST stay in sync with `defaultProviderBase` in
 * `internal/app/lahijan/agent/llm_harness.go` — every slug here has a matching
 * case there. Only OpenAI-compatible providers are listed, because the agent
 * harness speaks only the OpenAI /chat/completions protocol; a provider whose
 * API surface differs (Anthropic, Bedrock, Vertex AI, Azure OpenAI, ...) would
 * fail at runtime and is intentionally omitted.
 */
const PROVIDER_OPTIONS = [
  // OpenAI + OpenAI-compatible aggregators.
  "openai",
  "openrouter",
  "302ai",
  // Hosted inference platforms.
  "groq",
  "together",
  "deepseek",
  "cerebras",
  "deepinfra",
  "fireworks",
  "moonshot",
  "minimax",
  "nvidia",
  "venice",
  "xai",
  "zai",
  "zai-coding-plan",
  // Local / self-hosted OpenAI-compatible servers.
  "ollama",
  "lmstudio",
  "llamacpp",
] as const;

function AgentSettingsPage() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const canManage = usePerm("agent.provider.manage");

  const providers = useAgentProviders(tenantId);
  const save = useSaveAgentProvider(tenantId);
  const del = useDeleteAgentProvider(tenantId);
  const test = useTestAgentProvider();
  // Latest test outcome for the add form ("draft") and for each saved provider.
  const [results, setResults] = useState<Record<string, AgentProviderTestResult | "error">>({});
  const [testingKey, setTestingKey] = useState<string | null>(null);

  function runTest(key: string, input: Parameters<typeof test.mutate>[0]) {
    setTestingKey(key);
    setResults((r) => {
      const next = { ...r };
      delete next[key];
      return next;
    });
    test.mutate(input, {
      onSuccess: (res) => setResults((r) => ({ ...r, [key]: res })),
      onError: () => setResults((r) => ({ ...r, [key]: "error" })),
      onSettled: () => setTestingKey(null),
    });
  }

  const [provider, setProvider] = useState<string>("openai");
  const [model, setModel] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");

  function reset() {
    setProvider("openai");
    setModel("");
    setBaseUrl("");
    setApiKey("");
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!apiKey || !provider || save.isPending) return;
    save.mutate(
      { provider, apiKey, model: model || undefined, baseUrl: baseUrl || undefined },
      { onSuccess: reset },
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("agent.settings.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("agent.settings.subtitle")}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <KeyRoundIcon className="h-5 w-5" />
            {t("agent.settings.addTitle")}
          </CardTitle>
          <CardDescription>{t("agent.settings.addDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid grid-cols-1 gap-4 sm:grid-cols-2" onSubmit={onSubmit}>
            <div className="space-y-2">
              <Label htmlFor="agent-provider">{t("agent.settings.provider")}</Label>
              <Select value={provider} onValueChange={setProvider} disabled={!canManage.hasPerm}>
                <SelectTrigger id="agent-provider">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PROVIDER_OPTIONS.map((p) => (
                    <SelectItem key={p} value={p}>
                      {p}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="agent-model">{t("agent.settings.model")}</Label>
              <Input
                id="agent-model"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder={t("agent.settings.modelPlaceholder")}
                disabled={!canManage.hasPerm}
              />
            </div>
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="agent-baseurl">{t("agent.settings.baseUrl")}</Label>
              <Input
                id="agent-baseurl"
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
                placeholder={t("agent.settings.baseUrlPlaceholder")}
                disabled={!canManage.hasPerm}
              />
            </div>
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="agent-apikey">{t("agent.settings.apiKey")}</Label>
              <Input
                id="agent-apikey"
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={t("agent.settings.apiKeyPlaceholder")}
                autoComplete="off"
                disabled={!canManage.hasPerm}
              />
            </div>
            <div className="sm:col-span-2">
              <div className="flex flex-wrap items-center gap-3">
                <Button type="submit" disabled={!canManage.hasPerm || save.isPending || !apiKey}>
                  <PlusIcon className="h-4 w-4" />
                  {t("agent.settings.save")}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={!canManage.hasPerm || !apiKey || testingKey === "draft"}
                  onClick={() =>
                    runTest("draft", {
                      provider,
                      apiKey,
                      model: model || undefined,
                      baseUrl: baseUrl || undefined,
                    })
                  }
                >
                  <PlugZapIcon className="h-4 w-4" />
                  {testingKey === "draft"
                    ? t("agent.settings.test.testing")
                    : t("agent.settings.test.button")}
                </Button>
                <TestOutcome result={results.draft} />
              </div>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("agent.settings.configuredTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          {providers.isLoading && (
            <p className="text-sm text-muted-foreground">{t("common.loading")}</p>
          )}
          {providers.data && providers.data.length === 0 && (
            <p className="text-sm text-muted-foreground">{t("agent.settings.noneConfigured")}</p>
          )}
          <ul className="divide-y divide-border">
            {providers.data?.map((p) => (
              <li key={p.id} className="flex items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium">
                    {p.provider}
                    {p.model ? <span className="text-muted-foreground"> · {p.model}</span> : null}
                  </div>
                  {p.baseUrl && (
                    <div className="truncate text-xs text-muted-foreground">{p.baseUrl}</div>
                  )}
                </div>
                <div className="flex items-center gap-3">
                  {p.hasKey && (
                    <span className="text-xs text-muted-foreground">
                      {t("agent.settings.keySet")}
                    </span>
                  )}
                  <TestOutcome result={results[p.id]} />
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={!canManage.hasPerm || testingKey === p.id}
                    onClick={() =>
                      runTest(p.id, {
                        provider: p.provider,
                        model: p.model || undefined,
                        baseUrl: p.baseUrl || undefined,
                        providerId: p.id,
                      })
                    }
                  >
                    <PlugZapIcon className="h-4 w-4" />
                    {testingKey === p.id
                      ? t("agent.settings.test.testing")
                      : t("agent.settings.test.button")}
                  </Button>
                  <button
                    type="button"
                    aria-label={t("agent.settings.delete")}
                    disabled={!canManage.hasPerm || del.isPending}
                    onClick={() => del.mutate(p.id)}
                  >
                    <Trash2Icon className="h-4 w-4 text-muted-foreground hover:text-destructive" />
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}

/** TestOutcome renders the result of a provider connection test, if any. */
function TestOutcome({ result }: { result: AgentProviderTestResult | "error" | undefined }) {
  const { t } = useTranslation();
  if (!result) return null;
  if (result === "error") {
    return (
      <span role="status" className="flex items-center gap-1 text-xs text-destructive">
        <XCircleIcon className="h-4 w-4" />
        {t("agent.settings.test.requestFailed")}
      </span>
    );
  }
  const ok = result.ok;
  return (
    <span
      role="status"
      className={`flex items-center gap-1 text-xs ${ok ? "text-success" : "text-destructive"}`}
    >
      {ok ? <CheckCircle2Icon className="h-4 w-4" /> : <XCircleIcon className="h-4 w-4" />}
      {t(`agent.settings.test.status.${result.status}`)}
      {ok ? ` · ${t("agent.settings.test.latency", { ms: result.latencyMs })}` : null}
    </span>
  );
}
