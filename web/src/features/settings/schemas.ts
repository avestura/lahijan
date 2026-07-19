/**
 * Settings-area zod schemas.
 *
 * Each form's schema derives from the OpenAPI request shape so the
 * client and server stay in sync.
 */
import { z } from "zod";

/** Update profile (PATCH /api/v1/auth/me). */
export const updateProfileSchema = z
  .object({
    displayName: z.string().optional(),
    locale: z.enum(["en", "fa"]).optional(),
    currentPassword: z.string().optional(),
    newPassword: z.string().min(8).optional(),
    newEmail: z.string().email().optional(),
  })
  .refine((v) => !v.newPassword || (v.newPassword && v.currentPassword), {
    message: "Current password is required when changing password",
    path: ["currentPassword"],
  });

export type UpdateProfileValues = z.infer<typeof updateProfileSchema>;

/** Disable TOTP (re-auth required). */
export const disableTotpSchema = z.object({
  currentPassword: z.string().min(1),
});
export type DisableTotpValues = z.infer<typeof disableTotpSchema>;

/** Verify a TOTP 6-digit code at enrollment. */
export const verifyTotpSchema = z.object({
  code: z
    .string()
    .length(6)
    .regex(/^\d{6}$/, "Code must be 6 digits"),
});
export type VerifyTotpValues = z.infer<typeof verifyTotpSchema>;

/** Create a personal access token. */
export const createTokenSchema = z.object({
  name: z.string().min(1).max(64),
  scopes: z.array(z.string()).optional(),
  expiresAt: z.string().optional(),
});
export type CreateTokenValues = z.infer<typeof createTokenSchema>;
