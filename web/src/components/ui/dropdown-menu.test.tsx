/**
 * DropdownMenu tests — the Boxy menu contract (components-overlays.md):
 *   - the content is a raised scope, so hover stays visible in dark mode;
 *   - destructive items carry the danger treatment;
 *   - radio items expose menuitemradio + checked state for the 6px mark.
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "./dropdown-menu";

function Fixture() {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger>open</DropdownMenuTrigger>
      <DropdownMenuContent data-testid="content">
        <DropdownMenuItem>plain</DropdownMenuItem>
        <DropdownMenuItem variant="destructive">danger</DropdownMenuItem>
        <DropdownMenuRadioGroup value="b">
          <DropdownMenuRadioItem value="a">alpha</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="b">beta</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

describe("<DropdownMenu />", () => {
  it("renders the content as a raised floating surface", async () => {
    render(<Fixture />);
    await userEvent.setup().click(screen.getByText("open"));
    const content = screen.getByTestId("content");
    expect(content).toHaveClass("bx-raised");
    expect(content).toHaveClass("border-line-heavy");
    expect(content).toHaveClass("shadow-hard-2");
  });

  it("gives destructive items the danger ink and highlight", async () => {
    render(<Fixture />);
    await userEvent.setup().click(screen.getByText("open"));
    const danger = screen.getByRole("menuitem", { name: "danger" });
    expect(danger).toHaveClass("text-destructive-ink");
    expect(danger.className).toContain("data-[highlighted]:bg-destructive-soft");
    expect(screen.getByRole("menuitem", { name: "plain" })).not.toHaveClass("text-destructive-ink");
  });

  it("marks the chosen radio item as checked", async () => {
    render(<Fixture />);
    await userEvent.setup().click(screen.getByText("open"));
    const radios = screen.getAllByRole("menuitemradio");
    expect(radios).toHaveLength(2);
    expect(radios[1]).toHaveAttribute("aria-checked", "true");
    expect(radios[0]).toHaveAttribute("aria-checked", "false");
  });
});
