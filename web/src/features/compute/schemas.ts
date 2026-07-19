/**
 * Compute zod schemas, derived from the OpenAPI-generated types so the
 * form and the API request body can never drift (per the frontend
 * skill's Forms section).
 */
import { z } from "zod";

/**
 * createInstanceSchema drives the 3-step Create Instance wizard.
 *
 * The shape mirrors components["schemas"]["ComputeInstanceCreateRequest"]
 * with two additions:
 *   - cpu / memory / disk are first-class fields (the API would otherwise
 *     expect them as `config["limits.cpu"]` etc.); we collect them here
 *     and translate to the config map inside the mutation.
 *   - `profile` is a single string; the API takes a `profiles` array. We
 *     surface the common case (one profile) as a single select.
 */
export const createInstanceSchema = z.object({
  name: z
    .string()
    .min(1)
    .max(63)
    .regex(
      /^[a-z0-9][a-z0-9-]*$/,
      "Name must start lowercase alphanumeric and only contain [a-z0-9-].",
    ),
  description: z.string().optional(),
  type: z.enum(["container", "virtual-machine"]).default("container"),
  imageAlias: z.string().min(1),
  profile: z.string().default("default"),
  cpu: z.coerce.number().int().min(1).max(64).default(1),
  memoryMiB: z.coerce
    .number()
    .int()
    .min(64)
    .max(1024 * 64)
    .default(512),
  diskGiB: z.coerce.number().int().min(1).max(1024).default(10),
  // Optional free-form config overrides; the wizard's Advanced panel
  // surfaces this as a textarea the user fills with YAML/JSON.
  configYAML: z.string().optional(),
});

export type CreateInstanceValues = z.infer<typeof createInstanceSchema>;

/**
 * execCommandSchema is the small form that powers the per-instance
 * Console tab. The user types a command, hits Run, and the result is
 * rendered in the xterm.js terminal.
 */
export const execCommandSchema = z.object({
  command: z.string().min(1),
});

export type ExecCommandValues = z.infer<typeof execCommandSchema>;
