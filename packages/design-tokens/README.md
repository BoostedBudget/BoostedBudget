# BoostedBudget design tokens

`src/tokens.ts` is the typed source of truth for shared semantic color and radius tokens.
Run `bun run generate` after editing it and commit all three files in `generated/`.

Three adapters are generated from that one source:

- `@boostedbudget/design-tokens/web.css` (`generated/web.css`) keeps the existing explicit `.dark`
  selector so the desktop web theme remains user-controlled.
- `@boostedbudget/design-tokens/nativewind.css` (`generated/nativewind.css`) follows
  `prefers-color-scheme` for the initial Expo system-theme policy.
- `@boostedbudget/design-tokens/native` (`generated/native.ts`) is the React Native / StyleSheet
  projection: `nativeTokens = { light, dark }` with `.color`, `.contrast`, `.spacing`,
  `.typography`, `.elevation`, `.motion`, `.touchTarget`, and `.focus`. Colours are sRGB
  hex/rgba (converted from the same `oklch()` source, never hand-maintained); numeric scales are
  unitless numbers; font families stay strings. `foundationTokens` is deliberately NOT projected
  — it is a map of `var(--…)` and `color-mix()` aliases, not literal colours.

Which platform consumes which adapter:

| Adapter | Generated artifact | Consumer |
| --- | --- | --- |
| `web.css` | `generated/web.css` | `apps/web` (desktop), imported from `apps/web/src/index.css`; explicit `.dark` class selector. |
| `nativewind.css` | `generated/nativewind.css` | `apps/mobile` via `apps/mobile/global.css`; `prefers-color-scheme` plus runtime CSS custom properties. |
| `native` | `generated/native.ts` | `apps/mobile` React Native / StyleSheet code (e.g. `nativeNavigationTokens` consumers and the theme bootstrap); a design-tokens projection, not a NativeWind feature. |

`bun run check` fails when a generated artifact is missing or stale.

## `nativeNavigationTokens` exceptions

`src/tokens.ts` also exports a hand-authored `nativeNavigationTokens` map consumed by React
Navigation. Seven of its twelve values equal the generated semantic conversion; five are
deliberately different because the navigation chrome was authored against a different neutral
step. These five are named exceptions and must not be "fixed" to match:

| Key | `nativeNavigationTokens` | Generated semantic conversion |
| --- | --- | --- |
| `light.primary` | `#262626` | `#171717` (`primary`) |
| `light.text` | `#171717` | `#0a0a0a` (`foreground`) |
| `dark.background` | `#171717` | `#0a0a0a` (`background`) |
| `dark.card` | `#262626` | `#171717` (`card`) |
| `dark.notification` | `#fb2c36` | `#ff6467` (`destructive`) |

The other seven (`light.background`, `light.card`, `light.border`, `light.notification`,
`dark.primary`, `dark.text`, `dark.border`) are asserted equal to their conversions by
`test/native-adapter.test.ts`.
