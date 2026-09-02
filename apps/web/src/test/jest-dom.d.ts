/**
 * Teach `bun:test`'s `expect` about the jest-dom matchers (#1569 / #1591 follow-up).
 *
 * `src/test/testing-library.ts` calls `expect.extend(matchers)` at preload time, so every
 * `toBeInTheDocument()` in the suite works at RUNTIME. It did not work at TYPE time:
 * `@testing-library/jest-dom` ships augmentations for jest's and vitest's `Matchers`, not bun's,
 * so `expect(el).toBeInTheDocument()` is a `TS2339` under `tsc`. That went unnoticed for as long
 * as test files were excluded from `tsc --noEmit` entirely (`tsconfig.json`'s `exclude`), which
 * is the hole `tsconfig.test.json` closes — 3,831 of the 5,951 errors that hole was hiding were
 * this one missing augmentation.
 */
import type { TestingLibraryMatchers } from '@testing-library/jest-dom/matchers'

declare module 'bun:test' {
  // eslint-disable-next-line @typescript-eslint/no-empty-object-type
  interface Matchers<T = unknown> extends TestingLibraryMatchers<T, void> {}
  // eslint-disable-next-line @typescript-eslint/no-empty-object-type
  interface AsymmetricMatchers extends TestingLibraryMatchers<unknown, void> {}
}
