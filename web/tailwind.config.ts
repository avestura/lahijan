import type { Config } from "tailwindcss";

/**
 * Tailwind theme mapped onto the Boxy design system (src/styles/boxy.css).
 *
 * The shadcn-style semantic names (`bg-card`, `border-border`,
 * `text-muted-foreground`, `bg-primary`, …) resolve to Boxy *role* tokens, so
 * light/dark themes, density and modes all come from boxy.css. Values are
 * plain `var()` references: opacity modifiers (`bg-primary/80`) do not apply
 * and are not used — Boxy uses solid surfaces and hairlines instead.
 *
 * Axioms enforced here: every radius is 0, every shadow is a hard offset
 * (or none), type sizes sit on the Boxy scale, motion is 80–240ms linear.
 */
const role = (name: string) => `var(--bx-${name})`;

const config: Config = {
  // Theme is driven by [data-theme] (see theme-store); `.dark` is kept on
  // <html> for any code that still checks it.
  darkMode: ["selector", '[data-theme="dark"]'],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    container: {
      center: true,
      padding: "1rem",
      screens: {
        "2xl": "1440px",
      },
    },
    borderRadius: {
      none: "0",
      DEFAULT: "0",
      sm: "0",
      md: "0",
      lg: "0",
      xl: "0",
      "2xl": "0",
      "3xl": "0",
      full: "0",
    },
    boxShadow: {
      // Hard offsets only (motion-depth.md). No blur, ever. The default
      // Tailwind names (shadow-sm/md/lg) are intentionally absent: they read
      // as soft shadows, and any leftover use now renders nothing.
      none: "none",
      seam: role("shadow-1"),
      "hard-2": role("shadow-2"),
      "hard-3": role("shadow-3"),
      ring: role("ring"),
    },
    fontSize: {
      // Boxy type scale (tokens.md). Tailwind names keep their usual role:
      // text-xs = captions, text-sm = default UI text, text-base = body.
      "2xs": [role("text-2xs"), { lineHeight: role("lh-2xs") }],
      label: [role("text-xs"), { lineHeight: role("lh-xs") }],
      xs: [role("text-sm"), { lineHeight: role("lh-sm") }],
      sm: [role("text-base"), { lineHeight: role("lh-base") }],
      base: [role("text-md"), { lineHeight: role("lh-md") }],
      lg: [role("text-lg"), { lineHeight: role("lh-lg") }],
      xl: [role("text-xl"), { lineHeight: role("lh-xl") }],
      "2xl": [role("text-2xl"), { lineHeight: role("lh-2xl") }],
      "3xl": [role("text-3xl"), { lineHeight: role("lh-3xl") }],
      "4xl": [role("text-4xl"), { lineHeight: role("lh-4xl") }],
    },
    fontWeight: {
      normal: "400",
      medium: "500",
      semibold: "600",
      bold: "700",
    },
    transitionDuration: {
      DEFAULT: "120ms",
      75: "80ms",
      100: "80ms",
      150: "120ms",
      200: "160ms",
      300: "240ms",
    },
    transitionTimingFunction: {
      DEFAULT: "linear",
      linear: "linear",
      sharp: role("ease-sharp"),
      step: role("ease-step"),
    },
    extend: {
      colors: {
        // shadcn semantic names → Boxy roles
        border: role("line"),
        input: role("line"),
        ring: role("focus"),
        background: role("canvas"),
        foreground: role("ink"),
        primary: {
          DEFAULT: role("accent"),
          hover: role("accent-hover"),
          active: role("accent-active"),
          soft: role("accent-soft"),
          foreground: role("on-accent"),
        },
        secondary: {
          DEFAULT: role("surface-sunken"),
          foreground: role("ink"),
        },
        destructive: {
          DEFAULT: role("danger"),
          hover: role("danger-hover"),
          soft: role("danger-soft"),
          ink: role("ink-danger"),
          foreground: role("on-danger"),
        },
        muted: {
          DEFAULT: role("surface-sunken"),
          foreground: role("ink-muted"),
        },
        accent: {
          DEFAULT: role("surface-hover"),
          foreground: role("ink"),
        },
        popover: {
          DEFAULT: role("surface-raised"),
          foreground: role("ink"),
        },
        card: {
          DEFAULT: role("surface"),
          foreground: role("ink"),
        },
        success: {
          DEFAULT: role("success"),
          soft: role("success-soft"),
          ink: role("ink-success"),
          foreground: role("on-success"),
        },
        warning: {
          DEFAULT: role("warning"),
          soft: role("warning-soft"),
          ink: role("ink-warning"),
          foreground: role("on-warning"),
        },
        sidebar: {
          DEFAULT: role("surface"),
          foreground: role("ink-muted"),
          accent: role("surface-active"),
          "accent-fg": role("ink"),
          border: role("line"),
        },
        // Boxy roles by their own names, for the cases the shadcn set lacks.
        canvas: role("canvas"),
        surface: {
          DEFAULT: role("surface"),
          sunken: role("surface-sunken"),
          raised: role("surface-raised"),
          hover: role("surface-hover"),
          active: role("surface-active"),
          inverse: role("surface-inverse"),
          accent: role("surface-accent"),
        },
        ink: {
          DEFAULT: role("ink"),
          muted: role("ink-muted"),
          subtle: role("ink-subtle"),
          faint: role("ink-faint"),
          accent: role("ink-accent"),
          inverse: role("ink-inverse"),
        },
        line: {
          subtle: role("line-subtle"),
          DEFAULT: role("line"),
          strong: role("line-strong"),
          heavy: role("line-heavy"),
          accent: role("line-accent"),
        },
      },
      fontFamily: {
        sans: [role("font-sans")],
        display: [role("font-display")],
        mono: [role("font-mono")],
      },
      spacing: {
        topbar: "var(--app-topbar-h)",
        sidebar: "var(--app-sidebar-w)",
      },
      keyframes: {
        "fade-in": {
          from: { opacity: "0" },
          to: { opacity: "1" },
        },
        "enter-up": {
          from: { opacity: "0", transform: "translateY(4px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        // Floating surfaces drop in from 4px above (components-overlays.md).
        drop: {
          from: { opacity: "0", transform: "translateY(-4px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        "slide-in-right": {
          from: { transform: "translateX(100%)" },
          to: { transform: "translateX(0)" },
        },
      },
      animation: {
        "fade-in": "fade-in 120ms linear",
        // Submenus: opacity only, 80ms, no translate.
        "fade-fast": "fade-in 80ms linear",
        drop: "drop 120ms var(--bx-ease-sharp)",
        "enter-up": "enter-up 160ms var(--bx-ease-sharp)",
        "slide-in-right": "slide-in-right 160ms var(--bx-ease-sharp)",
      },
    },
  },
  plugins: [],
};

export default config;
