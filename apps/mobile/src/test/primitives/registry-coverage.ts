import { readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'

/**
 * Shared registry-coverage logic for the primitive barrel.
 *
 * Extracted so the Todo 27 registry guard (`src/test/primitives/registry.test.ts`) and the Todo 20
 * accessibility coverage suite both read ONE source. The accessibility suite must fail when a new
 * component file appears under `components/ui/` without a registry entry, and duplicating the
 * exemption list in two test files is exactly how the two guards would drift apart.
 */

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))

/** Absolute path to the primitive directory the registry is derived from. */
export const UI_DIR = resolve(MOBILE_ROOT, 'src/components/ui')

/**
 * Component files that render no text on a surface, so there is no WCAG contrast pair to derive and
 * no registry entry to point at. Every OTHER `.tsx` file that exports a component MUST be referenced
 * by at least one registry entry: adding a new component file without an entry (and without
 * deliberately adding it here) fails the coverage assertion.
 */
export const NON_TEXT_BEARING_FILES: readonly string[] = [
  'BottomSheet.tsx',
  'IconButton.tsx',
  'List.tsx',
  'ProgressBar.tsx',
  'Screen.tsx',
  'Separator.tsx',
  'Skeleton.tsx',
  'Spinner.tsx',
  'ToastProvider.tsx',
]

/** Every `.ts`/`.tsx` file currently on disk under `components/ui/`. */
export function uiFiles(): string[] {
  return readdirSync(UI_DIR).filter((file) => file.endsWith('.ts') || file.endsWith('.tsx'))
}

/**
 * The `.tsx` component files whose exported function components are not referenced by ANY of the
 * supplied registry components. An empty result means the registry covers every text-bearing file.
 */
export function findUncoveredComponentFiles(registeredComponents: Iterable<unknown>): string[] {
  const registered = new Set(registeredComponents)
  const uncovered: string[] = []
  for (const file of uiFiles().filter((name) => name.endsWith('.tsx'))) {
    if (NON_TEXT_BEARING_FILES.includes(file)) continue
    const moduleExports = require(resolve(UI_DIR, file)) as Record<string, unknown>
    const components = Object.values(moduleExports).filter((value) => typeof value === 'function')
    if (!components.some((component) => registered.has(component))) uncovered.push(file)
  }
  return uncovered
}
