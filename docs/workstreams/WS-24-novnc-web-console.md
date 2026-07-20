# WS-24 · noVNC Web Console for VMs

```
Status: in-progress
Phase: 7
Depends on: WS-11 (Incus provider), WS-20 (dashboard)
Unblocks: —
```

> Originally **deferred** past MVP. Implementing now per explicit request so
> the scope, design, and ADR trail are not lost. WS-14 ships xterm.js for
> container exec; this WS adds noVNC for full graphical VM console access.

## Goal

Add a browser-based VNC console for VM instances, using `noVNC` (HTML5 VNC
client) proxied to Incus' built-in VNC server. Users get a full graphical
desktop/serial console without leaving the dashboard.

## Scope (when work begins)

- WebSocket proxy in Lahijan that bridges browser ↔ Incus VNC port for a given
  VM instance
- `noVNC` static assets served from the dashboard (`web/public/novnc/`)
- New "Console (Graphical)" tab on the instance detail page (alongside the
  existing "Console (Terminal)" tab from WS-14)
- Permission: `compute.instance.console.vnc` (distinct from `.exec`)
- Clipboard sync, fullscreen, scale-to-fit controls
- Audit event: `compute.instance.console.vnc.connect`

## Required reading (when work begins)

- `/AGENTS.md`
- WS-11 doc (Incus provider — `dev-incus` API for VM consoles)
- WS-14 doc (compute module — exec console pattern to mirror)
- WS-20 doc (dashboard conventions)
- [noVNC docs](https://novnc.com/info.html)
- [Incus VM console access](https://linuxcontainers.org/incus/docs/main/howto/instances_console/)

## Notes

- Incus supports `dev-incus` API for VM graphical console; check current
  Incus version at the time this WS starts.
- noVNC adds a static asset bundle (~1MB) to the dashboard; bundle size
  budget review needed when work begins.
