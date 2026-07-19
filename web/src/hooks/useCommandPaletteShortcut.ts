/**
 * useCommandPaletteShortcut — wire ⌘K / Ctrl+K to open the palette.
 *
 * Lives in its own module so the `CommandPalette.tsx` file doesn't trip
 * react-refresh's "only export components" warning.
 */
import * as React from "react";

export function useCommandPaletteShortcut(onToggle: () => void): void {
  React.useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        onToggle();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onToggle]);
}
