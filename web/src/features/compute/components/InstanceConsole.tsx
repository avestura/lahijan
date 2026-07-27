/**
 * InstanceConsole — interactive xterm.js shell over WebSocket (WS-32).
 *
 * The terminal IS the input: every keystroke flows to the Incus exec
 * stdin fd via the Lahijan WS bridge at
 * /api/v1/compute/instances/{id}/console; Incus's combined stdout +
 * stderr flows back to the terminal. There is NO <Input> + <Button>
 * form — that one-shot scaffold (WS-20) was removed in WS-32 in favour
 * of a true bidirectional shell session. The previous one-shot POST
 * endpoint /exec stays for SDKs/automation.
 *
 * Responsive sizing: the container is observed with ResizeObserver
 * (NOT window.resize). The terminal's box changes when the sidebar
 * collapses, when devtools dock opens, when the tab is resized, etc.
 * window.resize only fires on the last of those, so the previous
 * listener was a real bug for split-pane layouts. Every observed size
 * change debounces 80ms, calls fitAddon.fit(), then sends a resize
 * control message {"type":"resize","cols":N,"rows":N} to the backend,
 * which forwards it to the Incus control fd so vim / htop / top
 * render at the right dimensions.
 *
 * Lifecycle: Connect/Disconnect button lets the user re-init cleanly.
 * On unmount the WS is closed (closing stdin triggers Incus to
 * terminate the exec) and the terminal is disposed.
 *
 * Permission gate: usePerm("compute.instance.console.exec"). The
 * server enforces too; this is defense in depth.
 */
import "@xterm/xterm/css/xterm.css";

import { useCallback, useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { usePerm } from "@/lib/perm";
import { classifyStatus } from "../api";

interface Props {
  instanceId: string;
  /** When the instance is stopped we surface a "disabled" hint. */
  status: string | undefined;
}

/** Connection state surfaced to the user via the Connect/Disconnect button. */
type ConnectionState = "disconnected" | "connecting" | "connected";

/** Debounce window for ResizeObserver-driven fits (ms). */
const RESIZE_DEBOUNCE_MS = 80;

/** Resize control envelope sent on the same WS as stdin. */
interface ResizeControlMessage {
  type: "resize";
  cols: number;
  rows: number;
}

/**
 * buildWebSocketURL constructs the ws/wss URL for the console bridge.
 * Same-origin session-cookie auth: the browser sends cookies on the
 * WS upgrade automatically.
 */
function buildWebSocketURL(instanceId: string): string {
  const scheme = location.protocol === "https:" ? "wss" : "ws";
  return `${scheme}://${location.host}/api/v1/compute/instances/${instanceId}/console`;
}

export function InstanceConsole({ instanceId, status }: Props) {
  const { t } = useTranslation();
  const { hasPerm } = usePerm("compute.instance.console.exec");

  const termRef = useRef<HTMLDivElement | null>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const ws = useRef<WebSocket | null>(null);
  /** resizeTimer holds the active debounce timeout id (or null). */
  const resizeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [connectionState, setConnectionState] = useState<ConnectionState>("disconnected");

  const isRunning = classifyStatus(status) === "running";

  /** sendResize writes a resize control message to the WS (no-op when closed). */
  const sendResize = useCallback((cols: number, rows: number) => {
    // The bridge ignores non-positive dims; skip the round-trip if
    // either is zero (initial layout pass can produce that).
    if (cols <= 0 || rows <= 0) return;
    const sock = ws.current;
    if (!sock || sock.readyState !== WebSocket.OPEN) return;
    const msg: ResizeControlMessage = { type: "resize", cols, rows };
    try {
      sock.send(JSON.stringify(msg));
    } catch {
      // WS send can throw if the socket closed between the readyState
      // check above and the send call (race). Swallow — the close
      // handler will surface the disconnect.
    }
  }, []);

  /** doFit re-measures the terminal's container and applies the new
   *  cell counts to the terminal. Returns the (cols, rows) it landed
   *  on so the caller can forward a resize control message. */
  const doFit = useCallback((): { cols: number; rows: number } => {
    const addon = fit.current;
    const termInst = term.current;
    if (!addon || !termInst) return { cols: 0, rows: 0 };
    try {
      addon.fit();
    } catch {
      // fit() can throw before the terminal's first layout pass; the
      // next ResizeObserver tick will retry. Safe to ignore.
      return { cols: termInst.cols, rows: termInst.rows };
    }
    return { cols: termInst.cols, rows: termInst.rows };
  }, []);

  /** connect opens the WebSocket + wires the bridge in both directions. */
  const connect = useCallback(() => {
    // Tear down any prior session first so a double-click on Connect
    // doesn't leak a dangling socket.
    if (ws.current) {
      try {
        ws.current.close();
      } catch {
        // ignore — already closed
      }
      ws.current = null;
    }
    const termInst = term.current;
    if (!termInst) return;

    setConnectionState("connecting");
    const sock = new WebSocket(buildWebSocketURL(instanceId));
    ws.current = sock;

    sock.onopen = () => {
      setConnectionState("connected");
      // Send the initial resize right after the WS opens so the remote
      // PTY starts at the right dimensions instead of the daemon's
      // default 80x25.
      const { cols, rows } = doFit();
      sendResize(cols, rows);
      termInst.focus();
    };

    // Incus stdout -> terminal. Bytes flow verbatim (ANSI escapes
    // included); xterm.js renders them. Binary frames are written as
    // Uint8Array so UTF-8 multibyte sequences stay intact.
    sock.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) {
        termInst.write(new Uint8Array(ev.data));
      } else if (typeof ev.data === "string") {
        termInst.write(ev.data);
      } else if (ev.data instanceof Blob) {
        // Blobs arrive when the server sends binary frames and the
        // browser hasn't set binaryType=arraybuffer. Convert to text
        // for the terminal; binary blob throughput on a shell is rare
        // (control sequences are text-shaped).
        ev.data
          .arrayBuffer()
          .then((buf) => termInst.write(new Uint8Array(buf)))
          .catch(() => {
            // Swallow read errors; the next onmessage will retry.
          });
      }
    };

    sock.onclose = () => {
      setConnectionState("disconnected");
      ws.current = null;
    };

    sock.onerror = () => {
      // The browser fires onerror before onclose when the WS upgrade
      // fails (e.g. 403 from the audit gate). The close handler does
      // the state transition; nothing to do here beyond a no-op so
      // React's exhaustive-deps lint is happy.
    };
  }, [instanceId, doFit, sendResize]);

  /** disconnect closes the WS cleanly. Closing stdin triggers Incus to
   *  terminate the exec per the documented "EOF on stdin ends the
   *  session" behaviour. */
  const disconnect = useCallback(() => {
    if (ws.current) {
      try {
        ws.current.close();
      } catch {
        // ignore — already closed
      }
      ws.current = null;
    }
    setConnectionState("disconnected");
  }, []);

  // Boot the terminal once on mount. The terminal persists across
  // connect/disconnect cycles; only the WS is torn down + rebuilt.
  useEffect(() => {
    if (!termRef.current) return;
    const termInst = new Terminal({
      fontFamily: "var(--font-mono), ui-monospace, monospace",
      fontSize: 13,
      // convertEol is off: the PTY already speaks \r\n. Turning it on
      // would double-translate and break cursor positioning for vim
      // and other screen-oriented programs.
      convertEol: false,
      cursorBlink: true,
      disableStdin: false,
      scrollback: 5000,
      // The terminal is conventionally dark regardless of the app
      // theme; the dashboard's light theme would wash out ANSI
      // colours if we let xterm.js inherit.
      theme: {
        background: "#000000",
        foreground: "#e6e6e6",
        cursor: "#e6e6e6",
      },
    });
    const fitAddon = new FitAddon();
    termInst.loadAddon(fitAddon);
    termInst.open(termRef.current);
    fitAddon.fit();
    term.current = termInst;
    fit.current = fitAddon;

    // term.onData fires on every keystroke the user types into the
    // terminal (including paste). Forward verbatim to the WS as a
    // text frame; the backend routes it to Incus' stdin fd.
    const dataDisposable = termInst.onData((data) => {
      const sock = ws.current;
      if (!sock || sock.readyState !== WebSocket.OPEN) return;
      try {
        sock.send(data);
      } catch {
        // Race with close; the close handler will surface the state.
      }
    });

    return () => {
      dataDisposable.dispose();
      termInst.dispose();
      term.current = null;
      fit.current = null;
    };
    // We intentionally only run this once on mount; the terminal
    // outlives the WS connection.
  }, []);

  // ResizeObserver on the terminal's container. This catches every
  // layout change (sidebar collapse, devtools dock, window resize,
  // tab resize) instead of just window.resize. Debounced 80ms to
  // avoid spamming fit() + resize control messages during a drag.
  useEffect(() => {
    const container = termRef.current;
    if (!container) return;
    if (typeof ResizeObserver === "undefined") {
      // Very old browsers (or jsdom) — fall back to no-op. Tests
      // mock ResizeObserver explicitly; this branch is for safety.
      return;
    }

    const observer = new ResizeObserver(() => {
      // Debounce: clear any pending fit + schedule a new one.
      if (resizeTimer.current !== null) {
        clearTimeout(resizeTimer.current);
      }
      resizeTimer.current = setTimeout(() => {
        const { cols, rows } = doFit();
        sendResize(cols, rows);
      }, RESIZE_DEBOUNCE_MS);
    });
    observer.observe(container);

    return () => {
      observer.disconnect();
      if (resizeTimer.current !== null) {
        clearTimeout(resizeTimer.current);
        resizeTimer.current = null;
      }
    };
  }, [doFit, sendResize]);

  // Auto-connect when the instance is running + perm is granted. The
  // user can Disconnect manually and re-Connect with the button.
  useEffect(() => {
    if (!isRunning || !hasPerm) return;
    connect();
    return () => {
      disconnect();
    };
    // We intentionally depend only on instanceId + the gates; the
    // connect/disconnect callbacks are stable enough for this lifecycle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId, isRunning, hasPerm]);

  // Permission gate: server still enforces; this is defense in depth.
  if (!hasPerm) {
    return <p className="text-sm text-muted-foreground">{t("compute.console.noPermission")}</p>;
  }

  // Instance must be running; the backend re-validates.
  if (!isRunning) {
    return <p className="text-sm text-muted-foreground">{t("compute.console.notRunning")}</p>;
  }

  const isConnecting = connectionState === "connecting";
  const isConnected = connectionState === "connected";

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={isConnected ? disconnect : connect}
          disabled={isConnecting}
        >
          {isConnected
            ? t("compute.console.disconnect")
            : isConnecting
              ? t("compute.console.connecting")
              : t("compute.console.connect")}
        </Button>
        <span className="text-xs text-muted-foreground">
          {isConnected
            ? t("compute.console.connected")
            : isConnecting
              ? t("compute.console.connecting")
              : t("compute.console.disconnected")}
        </span>
      </div>
      <div
        ref={termRef}
        className="h-80 overflow-hidden rounded-md border border-border bg-black p-2"
        aria-label={t("compute.console.title")}
        role="region"
      />
    </div>
  );
}
