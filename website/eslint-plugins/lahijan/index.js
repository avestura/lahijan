/**
 * Local ESLint plugin: @lahijan/i18n.
 *
 * Mirrors web/eslint-plugins/lahijan/index.js — same rules, same behaviour.
 * Kept duplicated (not symlinked) so each SPA is independently buildable on
 * Windows hosts where symlinks need elevated permissions.
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
