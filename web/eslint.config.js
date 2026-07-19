// ESLint v9 flat config for the Lahijan dashboard SPA.
//
// - typescript-eslint strict
// - react + react-hooks + react-refresh
// - prettier integration (turns off conflicting formatting rules)
// - local @lahijan/i18n plugin (no hardcoded strings; no physical layout
//   utilities) — required by ADR-0017 + docs/architecture/conventions.md
import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import react from "eslint-plugin-react";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";
import prettier from "eslint-config-prettier";
import lahijanI18n from "./eslint-plugins/lahijan/index.js";

export default tseslint.config(
  {
    ignores: [
      "dist/**",
      "node_modules/**",
      "src/routeTree.gen.ts",
      "coverage/**",
      ".vite/**",
      "eslint-plugins/**",
    ],
  },

  // Base JS + TS (non-type-checked) recommended rules apply everywhere.
  js.configs.recommended,
  ...tseslint.configs.recommended,

  // Type-checked rules + project-aware linting apply only to TS source
  // under src/ (where the tsconfig's projectService knows about files).
  {
    files: ["src/**/*.{ts,tsx}"],
    extends: [...tseslint.configs.recommendedTypeChecked, ...tseslint.configs.stylisticTypeChecked],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: {
        ...globals.browser,
      },
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      react,
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
      "@lahijan/i18n": lahijanI18n,
    },
    rules: {
      // React.
      ...react.configs.recommended.rules,
      ...reactHooks.configs.recommended.rules,
      "react/react-in-jsx-scope": "off",
      "react/prop-types": "off",
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],

      // i18n + RTL.
      "@lahijan/i18n/no-jsx-literal-strings": "error",
      "@lahijan/i18n/no-physical-directional-utilities": "error",

      // Type-safe conventions.
      "@typescript-eslint/consistent-type-imports": [
        "error",
        { prefer: "type-imports", fixStyle: "inline-type-imports" },
      ],
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
      "@typescript-eslint/no-misused-promises": [
        "error",
        // Allow Promise-returning handlers in JSX attributes (onSubmit,
        // onClick, etc.). React tolerates these fine and react-hook-form's
        // handleSubmit is the canonical example.
        { checksVoidReturn: { attributes: false } },
      ],
    },
    settings: {
      react: { version: "detect" },
    },
  },

  // Loose rules for non-TS scripts (Node-context).
  {
    files: ["scripts/**/*.{mjs,js}"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: { ...globals.node },
    },
  },

  // Config files (TS, but not part of the main project).
  {
    files: ["*.config.{ts,js,mjs}", "postcss.config.js", "tailwind.config.ts"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: { ...globals.node },
    },
    rules: {
      "@typescript-eslint/no-require-imports": "off",
    },
  },

  // Tests can be a little looser.
  {
    files: ["src/**/*.{test,spec}.{ts,tsx}", "src/test/**"],
    languageOptions: {
      globals: { ...globals.jest, ...globals.browser },
    },
    rules: {
      "@typescript-eslint/no-non-null-assertion": "off",
      "@typescript-eslint/no-unsafe-assignment": "off",
      "@typescript-eslint/no-unsafe-member-access": "off",
      "@typescript-eslint/no-unsafe-call": "off",
      "@typescript-eslint/no-unsafe-argument": "off",
      "@typescript-eslint/require-await": "off",
      "@lahijan/i18n/no-jsx-literal-strings": "off",
    },
  },

  // Don't let formatting rules fight prettier.
  prettier,
);
