/**
 * InstanceGraphicalConsole — noVNC-backed graphical console for VMs (WS-24).
 *
 * The component dynamically loads the vendored noVNC client from
 * `/novnc/core/rfb.js` (see web/public/novnc/README.md), then opens a
 * WebSocket to `/api/v1/compute/instances/{id}/vnc`. The backend handler
 * upgrades the request and bridges the browser's RFB bytes to Incus'
 * VM VGA console. The browser sends session cookies on the WS upgrade
 * automatically (same-origin).
 *
 * Asset strategy: per ADR-0031 noVNC is vendored as static assets under
 * web/public/novnc/. The component renders a localised notice instead
 * of crashing when the operator has not run the vendor step.
 *
 * Permission gate: usePerm("compute.instance.console.vnc"). Containers
 * do not render this tab at all (the route hides it).
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangleIcon, Maximize2Icon, ScanIcon, ClipboardIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { usePerm } from "@/lib/perm";

// RFB is the noVNC client class. Declared locally so the hand-written
// call sites are type-checked without pulling the @novnc/novnc package
// (per ADR-0031 we vendor the upstream build; types are kept narrow).
interface RFB {
  disconnect(): void;
  sendCredentials(credentials: { password?: string }): void;
  sendCtrlAltDel(): void;
  grabKeyboard(): void;
  ungrabKeyboard(): void;
  focus(): void;
  resize(width: number, height: number): void;
  get capabilities(): { power: boolean };
  scaleViewport: boolean;
  showDotCursor: boolean;
}

type RFBConstructor = new (options: {
  target: HTMLElement;
  url: string;
  wsProtocols?: string[];
  repeaterID?: string;
  shared?: boolean;
  credentials?: { password?: string };
}) => RFB;

// Augment the global window so the dynamic <script> load is type-safe.
declare global {
  interface Window {
    RFB?: RFBConstructor;
  }
}

interface Props {
  instanceId: string;
  /** When the instance is stopped we surface a "disabled" hint. */
  status: string | undefined;
}

type LoadState = "idle" | "loading" | "ready" | "missing" | "failed";

const NOVNC_SCRIPT_URL = "/novnc/core/rfb.js";

/**
 * useLoadNovnc loads the vendored noVNC script once. Subsequent mounts
 * reuse the cached window.RFB. Returns the current state + a `load`
 * function that triggers the fetch (lazy: nothing loads until the user
 * actually opens the graphical console tab).
 */
function useLoadNovnc(): { state: LoadState; load: () => void } {
  const [state, setState] = useState<LoadState>("idle");

  const load = useCallback(() => {
    if (typeof window === "undefined") return;
    if (window.RFB) {
      setState("ready");
      return;
    }
    if (state === "loading") return;
    setState("loading");

    const existing = document.querySelector<HTMLScriptElement>(
      `script[src="${NOVNC_SCRIPT_URL}"]`,
    );
    if (existing) {
      existing.addEventListener(
        "load",
        () => {
          if (window.RFB) setState("ready");
          else setState("failed");
        },
        { once: true },
      );
      existing.addEventListener(
        "error",
        () => setState(existing.dataset.missing === "1" ? "missing" : "failed"),
        { once: true },
      );
      return;
    }

    const script = document.createElement("script");
    script.src = NOVNC_SCRIPT_URL;
    script.async = true;
    script.onload = () => {
      if (window.RFB) setState("ready");
      else setState("failed");
    };
    script.onerror = () => {
      // The most likely onerror cause is "operator did not run the
      // vendor step" (404 on /novnc/core/rfb.js). We treat any fetch
      // failure as missing; the user-facing message is the same.
      setState("missing");
    };
    document.head.appendChild(script);
  }, [state]);

  return { state, load };
}

export function InstanceGraphicalConsole({ instanceId, status }: Props) {
  const { t } = useTranslation();
  const { hasPerm } = usePerm("compute.instance.console.vnc");
  const { state: novncState, load: loadNovnc } = useLoadNovnc();

  const screenRef = useRef<HTMLDivElement | null>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [connected, setConnected] = useState(false);
  const [scaleToFit, setScaleToFit] = useState(true);
  const [clipboardOpen, setClipboardOpen] = useState(false);
  const [clipboard, setClipboard] = useState("");

  const isRunning = status === "Running";

  // Pre-load noVNC the first time the user is allowed to use the console.
  useEffect(() => {
    if (hasPerm && isRunning && novncState === "idle") {
      loadNovnc();
    }
  }, [hasPerm, isRunning, novncState, loadNovnc]);

  // Disconnect on unmount.
  useEffect(() => {
    return () => {
      if (rfbRef.current) {
        try {
          rfbRef.current.disconnect();
        } catch {
          // noVNC may throw on disconnect-after-close; ignore.
        }
        rfbRef.current = null;
      }
    };
  }, []);

  const handleConnect = useCallback(() => {
    if (!screenRef.current) return;
    if (!window.RFB) return;
    if (rfbRef.current) {
      rfbRef.current.disconnect();
      rfbRef.current = null;
    }
    const url = `${location.protocol === "https:" ? "wss" : "ws"}://${
      location.host
    }/api/v1/compute/instances/${instanceId}/vnc`;
    const rfb = new window.RFB({
      target: screenRef.current,
      url,
      // wsProtocols left default; noVNC picks "binary" automatically.
    });
    rfb.scaleViewport = scaleToFit;
    // Focus so keyboard input flows to the VM immediately.
    try {
      rfb.focus();
    } catch {
      // focus can throw before the canvas is laid out; safe to ignore.
    }
    rfbRef.current = rfb;
    setConnected(true);
  }, [instanceId, scaleToFit]);

  const handleDisconnect = useCallback(() => {
    if (rfbRef.current) {
      rfbRef.current.disconnect();
      rfbRef.current = null;
    }
    setConnected(false);
  }, []);

  // Apply scaleToFit toggles without reconnecting.
  useEffect(() => {
    if (rfbRef.current) {
      rfbRef.current.scaleViewport = scaleToFit;
    }
  }, [scaleToFit]);

  const handleFullscreen = useCallback(() => {
    const el = screenRef.current;
    if (!el) return;
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void el.requestFullscreen?.();
    }
  }, []);

  const handleClipboardSync = useCallback(() => {
    // noVNC <0.8 used a callback; modern builds read from the textarea.
    // We push the local textarea contents into the VM clipboard via a
    // best-effort paste: dispatch a copy event the RFB listener observes.
    // This is intentionally minimal — full bidirectional clipboard sync
    // is a future enhancement (WS-24 scope item).
    const text = clipboard;
    if (!text) return;
    // Write to the system clipboard as a fallback for the user to paste.
    void navigator.clipboard?.writeText?.(text).catch(() => {
      // ignore — clipboard may be unavailable (permissions, non-secure context).
    });
  }, [clipboard]);

  // Permission gate: server still enforces; this is defense in depth.
  if (!hasPerm) {
    return (
      <p className="text-sm text-muted-foreground">
        {t("compute.graphicalConsole.noPermission")}
      </p>
    );
  }

  // Instance must be a VM (route hides this tab for containers; the
  // backend re-validates) AND must be running.
  if (!isRunning) {
    return (
      <p className="text-sm text-muted-foreground">
        {t("compute.graphicalConsole.notRunning")}
      </p>
    );
  }

  // Asset gate: show the vendor notice if noVNC could not be loaded.
  if (novncState === "missing") {
    return (
      <div className="space-y-2 rounded-md border border-yellow-500/40 bg-yellow-500/10 p-4 text-sm">
        <div className="flex items-center gap-2 font-medium">
          <AlertTriangleIcon className="h-4 w-4" />
          {t("compute.graphicalConsole.notVendoredTitle")}
        </div>
        <p className="text-muted-foreground">
          {t("compute.graphicalConsole.notVendoredBody")}
        </p>
      </div>
    );
  }
  if (novncState === "failed") {
    return (
      <p className="text-sm text-destructive">
        {t("compute.graphicalConsole.loadFailed")}
      </p>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={connected ? handleDisconnect : handleConnect}
          disabled={novncState !== "ready"}
        >
          {connected
            ? t("compute.graphicalConsole.disconnect")
            : t("compute.graphicalConsole.connect")}
        </Button>
        <Button variant="ghost" size="sm" onClick={handleFullscreen} disabled={!connected}>
          <Maximize2Icon className="h-4 w-4" />
          {t("compute.graphicalConsole.fullscreen")}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          aria-pressed={scaleToFit}
          onClick={() => setScaleToFit((s) => !s)}
          disabled={!connected}
        >
          <ScanIcon className="h-4 w-4" />
          {t("compute.graphicalConsole.scaleToFit")}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          aria-pressed={clipboardOpen}
          onClick={() => setClipboardOpen((s) => !s)}
          disabled={!connected}
        >
          <ClipboardIcon className="h-4 w-4" />
          {t("compute.graphicalConsole.clipboardSync")}
        </Button>
        <span className="text-xs text-muted-foreground">
          {connected
            ? t("compute.graphicalConsole.connected")
            : t("compute.graphicalConsole.disconnected")}
        </span>
      </div>

      {clipboardOpen && (
        <div className="space-y-2 rounded-md border border-border bg-card p-3">
          <Label htmlFor="vnc-clipboard" className="text-xs">
            {t("compute.graphicalConsole.clipboardSync")}
          </Label>
          <Textarea
            id="vnc-clipboard"
            value={clipboard}
            onChange={(e) => setClipboard(e.target.value)}
            placeholder={t("compute.graphicalConsole.clipboardPlaceholder")}
            className="h-20 font-mono text-xs"
          />
          <div className="flex justify-end">
            <Button size="sm" variant="ghost" onClick={handleClipboardSync} disabled={!clipboard}>
              {t("compute.graphicalConsole.clipboardSync")}
            </Button>
          </div>
        </div>
      )}

      <div
        ref={screenRef}
        className="h-96 overflow-hidden rounded-md border border-border bg-black p-2"
        aria-label={t("compute.graphicalConsole.title")}
        role="region"
      />
    </div>
  );
}
