/**
 * #1537 — the per-test timeout floor, set where bun itself will honour it.
 *
 * WHY THIS FILE EXISTS, because the obvious alternative does NOT work:
 * bunfig.toml's own `[test] timeout` key is **INERT** on the version this project pins and declares
 * (bun 1.3.14, `packageManager` in the root package.json). Re-verified directly: a bunfig containing
 * `[test] timeout = 30000` still kills a 6-second test at 5000ms. That trap is what produced the
 * original incident — the frontend suite silently ran at bun's 5s default for months while a config
 * file appeared to say 30s.
 *
 * A `preload`, by contrast, is honoured, and `jest.setTimeout()` from inside it sets the default for
 * every test in the run. Measured on 1.3.14, all four cases:
 *   - no --timeout flag at all            -> 30000 applies (a 6s test passes)
 *   - invoked via `bun run <script>`      -> 30000 applies
 *   - an explicit `--timeout 1000`        -> 30000 STILL applies; the preload overrides the flag
 *   - run from a directory with no bunfig -> NOT applied (bunfig is resolved from CWD, see below)
 *
 * The consequence worth understanding: the floor is now a property of the ENVIRONMENT, not of each
 * call site. It cannot be lowered by a missing flag, a flag attached to the wrong command in a chain,
 * a `$(bun test)` capture, an absolute-path bun, or a branch of a script that CI takes and a checker
 * did not. Those were six separately-confirmed ways to lose the timeout, and none of them can reach a
 * value bun applies to itself.
 *
 * ⚠ THE ONE RESIDUAL: bun resolves bunfig.toml from the CURRENT WORKING DIRECTORY, not from the test
 * file's location. `cd apps/mcp-server && bun test src/` gets the floor; `bun test apps/mcp-server/src/`
 * from the repo root does not. CI uses `working-directory:` and package scripts run with cwd set to the
 * package, so every shape in use is covered, and the root bunfig.toml covers the rest.
 *
 * ⚠ Do not delete this preload without replacing the protection. `scripts/__tests__/`'s live probe
 * exists precisely to fail if it goes missing.
 */

// This file is a MODULE (see the `export {}` below) on purpose: without it, TypeScript treats each
// top-level declaration here as a GLOBAL, and the three copies of this file (root, apps/web,
// apps/mcp-server) then collide with 'Cannot redeclare block-scoped variable'. Observed, not
// theorised - it broke typecheck the moment the third copy landed.
export {}

// `jest` is provided by bun's test runtime. Guarded so that loading this file in any other context
// (a stray import, a future non-test preload) is a no-op rather than a crash.
declare const jest: { setTimeout?: (ms: number) => void } | undefined

const TIMEOUT_FLOOR_MS = 30_000

if (typeof jest !== 'undefined' && typeof jest?.setTimeout === 'function') {
  jest.setTimeout(TIMEOUT_FLOOR_MS)
}
