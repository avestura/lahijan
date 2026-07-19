/**
 * RTL layout smoke test.
 *
 * Asserts that the active RTL locale (fa) is exposed correctly via the
 * i18n helpers, AND that the Footer (representative of every shared
 * layout component) only uses logical Tailwind utilities — not physical
 * ones (ml-, mr-, pl-, pr-, left-, right-). The ESLint rule enforces this
 * statically; this test is a runtime belt-and-suspenders check.
 */
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Footer } from "@/components/layout/Footer";
import { isRTL, LOCALE_DIRECTIONS } from "@/lib/i18n";

describe("RTL", () => {
  it("Persian is marked RTL and English LTR", () => {
    expect(isRTL("fa")).toBe(true);
    expect(LOCALE_DIRECTIONS.fa).toBe("rtl");
    expect(LOCALE_DIRECTIONS.en).toBe("ltr");
  });

  it("shared layout components emit no physical-direction utilities", () => {
    const { container } = render(<Footer />);
    const physical = /\b(ml|mr|pl|pr|left|right)-(-?\d+(?:\.\d+)?|\[.+?\])\b/;
    const offenders: string[] = [];
    for (const el of container.querySelectorAll("[class]")) {
      const cls = el.getAttribute("class") ?? "";
      const m = physical.exec(cls);
      if (m) offenders.push(`${m[0]} on <${el.tagName.toLowerCase()}>`);
    }
    expect(offenders, `physical utilities leaked: ${offenders.join(", ")}`).toEqual([]);
  });
});
