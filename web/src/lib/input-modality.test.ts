import { afterEach, describe, expect, it } from "vitest";

import { setupInputModality } from "./input-modality";

describe("setupInputModality", () => {
  let cleanup: (() => void) | undefined;
  afterEach(() => {
    cleanup?.();
    delete document.documentElement.dataset.input;
  });

  it("starts as pointer and switches with the last input used", () => {
    cleanup = setupInputModality();
    const root = document.documentElement;
    expect(root.dataset.input).toBe("pointer");

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown" }));
    expect(root.dataset.input).toBe("keyboard");

    document.dispatchEvent(new Event("pointermove"));
    expect(root.dataset.input).toBe("pointer");
  });

  it("ignores modifier-only keys", () => {
    cleanup = setupInputModality();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Shift" }));
    expect(document.documentElement.dataset.input).toBe("pointer");
  });

  it("stops tracking after cleanup", () => {
    setupInputModality()();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown" }));
    expect(document.documentElement.dataset.input).toBe("pointer");
  });
});
