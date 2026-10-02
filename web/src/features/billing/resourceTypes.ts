/**
 * BILLING_RESOURCE_TYPES — the metering resource keys a price can be set
 * for, with the unit each one is conventionally billed in.
 *
 * The keys match the `prices.resource_type` documentation in migration
 * 0035. The unit stays free text in the API; `unit` here is only the
 * suggestion shown as the placeholder in the "Set price" dialog.
 */
export interface BillingResourceType {
  value: string;
  unit: string;
}

export const BILLING_RESOURCE_TYPES: readonly BillingResourceType[] = [
  { value: "compute.cpu", unit: "core-hours" },
  { value: "compute.ram", unit: "GiB-hours" },
  { value: "compute.disk", unit: "GiB-hours" },
  { value: "storage.size", unit: "GiB-hours" },
  { value: "storage.requests", unit: "1k-requests" },
  { value: "dns.queries", unit: "1M-queries" },
  { value: "network.egress", unit: "GiB" },
];

/** suggestedUnit returns the conventional unit for a resource type, or "". */
export function suggestedUnit(resourceType: string): string {
  return BILLING_RESOURCE_TYPES.find((r) => r.value === resourceType)?.unit ?? "";
}
