# noVNC assets (WS-24)

This directory hosts the vendored [noVNC](https://novnc.com/) client assets
served at `/novnc/*` by the dashboard's static-asset handler.

Per ADR-0031 these assets are **vendored as static files** rather than
imported via the `@novnc/novnc` npm package:

- **Bundle budget.** noVNC's pre-built `core/` is ~1 MB; importing it as a
  regular module would double the dashboard's gzipped first-paint budget.
  Serving it as a static asset means the cost is paid only when the user
  actually opens the "Console (Graphical)" tab on a VM instance.
- **Scope match.** The WS-24 doc lists `web/public/novnc/` as the intended
  asset location verbatim.
- **Type safety.** The component (`InstanceGraphicalConsole.tsx`) declares
  the global `RFB` shape via a local TypeScript declaration so the
  hand-written call sites are still type-checked.

## Vendor step (operator)

Re-run this step whenever noVNC publishes a security or feature release
worth picking up. The dashboard does NOT bundle these files at build
time; they are served verbatim.

1. Pick the upstream release to vendor:
   <https://github.com/novnc/noVNC/releases>. Pin a tag, do not track
   `master`.
2. Download the tarball and verify its sha256:

   ```bash
   curl -L -o /tmp/novnc.tar.gz \
     https://github.com/novnc/noVNC/archive/refs/tags/v<VERSION>.tar.gz
   sha256sum /tmp/novnc.tar.gz
   ```

3. Extract just the `core/` tree (everything else — docs, tests, vendor
   snapshots — is unnecessary at runtime):

   ```bash
   tar -xzf /tmp/novnc.tar.gz -C /tmp
   rm -rf web/public/novnc/core
   cp -R /tmp/noVNC-<VERSION>/core web/public/novnc/core
   ```

4. Record the version + sha256 below so reviewers can re-verify.
5. Commit `web/public/novnc/core/**` along with the version bump in
   this README.

## Currently vendored

| Field | Value |
|-------|-------|
| Upstream tag | _(not vendored yet — see step above)_ |
| Tarball sha256 | _(not vendored yet)_ |
| Vendor date | _(not vendored yet)_ |

When the field above is empty, the `InstanceGraphicalConsole` React
component renders a localised "noVNC assets not installed" notice
instead of crashing. Once an operator completes the vendor step, the
component picks up `/novnc/core/rfb.js` automatically on next page
load — no rebuild required for the dashboard itself.

## Layout

```
web/public/novnc/
├── README.md     # this file (committed; do not delete)
└── core/         # upstream noVNC core/ tree (created by the vendor step)
    ├── rfb.js
    ├── util/
    ├── input/
    ├── display/
    └── ...
```

Only `core/rfb.js` is loaded by the dashboard (via a dynamic
`<script>` tag from `InstanceGraphicalConsole.tsx`); the rest of
`core/` is its dependency graph and is fetched on demand by the
browser.

## License

noVNC is upstream MPL-2.0. The vendored files retain their upstream
license header; copying them into this repo does not change their
license. See <https://github.com/novnc/noVNC/blob/master/LICENSE.txt>
for the full text.
