/**
 * Local ESLint plugin: @lahijan/i18n.
 *
 * Enforces the two non-negotiable frontend rules from
 * docs/architecture/conventions.md#frontend and ADR-0017:
 *
 *   1. `no-jsx-literal-strings` — every user-visible string in JSX must go
 *      through react-i18next's `t("key")`. Bare string literals as JSX
 *      children (and in user-facing props like `title` / `aria-label`) are
 *      blocked. Whitespace-only strings, numeric/symbolic content, and
 *      i18n keys (identifiers that look like `area.scope.label`) are
 *      permitted. This matches the spirit of the rule: do not show a user
 *      English text that hasn't been translated.
 *
 *   2. `no-physical-directional-utilities` — flags Tailwind physical
 *      layout utilities (ml-, mr-, pl-, pr-, left-, right-) that break
 *      RTL layout. Logical equivalents (ms-, me-, ps-, pe-, start-, end-)
 *      are required.
 *
 * Both rules are intentionally small and conservative; we'd rather miss the
 * occasional odd case than produce false positives that block development.
 */
import noJsxLiteralStrings from "./rules/no-jsx-literal-strings.js";
import noPhysicalDirectionalUtilities from "./rules/no-physical-directional-utilities.js";

const plugin = {
  meta: { name: "@lahijan/i18n" },
  rules: {
    "no-jsx-literal-strings": noJsxLiteralStrings,
    "no-physical-directional-utilities": noPhysicalDirectionalUtilities,
  },
};

export default plugin;
