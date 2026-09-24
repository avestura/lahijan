/**
 * Plugins (WASM) zod schemas.
 *
 * Plugin management is platform-admin only; the schemas here drive
 * the upload + grant flows.
 */
import { z } from "zod";

/**
 * EXTENSION_PACKAGE_SUFFIX — file extension of a Lahijan extension
 * package: a ZIP with lahijan.manifest.yaml + plugin.wasm at its root
 * (backend: internal/app/lahijan/wasm/lahx).
 */
export const EXTENSION_PACKAGE_SUFFIX = ".lahx";

/**
 * pluginUploadSchema isn't a form schema — the upload endpoint is
 * multipart/form-data with one `package` file part (the .lahx). The
 * type is documented here for clarity; the dialog reads the file input
 * directly.
 */
export const pluginUploadSchema = z.object({
  pkg: z.instanceof(File).refine((f) => f.name.toLowerCase().endsWith(EXTENSION_PACKAGE_SUFFIX)),
});

export type PluginUploadValues = z.infer<typeof pluginUploadSchema>;
