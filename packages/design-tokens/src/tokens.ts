export type CssTokenMap = Readonly<Record<string, string>>

export const foundationTokens = {
  radius: '0.625rem',
  'color-text': 'var(--foreground)',
  'color-text-primary': 'var(--foreground)',
  'color-text-secondary': 'var(--muted-foreground)',
  'color-text-tertiary': 'var(--muted-foreground)',
  'color-bg-primary': 'var(--background)',
  'color-bg-secondary': 'var(--muted)',
  'color-bg-card': 'var(--card)',
  'color-border': 'var(--border)',
  'color-border-light': 'var(--border)',
  'color-success': 'var(--success)',
  'color-success-bg-light': 'color-mix(in oklch, var(--success) 12%, transparent)',
  'color-danger': 'var(--destructive)',
  'color-danger-bg-light': 'color-mix(in oklch, var(--destructive) 12%, transparent)',
  'color-warning': 'var(--warning)',
  'color-primary': 'var(--info)',
  'color-primary-50': 'color-mix(in oklch, var(--info) 10%, transparent)',
  'color-primary-100': 'color-mix(in oklch, var(--info) 20%, transparent)',
  'color-primary-200': 'color-mix(in oklch, var(--info) 30%, transparent)',
  'color-primary-300': 'color-mix(in oklch, var(--info) 40%, transparent)',
  'color-primary-500': 'var(--info)',
  'color-primary-700': 'var(--info)',
  'color-gray-50': 'oklch(0.985 0 0)',
  'color-gray-100': 'oklch(0.97 0 0)',
  'color-gray-200': 'oklch(0.922 0 0)',
  'color-gray-300': 'oklch(0.869 0 0)',
  'color-gray-400': 'oklch(0.708 0 0)',
  'color-gray-500': 'oklch(0.556 0 0)',
  'color-gray-600': 'oklch(0.453 0 0)',
  'color-gray-700': 'oklch(0.371 0 0)',
  'color-gray-750': 'oklch(0.320 0 0)',
  'color-gray-800': 'oklch(0.269 0 0)',
  'color-gray-900': 'oklch(0.145 0 0)',
  'color-blue-400': 'oklch(0.6 0.165 253.226)',
  'color-blue-900': 'oklch(0.231 0.07 253.226)',
} as const satisfies CssTokenMap

export const lightSemanticTokens = {
  background: 'oklch(1 0 0)',
  foreground: 'oklch(0.145 0 0)',
  card: 'oklch(1 0 0)',
  'card-foreground': 'oklch(0.145 0 0)',
  popover: 'oklch(1 0 0)',
  'popover-foreground': 'oklch(0.145 0 0)',
  primary: 'oklch(0.205 0 0)',
  'primary-foreground': 'oklch(0.985 0 0)',
  secondary: 'oklch(0.97 0 0)',
  'secondary-foreground': 'oklch(0.205 0 0)',
  muted: 'oklch(0.97 0 0)',
  'muted-foreground': 'oklch(0.556 0 0)',
  accent: 'oklch(0.97 0 0)',
  'accent-foreground': 'oklch(0.205 0 0)',
  destructive: 'oklch(0.577 0.245 27.325)',
  'destructive-foreground': 'oklch(0.985 0 0)',
  success: 'oklch(0.527 0.154 150.069)',
  'success-foreground': 'oklch(0.985 0 0)',
  warning: 'oklch(0.769 0.188 70.08)',
  'warning-foreground': 'oklch(0.145 0 0)',
  info: 'oklch(0.576 0.167 253.226)',
  'info-foreground': 'oklch(0.985 0 0)',
  border: 'oklch(0.922 0 0)',
  input: 'oklch(0.922 0 0)',
  ring: 'oklch(0.708 0 0)',
  'chart-1': 'oklch(0.646 0.222 41.116)',
  'chart-2': 'oklch(0.6 0.118 184.704)',
  'chart-3': 'oklch(0.398 0.07 227.392)',
  'chart-4': 'oklch(0.828 0.189 84.429)',
  'chart-5': 'oklch(0.769 0.188 70.08)',
  sidebar: 'oklch(0.985 0 0)',
  'sidebar-foreground': 'oklch(0.145 0 0)',
  'sidebar-primary': 'oklch(0.205 0 0)',
  'sidebar-primary-foreground': 'oklch(0.985 0 0)',
  'sidebar-accent': 'oklch(0.97 0 0)',
  'sidebar-accent-foreground': 'oklch(0.205 0 0)',
  'sidebar-border': 'oklch(0.922 0 0)',
  'sidebar-ring': 'oklch(0.708 0 0)',
  skeleton: 'oklch(0.905 0 0)',
} as const satisfies CssTokenMap

export const darkSemanticTokens = {
  background: 'oklch(0.145 0 0)',
  foreground: 'oklch(0.985 0 0)',
  card: 'oklch(0.205 0 0)',
  'card-foreground': 'oklch(0.985 0 0)',
  popover: 'oklch(0.205 0 0)',
  'popover-foreground': 'oklch(0.985 0 0)',
  primary: 'oklch(0.922 0 0)',
  'primary-foreground': 'oklch(0.205 0 0)',
  secondary: 'oklch(0.269 0 0)',
  'secondary-foreground': 'oklch(0.985 0 0)',
  muted: 'oklch(0.269 0 0)',
  'muted-foreground': 'oklch(0.708 0 0)',
  accent: 'oklch(0.269 0 0)',
  'accent-foreground': 'oklch(0.985 0 0)',
  destructive: 'oklch(0.704 0.191 22.216)',
  'destructive-foreground': 'oklch(0.985 0 0)',
  success: 'oklch(0.627 0.173 151.6)',
  'success-foreground': 'oklch(0.145 0 0)',
  warning: 'oklch(0.828 0.189 84.429)',
  'warning-foreground': 'oklch(0.145 0 0)',
  info: 'oklch(0.6 0.165 253.226)',
  'info-foreground': 'oklch(0.985 0 0)',
  border: 'oklch(1 0 0 / 10%)',
  input: 'oklch(1 0 0 / 15%)',
  ring: 'oklch(0.556 0 0)',
  'chart-1': 'oklch(0.488 0.243 264.376)',
  'chart-2': 'oklch(0.696 0.17 162.48)',
  'chart-3': 'oklch(0.769 0.188 70.08)',
  'chart-4': 'oklch(0.627 0.265 303.9)',
  'chart-5': 'oklch(0.645 0.246 16.439)',
  sidebar: 'oklch(0.205 0 0)',
  'sidebar-foreground': 'oklch(0.985 0 0)',
  'sidebar-primary': 'oklch(0.488 0.243 264.376)',
  'sidebar-primary-foreground': 'oklch(0.985 0 0)',
  'sidebar-accent': 'oklch(0.269 0 0)',
  'sidebar-accent-foreground': 'oklch(0.985 0 0)',
  'sidebar-border': 'oklch(1 0 0 / 10%)',
  'sidebar-ring': 'oklch(0.556 0 0)',
  skeleton: 'oklch(0.371 0 0)',
} as const satisfies CssTokenMap

// React Navigation passes these values directly to native platform views, where CSS Color 4
// syntax and custom properties are not universally supported. Keep the native-safe equivalents
// beside the semantic CSS tokens so navigation chrome shares the same design source of truth.
export const nativeNavigationTokens = {
  light: {
    primary: '#262626',
    background: '#ffffff',
    card: '#ffffff',
    text: '#171717',
    border: '#e5e5e5',
    notification: '#e7000b',
  },
  dark: {
    primary: '#e5e5e5',
    background: '#171717',
    card: '#262626',
    text: '#fafafa',
    border: '#ffffff1a',
    notification: '#fb2c36',
  },
} as const

export const tailwindThemeTokens = {
  'radius-sm': 'calc(var(--radius) - 4px)',
  'radius-md': 'calc(var(--radius) - 2px)',
  'radius-lg': 'var(--radius)',
  'radius-xl': 'calc(var(--radius) + 4px)',
  'color-background': 'var(--background)',
  'color-foreground': 'var(--foreground)',
  'color-card': 'var(--card)',
  'color-card-foreground': 'var(--card-foreground)',
  'color-popover': 'var(--popover)',
  'color-popover-foreground': 'var(--popover-foreground)',
  'color-primary': 'var(--primary)',
  'color-primary-foreground': 'var(--primary-foreground)',
  'color-secondary': 'var(--secondary)',
  'color-secondary-foreground': 'var(--secondary-foreground)',
  'color-muted': 'var(--muted)',
  'color-muted-foreground': 'var(--muted-foreground)',
  'color-accent': 'var(--accent)',
  'color-accent-foreground': 'var(--accent-foreground)',
  'color-destructive': 'var(--destructive)',
  'color-destructive-foreground': 'var(--destructive-foreground)',
  'color-success': 'var(--success)',
  'color-success-foreground': 'var(--success-foreground)',
  'color-warning': 'var(--warning)',
  'color-warning-foreground': 'var(--warning-foreground)',
  'color-info': 'var(--info)',
  'color-info-foreground': 'var(--info-foreground)',
  'color-border': 'var(--border)',
  'color-input': 'var(--input)',
  'color-ring': 'var(--ring)',
  'color-chart-1': 'var(--chart-1)',
  'color-chart-2': 'var(--chart-2)',
  'color-chart-3': 'var(--chart-3)',
  'color-chart-4': 'var(--chart-4)',
  'color-chart-5': 'var(--chart-5)',
  'color-sidebar': 'var(--sidebar)',
  'color-sidebar-foreground': 'var(--sidebar-foreground)',
  'color-sidebar-primary': 'var(--sidebar-primary)',
  'color-sidebar-primary-foreground': 'var(--sidebar-primary-foreground)',
  'color-sidebar-accent': 'var(--sidebar-accent)',
  'color-sidebar-accent-foreground': 'var(--sidebar-accent-foreground)',
  'color-sidebar-border': 'var(--sidebar-border)',
  'color-sidebar-ring': 'var(--sidebar-ring)',
  'color-skeleton': 'var(--skeleton)',
} as const satisfies CssTokenMap

// Mobile-only scales, emitted exclusively through `mobileThemeBlock()` into `nativewind.css`.
// Keys are Tailwind-namespace-shaped (`--spacing-bb-4` yields `p-bb-4`, `--text-bb-body` yields
// `text-bb-body`, `--transition-duration-bb-fast` yields `duration-bb-fast`); a variable outside a
// known namespace produces no utility. Plain `as const`, not `satisfies CssTokenMap`, because the
// numeric touch-target scale would fail that string-valued alias.
export const typographyTokens = {
  'font-bb-sans':
    "ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
  'font-bb-mono':
    "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
  'text-bb-xs': '0.75rem',
  'text-bb-sm': '0.875rem',
  'text-bb-body': '1rem',
  'text-bb-lg': '1.125rem',
  'text-bb-xl': '1.25rem',
  'text-bb-2xl': '1.5rem',
  'text-bb-3xl': '1.875rem',
  'text-bb-4xl': '2.25rem',
  'leading-bb-tight': '1.25',
  'leading-bb-snug': '1.375',
  'leading-bb-normal': '1.5',
  'leading-bb-relaxed': '1.625',
  'font-weight-bb-regular': '400',
  'font-weight-bb-medium': '500',
  'font-weight-bb-semibold': '600',
  'font-weight-bb-bold': '700',
  'tracking-bb-tight': '-0.025em',
  'tracking-bb-normal': '0em',
  'tracking-bb-wide': '0.025em',
} as const

export const spacingTokens = {
  'spacing-bb-0': '0px',
  'spacing-bb-1': '4px',
  'spacing-bb-2': '8px',
  'spacing-bb-3': '12px',
  'spacing-bb-4': '16px',
  'spacing-bb-5': '20px',
  'spacing-bb-6': '24px',
  'spacing-bb-8': '32px',
  'spacing-bb-10': '40px',
  'spacing-bb-12': '48px',
  'spacing-bb-16': '64px',
} as const

export const elevationTokens = {
  'shadow-bb-sm': '0 1px 2px 0 rgb(0 0 0 / 0.05)',
  'shadow-bb-md': '0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)',
  'shadow-bb-lg': '0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)',
} as const

export const motionTokens = {
  'transition-duration-bb-fast': '150ms',
  'transition-duration-bb-normal': '250ms',
  'transition-duration-bb-slow': '400ms',
  'ease-bb-standard': 'cubic-bezier(0.4, 0, 0.2, 1)',
  'ease-bb-decelerate': 'cubic-bezier(0, 0, 0.2, 1)',
  'ease-bb-accelerate': 'cubic-bezier(0.4, 0, 1, 1)',
} as const

// Numeric (not CSS) values: consumed by the native adapter and by primitive `minHeight`/`minWidth`
// floors. Emitted as `--bb-touch-min` / `--bb-touch-comfortable` so the generic `--min` name never
// leaks into the global custom-property namespace.
export const touchTargetTokens = {
  min: 44,
  comfortable: 48,
} as const

export const focusTokens = {
  'bb-focus-ring-width': '2px',
  'bb-focus-ring-offset': '2px',
  'bb-focus-ring-color': 'var(--ring)',
} as const

// Contrast remediation. These are ADDITIVE derived colours that fix four pairs the existing
// semantic palette cannot satisfy at 4.5:1. They are emitted ONLY through `mobileThemeBlock()`
// into `nativewind.css` — never into `lightSemanticTokens`/`darkSemanticTokens`/`tailwindThemeTokens`
// — so `generated/web.css` stays byte-identical. The `*-text` tokens are darker body text on a
// neutral surface; the `*-strong` tokens are darker filled surfaces that let the near-white
// `info-foreground` / `destructive-foreground` clear 4.5:1. The dark half flips to lighter body
// text and a darker filled surface.
export const contrastTokens = {
  light: {
    'bb-info-text': 'oklch(0.5 0.167 253.226)',
    'bb-danger-text': 'oklch(0.5 0.245 27.325)',
    'bb-info-strong': 'oklch(0.5 0.167 253.226)',
    'bb-danger-strong': 'oklch(0.5 0.245 27.325)',
  },
  dark: {
    'bb-info-text': 'oklch(0.72 0.167 253.226)',
    'bb-danger-text': 'oklch(0.72 0.245 27.325)',
    'bb-info-strong': 'oklch(0.48 0.167 253.226)',
    'bb-danger-strong': 'oklch(0.48 0.245 27.325)',
  },
} as const
