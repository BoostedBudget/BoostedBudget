import { afterEach } from '@jest/globals'
import { screen, within } from '@testing-library/react-native'
import {
  PRIMITIVE_CASES,
  renderCase,
  registryInteractiveComponents,
  type PrimitiveCase,
} from './helpers'

afterEach(() => {
  jest.restoreAllMocks()
})

function nodeFor(item: PrimitiveCase) {
  return item.query ? item.query() : screen.getByTestId(item.testID as string)
}

/**
 * Criterion 1 (#1656): semantic roles and accessible names on every interactive primitive.
 *
 * The table is the enumeration. The first test cross-checks it against `PRIMITIVE_REGISTRY`'s
 * `interactive` entries, so a new interactive registry entry that is not in the table fails rather
 * than passing by omission.
 */
describe('accessibility: semantic roles and accessible names (criterion 1)', () => {
  const interactive = PRIMITIVE_CASES.filter((item) => item.interactive)

  it('covers every component the registry marks interactive', () => {
    const covered = new Set(interactive.map((item) => item.component))
    const missing = registryInteractiveComponents().filter((component) => !covered.has(component))
    expect(missing).toEqual([])
  })

  it('is not vacuous — it enumerates a substantial interactive set', () => {
    expect(interactive.length).toBeGreaterThanOrEqual(20)
    expect(registryInteractiveComponents().length).toBeGreaterThan(0)
  })

  for (const item of interactive) {
    it(`${item.name} exposes a semantic role`, async () => {
      await renderCase(item)
      const node = nodeFor(item)
      if (item.role) {
        expect(node.props.accessibilityRole).toBe(item.role)
      } else {
        // A text input relies on its implicit native role; pin the host component instead.
        expect(node.type).toBe(item.implicitHostType)
      }
    })

    it(`${item.name} exposes an accessible name`, async () => {
      await renderCase(item)
      const node = nodeFor(item)
      if (item.accessibleName) {
        expect(node.props.accessibilityLabel).toBe(item.accessibleName)
      } else if (item.childText) {
        // The name is derived from the visible text content, which is a valid accessible name.
        expect(within(node).getByText(item.childText)).toBeTruthy()
      } else {
        throw new Error(`${item.name} declares no accessible-name contract`)
      }
    })
  }
})
