/**
 * Object storage (S3) zod schemas, derived from the OpenAPI-generated
 * types so the form and the API request body can never drift.
 */
import { z } from "zod";

/**
 * CREDENTIAL_ACTIONS mirrors StorageCredential.actions enum on the
 * StorageCredentialCreateRequest shape.
 */
export const CREDENTIAL_ACTIONS = ["Read", "Write", "List", "Tagging", "Admin"] as const;

export type CredentialAction = (typeof CREDENTIAL_ACTIONS)[number];

/**
 * createBucketSchema drives the "New bucket" form. The slug pattern
 * mirrors the OpenAPI `pattern` regex (DNS-safe, 1-26 chars).
 */
export const createBucketSchema = z.object({
  slug: z
    .string()
    .min(1)
    .max(26)
    .regex(
      /^[a-z0-9][a-z0-9-]{0,24}[a-z0-9]$|^[a-z0-9]$/,
      "Slug must be 1–26 lowercase letters, digits, or dashes.",
    ),
  label: z.string().max(100).optional(),
  description: z.string().optional(),
  quotaBytes: z.coerce.number().int().min(0).default(0),
  quotaObjects: z.coerce.number().int().min(0).default(0),
});

export type CreateBucketValues = z.infer<typeof createBucketSchema>;

/**
 * updateBucketSchema mirrors StorageBucketUpdateRequest.
 */
export const updateBucketSchema = z.object({
  label: z.string().max(100).optional(),
  description: z.string().optional(),
});

export type UpdateBucketValues = z.infer<typeof updateBucketSchema>;

/**
 * createCredentialSchema mirrors StorageCredentialCreateRequest.
 * At least one action is required.
 */
export const createCredentialSchema = z.object({
  label: z.string().max(100).optional(),
  actions: z.array(z.enum(CREDENTIAL_ACTIONS)).min(1),
  expiresInSeconds: z.coerce.number().int().min(1).optional(),
});

export type CreateCredentialValues = z.infer<typeof createCredentialSchema>;

/**
 * presignSchema mirrors StoragePresignRequest.
 */
export const presignSchema = z.object({
  method: z.enum(["GET", "PUT"]),
  key: z.string().default(""),
  expiresInSeconds: z.coerce.number().int().min(1).max(86400).default(3600),
});

export type PresignValues = z.infer<typeof presignSchema>;

/**
 * quotaSchema mirrors StorageQuotaRequest.
 */
export const quotaSchema = z.object({
  quotaBytes: z.coerce.number().int().min(0),
  quotaObjects: z.coerce.number().int().min(0),
});

export type QuotaValues = z.infer<typeof quotaSchema>;
