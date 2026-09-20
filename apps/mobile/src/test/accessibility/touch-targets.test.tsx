import { afterEach } from '@jest/globals'
import { screen } from '@testing-library/react-native'
import { StyleSheet } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { PRIMITIVE_CASES, renderCase } from './helpers'

afterEach(() => {
  jest.restoreAllMocks()
})

const TOUCH_MIN = nativeTokens.light.touchTarget.min

/**
 * Criterion 7 (#1656): touch targets. Every interactive primitive resolves a `minHeight` of at
 * least 44px; icon-only / square affordances also resolve a 44px `minWidth`. The floor is an
 * explicit `minHeight`/`minWidth`, never padding, so a zero-content control still meets it.
 */
describe('accessibility: touch targets (criterion 7)', () => {
  const interactive = PRIMITIVE_CASES.filter((item) => item.interactive)

  it('is not vacuous — it enumerates a substantial interactive set', () => {
    expect(interactive.length).toBeGreaterThanOrEqual(20)
    expect(TOUCH_MIN).toBeGreaterThanOrEqual(44)
  })

  for (const item of interactive) {
    it(`${item.name} meets the ${TOUCH_MIN}px touch floor`, async () => {
      await renderCase(item)
      const node = item.query ? item.query() : screen.getByTestId(item.testID as string)
      const style = StyleSheet.flatten(node.props.style) as Record<string, unknown>

      expect({ case: item.name, minHeight: style.minHeight }).toEqual({
        case: item.name,
        minHeight: expect.any(Number),
      })
      expect(style.minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)

      if (item.requiresMinWidth) {
        expect({ case: item.name, minWidth: style.minWidth }).toEqual({
          case: item.name,
          minWidth: expect.any(Number),
        })
        expect(style.minWidth as number).toBeGreaterThanOrEqual(TOUCH_MIN)
      }
    })
  }
})
