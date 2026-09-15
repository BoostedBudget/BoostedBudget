# BoostedBudget design tokens

`src/tokens.ts` is the typed source of truth for shared semantic color and radius tokens.
Run `bun run generate` after editing it and commit both files in `generated/`.

- `@boostedbudget/design-tokens/web.css` keeps the existing explicit `.dark` selector so the
  desktop web theme remains user-controlled.
- `@boostedbudget/design-tokens/nativewind.css` follows `prefers-color-scheme` for the initial
  Expo system-theme policy.

`bun run check` fails when a generated artifact is missing or stale.
