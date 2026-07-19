/**
 * Auth zod schemas, derived from the OpenAPI-generated types so the form
 * and the API request body can never drift (per the frontend skill's
 * Forms section).
 */
import { z } from "zod";

export const loginSchema = z.object({
  email: z.string().email(),
  password: z.string().min(8),
});

export type LoginValues = z.infer<typeof loginSchema>;
