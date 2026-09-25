/**
 * Input modality — records whether the user last drove the UI with the
 * keyboard or a pointer, as `<html data-input="keyboard|pointer">`.
 *
 * Boxy menus mark a highlighted item with a fill, and add a 2px accent bar
 * only for keyboard focus (components-overlays.md). Radix moves real focus
 * with the pointer, so `:focus-visible` cannot tell the two apart reliably;
 * this attribute can. A pointer move hands the highlight back to the mouse,
 * so the keyboard carries on from wherever the pointer left off.
 */
const NAV_KEYS = new Set([
  "ArrowUp",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "Home",
  "End",
  "Tab",
  "Enter",
  " ",
]);

/** setupInputModality installs the listeners once; returns a cleanup. */
export function setupInputModality(doc: Document = document): () => void {
  const root = doc.documentElement;
  const set = (mode: "keyboard" | "pointer") => {
    if (root.dataset.input !== mode) root.dataset.input = mode;
  };
  const onKey = (e: KeyboardEvent) => {
    if (NAV_KEYS.has(e.key) || e.key.length === 1) set("keyboard");
  };
  const onPointer = () => set("pointer");
  doc.addEventListener("keydown", onKey, true);
  doc.addEventListener("pointerdown", onPointer, true);
  doc.addEventListener("pointermove", onPointer, true);
  set("pointer");
  return () => {
    doc.removeEventListener("keydown", onKey, true);
    doc.removeEventListener("pointerdown", onPointer, true);
    doc.removeEventListener("pointermove", onPointer, true);
  };
}
