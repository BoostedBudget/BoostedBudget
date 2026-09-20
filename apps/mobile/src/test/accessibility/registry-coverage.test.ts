import { PRIMITIVE_REGISTRY } from '../../components/ui'
import {
  NON_TEXT_BEARING_FILES,
  findUncoveredComponentFiles,
  uiFiles,
} from '../primitives/registry-coverage'
import { PRIMITIVE_CASES, registryComponents } from './helpers'

/**
 * Registry coverage for the accessibility suite.
 *
 * ⚠ The coverage/dedup assertion itself lives in `src/test/primitives/registry.test.ts` (Todo 27);
 * this suite REUSES the shared logic from `../primitives/registry-coverage` rather than duplicating
 * the exemption list. Adding a component file under `components/ui/` without a registry entry fails
 * here too, so the accessibility suite cannot pass by omission.
 */
describe('accessibility: registry coverage', () => {
  it('fails when a component file appears without a registry entry', () => {
    const registered = PRIMITIVE_REGISTRY.map((entry) => entry.component)
    expect(findUncoveredComponentFiles(registered)).toEqual([])
  })

  it('keeps the non-text-bearing exemption list exact', () => {
    const files = uiFiles()
    for (const file of NON_TEXT_BEARING_FILES) expect(files).toContain(file)
    expect(NON_TEXT_BEARING_FILES.length).toBeGreaterThan(0)
  })

  it('enumerates every registry component in the primitive table', () => {
    const covered = new Set(PRIMITIVE_CASES.map((item) => item.component))
    const missing = registryComponents().filter((component) => !covered.has(component))
    expect(missing).toEqual([])
  })

  it('does not let a missing primitive pass by omission (mutant shape)', () => {
    // A deliberately incomplete registry must be reported as uncovered — proving the helper is
    // load-bearing rather than returning an empty array unconditionally.
    const incomplete = PRIMITIVE_REGISTRY.slice(0, 1).map((entry) => entry.component)
    expect(findUncoveredComponentFiles(incomplete).length).toBeGreaterThan(0)
  })
})
