/**
 * InstanceGraphicalConsole unit tests (WS-24).
 *
 * Verifies the permission gate, the instance-running gate, and the
 * "noVNC assets not vendored" notice. We mock react-i18next directly to
 * assert the keys looked up by the component, and mock usePerm to flip
 * the gate without dragging in the session store.
 */
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

import { InstanceGraphicalConsole } from "./InstanceGraphicalConsole";

const tMock = vi.fn((key: string) => key);

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tMock }),
}));

const hasPermMock = vi.fn(() => ({ hasPerm: true, isLoading: false }));

vi.mock("@/lib/perm", () => ({
  usePerm: (_slug: string) => hasPermMock(),
}));

describe("<InstanceGraphicalConsole />", () => {
  beforeEach(() => {
    tMock.mockClear();
    hasPermMock.mockReset();
    hasPermMock.mockReturnValue({ hasPerm: true, isLoading: false });
  });

  it("renders the no-permission notice when usePerm returns false", () => {
    hasPermMock.mockReturnValue({ hasPerm: false, isLoading: false });
    render(<InstanceGraphicalConsole instanceId="i1" status="Running" />);
    expect(screen.getByText("compute.graphicalConsole.noPermission")).toBeInTheDocument();
  });

  it("renders the not-running notice when the instance is stopped", () => {
    render(<InstanceGraphicalConsole instanceId="i1" status="Stopped" />);
    expect(screen.getByText("compute.graphicalConsole.notRunning")).toBeInTheDocument();
  });

  it("renders the not-running notice when status is undefined", () => {
    render(<InstanceGraphicalConsole instanceId="i1" status={undefined} />);
    expect(screen.getByText("compute.graphicalConsole.notRunning")).toBeInTheDocument();
  });

  it("renders the vendor notice when noVNC assets are missing (script onerror)", () => {
    // Force the dynamic script load to fail by intercepting document.createElement.
    const realCreate = document.createElement.bind(document);
    const spy = vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
      const el = realCreate(tag);
      if (tag === "script") {
        // Defer the onerror to the next microtask so the component's
        // setState("missing") fires after the render.
        setTimeout(() => {
          if (typeof (el as HTMLScriptElement).onerror === "function") {
            (el as HTMLScriptElement).onerror?.(new Event("error"));
          }
        }, 0);
      }
      return el;
    });
    // Ensure no cached RFB.
    delete window.RFB;
    // Ensure no pre-existing <script> tag for the noVNC URL.
    document.querySelectorAll('script[src="/novnc/core/rfb.js"]').forEach((n) => n.remove());

    render(<InstanceGraphicalConsole instanceId="i1" status="Running" />);
    // Wait for the setTimeout-mocked onerror to flush.
    return new Promise<void>((resolve) => {
      setTimeout(() => {
        expect(
          screen.getByText("compute.graphicalConsole.notVendoredTitle"),
        ).toBeInTheDocument();
        spy.mockRestore();
        resolve();
      }, 10);
    });
  });

  it("renders the connect button when permitted + running + RFB is available", () => {
    // Pretend noVNC has already loaded (window.RFB set). The fake stubs
    // the methods the component calls during render (none today) so the
    // constructor + class fields are sufficient. Cast through unknown
    // because the full RFB interface has many methods we don't need here.
    class FakeRFB {
      scaleViewport = false;
      // eslint-disable-next-line @typescript-eslint/no-empty-function
      constructor() {}
      // eslint-disable-next-line @typescript-eslint/no-empty-function
      disconnect() {}
      // eslint-disable-next-line @typescript-eslint/no-empty-function
      focus() {}
    }
    window.RFB = FakeRFB as unknown as NonNullable<Window["RFB"]>;
    render(<InstanceGraphicalConsole instanceId="i1" status="Running" />);
    expect(screen.getByText("compute.graphicalConsole.connect")).toBeInTheDocument();
    delete window.RFB;
  });
});
