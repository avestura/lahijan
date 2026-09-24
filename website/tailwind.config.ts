import type { Config } from "tailwindcss";

// Boxy design system wiring (shared with web/ so the dashboard and the
// marketing site read as one family).
//
// Every colour resolves to a Boxy *role* token defined in
// src/styles/boxy.css (`--bx-ink`, `--bx-line`, `--bx-surface`, ...). The
// role tokens flip with `[data-theme]` on <html> (plus a
// `prefers-color-scheme` fallback), and they are remapped inside the
// `.bx-inverse` scope, so utilities such as `bg-background` or
// `text-foreground` stay correct in both themes and inside the inverted
// CTA band. Because the values are `var()` references, Tailwind opacity
// modifiers (`bg-primary/80`) do not apply: use a solid role colour instead.
//
// Geometry is fixed by the system: every radius is 0 and every shadow is a
// hard offset (no blur). Motion defaults to 120ms linear.
const radiusZero = {
  none: "0",
  DEFAULT: "0",
  sm: "0",
  md: "0",
  lg: "0",
  xl: "0",
  "2xl": "0",
  "3xl": "0",
  full: "0",
};

const config: Config = {
  darkMode: ["selector", '[data-theme="dark"]'],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    // Boxy breakpoints: sm 480, md 768, lg 1024, xl 1280, 2xl 1440.
    screens: {
      sm: "480px",
      md: "768px",
      lg: "1024px",
      xl: "1280px",
      "2xl": "1440px",
    },
    container: {
      center: true,
      padding: { DEFAULT: "16px", md: "32px" },
      screens: { xl: "1280px" },
    },
    borderRadius: radiusZero,
    boxShadow: {
      none: "none",
      0: "var(--bx-shadow-0)",
      1: "var(--bx-shadow-1)",
      2: "var(--bx-shadow-2)",
      3: "var(--bx-shadow-3)",
      6: "var(--bx-shadow-6)",
      ring: "var(--bx-ring)",
      "ring-accent": "var(--bx-ring-accent)",
      // shadcn-style names kept for compatibility, all hard offsets.
      sm: "var(--bx-shadow-1)",
      DEFAULT: "var(--bx-shadow-2)",
      md: "var(--bx-shadow-2)",
      lg: "var(--bx-shadow-3)",
      xl: "var(--bx-shadow-3)",
    },
    fontFamily: {
      sans: ["var(--bx-font-sans)"],
      display: ["var(--bx-font-display)"],
      mono: ["var(--bx-font-mono)"],
    },
    // The Boxy type scale, size + 4px-snapped leading.
    fontSize: {
      "2xs": ["var(--bx-text-2xs)", { lineHeight: "var(--bx-lh-2xs)" }],
      xs: ["var(--bx-text-xs)", { lineHeight: "var(--bx-lh-xs)" }],
      sm: ["var(--bx-text-sm)", { lineHeight: "var(--bx-lh-sm)" }],
      base: ["var(--bx-text-base)", { lineHeight: "var(--bx-lh-base)" }],
      md: ["var(--bx-text-md)", { lineHeight: "var(--bx-lh-md)" }],
      lg: ["var(--bx-text-lg)", { lineHeight: "var(--bx-lh-lg)" }],
      xl: ["var(--bx-text-xl)", { lineHeight: "var(--bx-lh-xl)" }],
      "2xl": ["var(--bx-text-2xl)", { lineHeight: "var(--bx-lh-2xl)" }],
      "3xl": ["var(--bx-text-3xl)", { lineHeight: "var(--bx-lh-3xl)" }],
      "4xl": ["var(--bx-text-4xl)", { lineHeight: "var(--bx-lh-4xl)" }],
      "5xl": ["var(--bx-text-5xl)", { lineHeight: "var(--bx-lh-5xl)" }],
      "6xl": ["var(--bx-text-6xl)", { lineHeight: "var(--bx-lh-6xl)" }],
      "7xl": ["var(--bx-text-7xl)", { lineHeight: "var(--bx-lh-7xl)" }],
    },
    letterSpacing: {
      display: "var(--bx-track-display)",
      heading: "var(--bx-track-heading)",
      normal: "var(--bx-track-body)",
      label: "var(--bx-track-label)",
      micro: "var(--bx-track-micro)",
    },
    transitionDuration: {
      DEFAULT: "var(--bx-dur-2)",
      80: "var(--bx-dur-1)",
      120: "var(--bx-dur-2)",
      160: "var(--bx-dur-3)",
      240: "var(--bx-dur-4)",
    },
    transitionTimingFunction: {
      DEFAULT: "var(--bx-ease)",
      linear: "var(--bx-ease)",
      sharp: "var(--bx-ease-sharp)",
      step: "var(--bx-ease-step)",
    },
    extend: {
      colors: {
        // shadcn semantic names -> Boxy roles.
        border: "var(--bx-line)",
        input: "var(--bx-line)",
        ring: "var(--bx-focus)",
        background: "var(--bx-canvas)",
        foreground: "var(--bx-ink)",
        primary: {
          DEFAULT: "var(--bx-accent)",
          foreground: "var(--bx-on-accent)",
          hover: "var(--bx-accent-hover)",
          active: "var(--bx-accent-active)",
        },
        secondary: {
          DEFAULT: "var(--bx-surface)",
          foreground: "var(--bx-ink)",
        },
        destructive: {
          DEFAULT: "var(--bx-danger)",
          foreground: "var(--bx-on-danger)",
          hover: "var(--bx-danger-hover)",
        },
        muted: {
          DEFAULT: "var(--bx-surface-sunken)",
          foreground: "var(--bx-ink-muted)",
        },
        accent: {
          DEFAULT: "var(--bx-surface-hover)",
          foreground: "var(--bx-ink)",
        },
        popover: {
          DEFAULT: "var(--bx-surface-raised)",
          foreground: "var(--bx-ink)",
        },
        card: {
          DEFAULT: "var(--bx-surface)",
          foreground: "var(--bx-ink)",
        },
        success: {
          DEFAULT: "var(--bx-success)",
          foreground: "var(--bx-on-success)",
          soft: "var(--bx-success-soft)",
          ink: "var(--bx-ink-success)",
        },
        warning: {
          DEFAULT: "var(--bx-warning)",
          foreground: "var(--bx-on-warning)",
          soft: "var(--bx-warning-soft)",
          ink: "var(--bx-ink-warning)",
        },
        // Boxy role names, for when the shadcn vocabulary is too coarse.
        canvas: "var(--bx-canvas)",
        surface: {
          DEFAULT: "var(--bx-surface)",
          sunken: "var(--bx-surface-sunken)",
          raised: "var(--bx-surface-raised)",
          hover: "var(--bx-surface-hover)",
          active: "var(--bx-surface-active)",
          inverse: "var(--bx-surface-inverse)",
          accent: "var(--bx-surface-accent)",
        },
        ink: {
          DEFAULT: "var(--bx-ink)",
          muted: "var(--bx-ink-muted)",
          subtle: "var(--bx-ink-subtle)",
          faint: "var(--bx-ink-faint)",
          inverse: "var(--bx-ink-inverse)",
          accent: "var(--bx-ink-accent)",
        },
        line: {
          DEFAULT: "var(--bx-line)",
          subtle: "var(--bx-line-subtle)",
          strong: "var(--bx-line-strong)",
          heavy: "var(--bx-line-heavy)",
          accent: "var(--bx-line-accent)",
        },
        scrim: "var(--bx-scrim)",
      },
      borderColor: {
        DEFAULT: "var(--bx-line)",
      },
      maxWidth: {
        prose: "var(--bx-container-prose)",
        container: "var(--bx-container)",
      },
      keyframes: {
        "fade-in": {
          from: { opacity: "0" },
          to: { opacity: "1" },
        },
        "fade-in-down": {
          from: { opacity: "0", transform: "translateY(-4px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        "drawer-in": {
          from: { transform: "translateX(100%)" },
          to: { transform: "translateX(0)" },
        },
        "drawer-in-rtl": {
          from: { transform: "translateX(-100%)" },
          to: { transform: "translateX(0)" },
        },
      },
      animation: {
        "fade-in": "fade-in var(--bx-dur-4) var(--bx-ease)",
        "fade-in-down": "fade-in-down var(--bx-dur-2) var(--bx-ease-sharp)",
        "drawer-in": "drawer-in var(--bx-dur-3) var(--bx-ease-sharp)",
        "drawer-in-rtl": "drawer-in-rtl var(--bx-dur-3) var(--bx-ease-sharp)",
      },
    },
  },
  plugins: [],
};

export default config;
