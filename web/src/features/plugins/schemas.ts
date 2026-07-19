/**
 * Plugins (WASM) zod schemas.
 *
 * Plugin management is platform-admin only; the schemas here drive
 * the upload + grant flows.
 */
import { z } from "zod";

/**
 * MANIFEST_SAMPLE — a placeholder manifest the user can paste into
 * the manifest textarea when they have no manifest file. Surfaced
 * as the textarea default in the upload dialog.
 */
export const MANIFEST_SAMPLE = `name: my-plugin
version: 0.1.0
description: An example Lahijan plugin.
permissions:
  - events.listen:compute.instance.*
  - network.outbound:hooks.example.com
`;

/**
 * pluginUploadSchema isn't a form schema — the upload endpoint is
 * multipart/form-data with two file parts (wasm + manifest). The
 * types are documented here for clarity; the component reads file
 * inputs directly.
 */
export const pluginUploadSchema = z.object({
  wasm: z.instanceof(File),
  manifest: z.string().min(1),
});

export type PluginUploadValues = z.infer<typeof pluginUploadSchema>;
