/**
 * /compute/images — image catalog manager.
 *
 * Lists the tenant's images (featured + custom) and lets users with the
 * `compute.image.create` permission record custom images (the bytes must
 * already be in the Incus image store, or the alias must be one Incus can
 * resolve on demand, e.g. an entry from images.linuxcontainers.org). The
 * "Browse remote images" button opens the linuxcontainers stream browser
 * and pre-fills the alias + fingerprint.
 */
import { createFileRoute } from "@tanstack/react-router";

import { ImageManager } from "@/features/compute/components/ImageManager";

export const Route = createFileRoute("/compute/images")({
  component: ImagesRoute,
});

function ImagesRoute() {
  return <ImageManager />;
}
