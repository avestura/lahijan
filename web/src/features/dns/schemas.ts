/**
 * DNS zod schemas, derived from the OpenAPI-generated types so the
 * form and the API request body can never drift (per the frontend
 * skill's Forms section).
 */
import { z } from "zod";

/**
 * RECORD_TYPES mirrors the OpenAPI enum on DNSRecordCreateRequest.type
 * (the same set is reused for the update + filter dropdowns).
 */
export const RECORD_TYPES = [
  "A",
  "AAAA",
  "CAA",
  "CNAME",
  "DS",
  "MX",
  "NS",
  "PTR",
  "SOA",
  "SRV",
  "TLSA",
  "TXT",
] as const;

export type DNSRecordType = (typeof RECORD_TYPES)[number];

/**
 * createZoneSchema drives the "New zone" form. The shape mirrors
 * components["schemas"]["DNSZoneCreateRequest"] with an additional
 * templateId (used to apply a predefined record set right after
 * the zone is created).
 */
export const createZoneSchema = z.object({
  name: z.string().min(1).regex(/\.$/, "Zone name must end with a trailing dot (canonical form)."),
  description: z.string().optional(),
  kind: z.enum(["Native", "Master", "Slave"]).default("Native"),
  templateId: z.string().optional(),
});

export type CreateZoneValues = z.infer<typeof createZoneSchema>;

/**
 * updateZoneSchema mirrors DNSZoneUpdateRequest.
 */
export const updateZoneSchema = z.object({
  description: z.string().optional(),
  kind: z.enum(["Native", "Master", "Slave"]).optional(),
});

export type UpdateZoneValues = z.infer<typeof updateZoneSchema>;

/**
 * createRecordSchema mirrors DNSRecordCreateRequest. TTL is clamped
 * to [300, 86400] on the server; we mirror the bound client-side so
 * the form surfaces a clear message before the round-trip.
 */
export const createRecordSchema = z.object({
  name: z.string().min(1),
  type: z.enum(RECORD_TYPES),
  content: z.string().min(1),
  ttl: z.coerce.number().int().min(300).max(86400).default(3600),
  disabled: z.boolean().default(false),
});

export type CreateRecordValues = z.infer<typeof createRecordSchema>;

/**
 * updateRecordSchema mirrors DNSRecordUpdateRequest (name + type are
 * immutable — the form disables them).
 */
export const updateRecordSchema = z.object({
  content: z.string().min(1),
  ttl: z.coerce.number().int().min(300).max(86400),
  disabled: z.boolean(),
});

export type UpdateRecordValues = z.infer<typeof updateRecordSchema>;
