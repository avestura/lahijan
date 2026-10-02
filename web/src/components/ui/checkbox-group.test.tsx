/**
 * CheckboxGroup tests: toggling keeps option order, bulk actions work, and a
 * selected value that is missing from the options is kept (not silently dropped).
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { CheckboxGroup, type CheckboxGroupOption } from "./checkbox-group";

const OPTIONS: CheckboxGroupOption[] = [
  { value: "a.read", label: "Read A", group: "a" },
  { value: "a.write", label: "Write A", group: "a" },
  { value: "b.read", label: "Read B", description: "Reads B", group: "b" },
];

function Harness({
  initial = [],
  onChange,
}: {
  initial?: string[];
  onChange?: (v: string[]) => void;
}) {
  const [value, setValue] = useState<string[]>(initial);
  return (
    <CheckboxGroup
      id="t"
      options={OPTIONS}
      value={value}
      onChange={(v) => {
        setValue(v);
        onChange?.(v);
      }}
    />
  );
}

describe("<CheckboxGroup />", () => {
  it("checks and unchecks options, keeping option order", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<Harness onChange={onChange} />);

    await user.click(screen.getByLabelText("Read B"));
    await user.click(screen.getByLabelText("Read A"));
    expect(onChange).toHaveBeenLastCalledWith(["a.read", "b.read"]);

    await user.click(screen.getByLabelText("Read B"));
    expect(onChange).toHaveBeenLastCalledWith(["a.read"]);
  });

  it("selects all and clears all", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<Harness onChange={onChange} />);

    await user.click(screen.getByRole("button", { name: "Select all" }));
    expect(onChange).toHaveBeenLastCalledWith(["a.read", "a.write", "b.read"]);

    await user.click(screen.getByRole("button", { name: "Clear all" }));
    expect(onChange).toHaveBeenLastCalledWith([]);
  });

  it("shows group headings and descriptions", () => {
    render(<Harness />);
    expect(screen.getByText("a")).toBeInTheDocument();
    expect(screen.getByText("b")).toBeInTheDocument();
    expect(screen.getByText("Reads B")).toBeInTheDocument();
  });

  it("keeps a selected value that is not in the options list", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<Harness initial={["legacy.scope"]} onChange={onChange} />);

    expect(screen.getByLabelText("legacy.scope")).toBeChecked();
    await user.click(screen.getByLabelText("Read A"));
    expect(onChange).toHaveBeenLastCalledWith(["a.read", "legacy.scope"]);
  });
});
