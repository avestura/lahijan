/**
 * AGENT_TOOLS — the tools the agent can call, as exposed by the backend
 * module tool bridge (internal/app/lahijan/agent/module_tools.go).
 *
 * `value` is the exact tool name the policy denylist matches against.
 * `i18nKey` indexes `agent.policy.tools.*` (dots are not usable in
 * i18next keys). Keep this list in sync with the backend catalogue.
 */
export interface AgentToolInfo {
  value: string;
  i18nKey: string;
}

export const AGENT_TOOLS: readonly AgentToolInfo[] = [
  { value: "compute.list_instances", i18nKey: "compute_list_instances" },
  { value: "dns.list_zones", i18nKey: "dns_list_zones" },
  { value: "storage.list_buckets", i18nKey: "storage_list_buckets" },
  { value: "billing.list_usage", i18nKey: "billing_list_usage" },
  { value: "audit.list_events", i18nKey: "audit_list_events" },
];
