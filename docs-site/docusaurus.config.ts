import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

// Lahijan docs site config.
// Renders the markdown under ../docs/ as a Docusaurus site.
// en is the default locale; fa is the secondary locale (RTL).

const config: Config = {
  title: "Lahijan",
  tagline: "Open-source cloud platform for compute, DNS, and object storage.",
  favicon: "img/favicon.ico",

  url: "https://docs.lahijan.dev",
  baseUrl: "/",

  // GitHub Pages deploy lives at the org site; adjust if needed.
  organizationName: "avestura",
  projectName: "lahijan",

  onBrokenLinks: "throw",
  onBrokenMarkdownLinks: "warn",

  i18n: {
    defaultLocale: "en",
    locales: ["en", "fa"],
    localeConfigs: {
      en: { label: "English", direction: "ltr" },
      fa: { label: "فارسی", direction: "rtl" },
    },
  },

  presets: [
    [
      "classic",
      {
        docs: {
          path: "../docs",
          routeBasePath: "/",
          sidebarPath: "./sidebars.ts",
          editUrl:
            "https://github.com/avestura/lahijan/edit/main/docs/",
        },
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  markdown: {
    mermaid: true,
  },
  themes: ["@docusaurus/theme-mermaid"],

  themeConfig: {
    colorMode: { defaultMode: "light", respectPrefersColorScheme: true },
    navbar: {
      title: "Lahijan",
      logo: { alt: "Lahijan Logo", src: "img/logo.svg" },
      items: [
        {
          type: "doc",
          docId: "architecture/overview",
          position: "left",
          label: "Architecture",
        },
        {
          type: "doc",
          docId: "adr/README",
          position: "left",
          label: "ADRs",
        },
        {
          type: "doc",
          docId: "workstreams/README",
          position: "left",
          label: "Workstreams",
        },
        { to: "/glossary", position: "left", label: "Glossary" },
        {
          href: "https://github.com/avestura/lahijan",
          position: "right",
          label: "GitHub",
        },
        {
          type: "localeDropdown",
          position: "right",
        },
      ],
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ["sql", "go", "yaml", "bash"],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
