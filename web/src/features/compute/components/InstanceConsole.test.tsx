/**
 * InstanceConsole unit tests (WS-32).
 *
 * Asserts the headline behavioural changes:
 *  1. No Run button — the terminal IS the input now (no <form> +
 *     <Input> + Run button).
 *  2. Opening the tab with a Running instance + perm triggers a WS
 *     open (mock WebSocket).
 *  3. A ResizeObserver trigger sends a resize control message after
 *     the debounce window.
 *
 * xterm.js needs a real canvas (jsdom doesn't ship one), so we mock
 * @xterm/xterm + @xterm/addon-fit at the module boundary. The mock
 * exposes the same surface the component touches (.onData, .write,
 * .cols, .rows, .focus, .dispose, .loadAddon, .open) so the WS bridge
 * wiring is exercised without pulling in the rendering layer.
 *
 * We also mock react-i18next, usePerm, and the WebSocket constructor
 * so the assertions do not depend on the session store or a real
 * backend.
 */
import { render, screen, act, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { InstanceConsole } from "./InstanceConsole";

// ---------------------------------------------------------------------------
// Terminal / FitAddon mock
// ---------------------------------------------------------------------------

/**
 * onDataCallbacks captures every Disposable returned by Terminal.onData
 * across all instances so the test can fire synthetic keystrokes.
 */
const onDataCallbacks: ((data: string) => void)[] = [];

/**
 * terminalInstances captures every Terminal the component constructs so
 * tests can assert against the latest instance (cols, rows, written).
 */
interface TerminalStub {
  cols: number;
  rows: number;
  written: string[];
  disposed: boolean;
  focusCalls: number;
}

const terminalInstances: TerminalStub[] = [];

let fitCalls = 0;

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    written: string[] = [];
    disposed = false;
    focusCalls = 0;

    // The real constructor accepts an options bag; we ignore it.
    constructor(_options?: unknown) {
      terminalInstances.push(this);
    }
    // eslint-disable-next-line @typescript-eslint/no-empty-function
    open(_el: HTMLElement): void {}
    // eslint-disable-next-line @typescript-eslint/no-empty-function
    loadAddon(_addon: unknown): void {}
    onData(cb: (data: string) => void): { dispose: () => void } {
      onDataCallbacks.push(cb);
      return {
        // eslint-disable-next-line @typescript-eslint/no-empty-function
        dispose: () => {},
      };
    }
    write(data: string): void {
      this.written.push(data);
    }
    focus(): void {
      this.focusCalls++;
    }
    dispose(): void {
      this.disposed = true;
    }
  },
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit(): void {
      fitCalls++;
    }
  },
}));

// ---------------------------------------------------------------------------
// Other mocks
// ---------------------------------------------------------------------------

const tMock = vi.fn((key: string) => key);

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tMock }),
}));

const hasPermMock = vi.fn(() => ({ hasPerm: true, isLoading: false }));

vi.mock("@/lib/perm", () => ({
  usePerm: (_slug: string) => hasPermMock(),
}));

// The console WS URL carries ?tenant_id= because the browser's WebSocket
// API cannot set the X-Tenant-Id header (WS-32). Mock the session store so
// the component has a deterministic tenant id to stamp on the URL.
const FIXED_TENANT_ID = "00000000-0000-0000-0000-0000000000ab";
vi.mock("@/lib/stores/session-store", () => ({
  useSessionStore: (selector: (s: { currentTenantId: string | null }) => unknown) =>
    selector({ currentTenantId: FIXED_TENANT_ID }),
}));

/**
 * wsStub mirrors the bits of the real WebSocket the component touches.
 * The URL is captured for assertion; the state machine is just enough
 * to satisfy the readyState checks in the bridge.
 */
interface wsStub {
  url: string;
  readyState: number;
  onopen: (() => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: (() => void) | null;
  onerror: (() => void) | null;
  sent: string[];
  close: () => void;
}

const wsInstances: wsStub[] = [];

class WebSocketStub {
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  url: string;
  readyState: number;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    this.readyState = WebSocketStub.OPEN;
    wsInstances.push(this);
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.readyState = WebSocketStub.CLOSED;
    this.onclose?.();
  }
}

const realWebSocket = globalThis.WebSocket;

// Capture the ResizeObserver callback so tests can fire synthetic
// size changes.
type ROCallback = (entries: { target: Element }[], observer: unknown) => void;
let roCallback: ROCallback | null = null;

class ResizeObserverStub {
  constructor(cb: ROCallback) {
    roCallback = cb;
  }
  // eslint-disable-next-line @typescript-eslint/no-empty-function
  observe(): void {}
  // eslint-disable-next-line @typescript-eslint/no-empty-function
  disconnect(): void {}
  // eslint-disable-next-line @typescript-eslint/no-empty-function
  unobserve(): void {}
}
const realResizeObserver = globalThis.ResizeObserver;

// ---------------------------------------------------------------------------
// Setup / teardown
// ---------------------------------------------------------------------------

beforeEach(() => {
  tMock.mockClear();
  hasPermMock.mockReset();
  hasPermMock.mockReturnValue({ hasPerm: true, isLoading: false });
  wsInstances.length = 0;
  terminalInstances.length = 0;
  onDataCallbacks.length = 0;
  fitCalls = 0;
  roCallback = null;
  globalThis.WebSocket = WebSocketStub as unknown as typeof WebSocket;
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
});

afterEach(() => {
  globalThis.WebSocket = realWebSocket;
  globalThis.ResizeObserver = realResizeObserver;
  roCallback = null;
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("<InstanceConsole />", () => {
  it("does not render a Run button — the terminal IS the input", () => {
    render(<InstanceConsole instanceId="i1" status="Running" />);
    // The headline assertion: no Run button anywhere. The only
    // buttons are Connect/Disconnect.
    expect(screen.queryByRole("button", { name: /run/i })).toBeNull();
    // The new component renders a Connect/Disconnect button + a
    // status label. No <form>, no Run.
    expect(screen.getByRole("button")).toBeInTheDocument();
  });

  it("shows the no-permission notice when usePerm returns false", () => {
    hasPermMock.mockReturnValue({ hasPerm: false, isLoading: false });
    render(<InstanceConsole instanceId="i1" status="Running" />);
    expect(screen.getByText("compute.console.noPermission")).toBeInTheDocument();
  });

  it("shows the not-running notice when the instance is stopped", () => {
    render(<InstanceConsole instanceId="i1" status="Stopped" />);
    expect(screen.getByText("compute.console.notRunning")).toBeInTheDocument();
  });

  it("opens a WebSocket to /console when mounted with a Running instance + perm", () => {
    render(<InstanceConsole instanceId="i1" status="Running" />);
    expect(wsInstances).toHaveLength(1);
    expect(wsInstances[0]?.url).toContain("/api/v1/compute/instances/i1/console");
  });

  it("appends ?tenant_id= to the WS URL (the browser WebSocket API cannot set headers)", () => {
    render(<InstanceConsole instanceId="i1" status="Running" />);
    expect(wsInstances).toHaveLength(1);
    // The backend tenant middleware resolves the tenant from this query
    // param as a fallback for the header-less WS upgrade (WS-32).
    expect(wsInstances[0]?.url).toContain(`tenant_id=${FIXED_TENANT_ID}`);
  });

  it("does NOT open a WebSocket when the instance is not running", () => {
    render(<InstanceConsole instanceId="i1" status="Stopped" />);
    expect(wsInstances).toHaveLength(0);
  });

  it("renders Connect initially, then Disconnect after the WS opens", async () => {
    render(<InstanceConsole instanceId="i1" status="Running" />);
    expect(wsInstances).toHaveLength(1);
    const stub = wsInstances[0];
    expect(stub).toBeDefined();

    act(() => {
      stub?.onopen?.();
    });
    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "compute.console.disconnect" }),
      ).toBeInTheDocument();
    });
  });

  it("forwards terminal keystrokes to the WS as text frames", async () => {
    render(<InstanceConsole instanceId="i1" status="Running" />);
    const stub = wsInstances[0];
    expect(stub).toBeDefined();
    act(() => {
      stub?.onopen?.();
    });

    // The Terminal mock captured the onData callback; fire a synthetic
    // keystroke + assert the WS received it verbatim.
    expect(onDataCallbacks.length).toBeGreaterThan(0);
    act(() => {
      onDataCallbacks[0]?.("ls -la\r");
    });

    expect(stub?.sent).toContain("ls -la\r");
  });

  it("writes Incus stdout bytes to the terminal", async () => {
    render(<InstanceConsole instanceId="i1" status="Running" />);
    const stub = wsInstances[0];
    expect(stub).toBeDefined();
    act(() => {
      stub?.onopen?.();
    });

    const term = terminalInstances[0];
    expect(term).toBeDefined();
    if (!term) return;

    act(() => {
      stub?.onmessage?.({ data: "hello-from-incus" });
    });
    expect(term.written).toContain("hello-from-incus");
  });

  it("sends a resize control message when the ResizeObserver fires (after the debounce)", async () => {
    vi.useFakeTimers();
    try {
      render(<InstanceConsole instanceId="i1" status="Running" />);
      const stub = wsInstances[0];
      expect(stub).toBeDefined();
      act(() => {
        stub?.onopen?.();
      });
      // Clear sent messages so we only see the resize from the observer.
      if (stub) stub.sent.length = 0;

      // Fire a synthetic resize observation.
      expect(roCallback).not.toBeNull();
      act(() => {
        roCallback?.([{ target: document.body }], null);
      });
      // Before the debounce window: no resize message yet.
      expect(stub?.sent.find((s) => s.includes('"type":"resize"'))).toBeUndefined();

      // Advance past the debounce window.
      act(() => {
        vi.advanceTimersByTime(80);
      });

      // The mock Terminal reports cols=80 rows=24; the resize
      // control message must carry those values.
      const resize = stub?.sent.find((s) => s.includes('"type":"resize"'));
      expect(resize).toBeDefined();
      expect(resize).toContain('"cols":80');
      expect(resize).toContain('"rows":24');
    } finally {
      vi.useRealTimers();
    }
  });

  it("calls fitAddon.fit() on the debounce after a resize observation", async () => {
    vi.useFakeTimers();
    try {
      render(<InstanceConsole instanceId="i1" status="Running" />);
      const stub = wsInstances[0];
      expect(stub).toBeDefined();
      act(() => {
        stub?.onopen?.();
      });
      // fit() is also called once during mount; reset the counter so
      // we can isolate the observer-driven fit.
      const fitCallsBeforeObserver = fitCalls;

      expect(roCallback).not.toBeNull();
      act(() => {
        roCallback?.([{ target: document.body }], null);
      });
      expect(fitCalls).toBe(fitCallsBeforeObserver); // pre-debounce no-op

      act(() => {
        vi.advanceTimersByTime(80);
      });
      expect(fitCalls).toBeGreaterThan(fitCallsBeforeObserver);
    } finally {
      vi.useRealTimers();
    }
  });

  it("closes the WS when the Disconnect button is clicked", async () => {
    render(<InstanceConsole instanceId="i1" status="Running" />);
    const stub = wsInstances[0];
    expect(stub).toBeDefined();
    act(() => {
      stub?.onopen?.();
    });

    const disconnectBtn = await screen.findByRole("button", {
      name: "compute.console.disconnect",
    });
    act(() => {
      disconnectBtn.click();
    });

    expect(stub?.readyState).toBe(WebSocketStub.CLOSED);
  });

  it("renders the connect button label + title from i18n", () => {
    render(<InstanceConsole instanceId="i1" status="Running" />);
    expect(tMock).toHaveBeenCalledWith("compute.console.connect");
    expect(tMock).toHaveBeenCalledWith("compute.console.title");
  });
});
