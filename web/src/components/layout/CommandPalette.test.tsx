/**
 * CommandPalette tests — the Boxy palette spec (components-forms.md):
 *   - typed matches render as an inverted inline block (<mark>);
 *   - no match shows the query back in the empty state;
 *   - rows carry the route they open as real meta.
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
}));

import { CommandPalette } from "./CommandPalette";

// cmdk measures its list and scrolls the selection into view; jsdom has
// neither API.
const noop = vi.fn();
if (!("ResizeObserver" in globalThis)) {
  globalThis.ResizeObserver = class {
    observe = noop;
    unobserve = noop;
    disconnect = noop;
  };
}
if (!("scrollIntoView" in Element.prototype)) {
  Object.defineProperty(Element.prototype, "scrollIntoView", { value: noop });
}

describe("<CommandPalette />", () => {
  it("highlights the matched part of a destination label", async () => {
    render(<CommandPalette open onOpenChange={noop} />);
    await userEvent.setup().type(screen.getByTestId("command-palette-input"), "dash");
    const row = screen.getByTestId("command-palette-item-dashboard");
    expect(row.querySelector("mark")).toHaveTextContent(/^Dash$/);
    expect(row).toHaveTextContent("/dashboard");
  });

  it("echoes the query when nothing matches", async () => {
    render(<CommandPalette open onOpenChange={noop} />);
    await userEvent.setup().type(screen.getByTestId("command-palette-input"), "zzqx");
    expect(await screen.findByText('No results for "zzqx"')).toBeInTheDocument();
  });
});
