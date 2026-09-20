import { afterEach, beforeEach } from '@jest/globals'
import { act, render, screen } from '@testing-library/react-native'
import { Dimensions, StyleSheet, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { ThemeProvider } from '../../theme/ThemeProvider'
import { Text } from '../../components/ui/Text'
import { DIMENSION_PROFILES, PRIMITIVE_CASES, collectStyles } from './helpers'

const TOUCH_MIN = nativeTokens.light.touchTarget.min

beforeEach(() => {
  jest.spyOn(Dimensions, 'get')
})

afterEach(() => {
  jest.restoreAllMocks()
})

function mockViewport(width: number, height: number) {
  ;(Dimensions.get as jest.Mock).mockReturnValue({ width, height, scale: 2, fontScale: 1 })
}

async function renderRoots() {
  render(
    <ThemeProvider>
      <View>
        {PRIMITIVE_CASES.filter((item) => item.kind === 'root').map((item) => (
          <View key={item.name}>{item.render()}</View>
        ))}
      </View>
    </ThemeProvider>,
  )
  await act(async () => {})
}

/**
 * Criterion 9 (#1656): representative native dimensions.
 *
 * ⚠ jest/react-test-renderer has NO layout engine, so "content is clipped" and "a layout primitive
 * overflows its parent" CANNOT be asserted here — those live in Todo 22's real browser. This suite
 * asserts STYLE CONSTRAINTS only, and `device-checks.md` records the non-enforceable checks.
 */
describe('accessibility: representative dimensions (criterion 9)', () => {
  for (const profile of DIMENSION_PROFILES) {
    it(`keeps every fixed dimension within the ${profile.name} viewport (${profile.width}x${profile.height})`, async () => {
      mockViewport(profile.width, profile.height)
      await renderRoots()

      const violations: string[] = []
      for (const style of collectStyles()) {
        checkAtMost(style.width, profile.width, 'width', violations)
        checkAtMost(style.maxWidth, profile.width, 'maxWidth', violations)
        checkAtMost(style.height, profile.height, 'height', violations)
        checkAtMost(style.maxHeight, profile.height, 'maxHeight', violations)
        checkAtLeast(style.minHeight, TOUCH_MIN, 'minHeight', violations)
        checkAtLeast(style.minWidth, TOUCH_MIN, 'minWidth', violations)
      }
      expect(violations).toEqual([])
    })
  }

  it('renders the modal-bearing cases so the walk is not vacuous', async () => {
    mockViewport(320, 568)
    await renderRoots()
    expect(screen.getByTestId('c-dialog-surface')).toBeTruthy()
    expect(screen.getByTestId('c-action-sheet-surface')).toBeTruthy()
  })

  it('keeps text primitives free of a hard pixel width and forwards numberOfLines', () => {
    render(<Text testID="dim-text" numberOfLines={2}>A very long line of copy</Text>)
    const node = screen.getByTestId('dim-text')
    expect(node.props.numberOfLines).toBe(2)
    const style = StyleSheet.flatten(node.props.style) as Record<string, unknown>
    expect(style.width).toBeUndefined()
    expect(style.maxWidth).toBeUndefined()
    expect(style.height).toBeUndefined()
  })

  it('does not let a layout primitive hard-cap text growth', () => {
    const layoutNames = ['Box', 'Stack', 'Inline', 'Surface']
    const layoutCases = PRIMITIVE_CASES.filter((item) => layoutNames.includes(item.name))
    expect(layoutCases).toHaveLength(layoutNames.length)
    for (const item of layoutCases) {
      render(item.render())
      const style = (StyleSheet.flatten(screen.getByTestId(item.testID as string).props.style) ??
        {}) as Record<string, unknown>
      expect({ name: item.name, height: style.height }).toEqual({ name: item.name, height: undefined })
      expect({ name: item.name, maxHeight: style.maxHeight }).toEqual({ name: item.name, maxHeight: undefined })
    }
  })
})

function checkAtMost(value: unknown, limit: number, prop: string, violations: string[]): void {
  if (typeof value === 'number' && value > limit) violations.push(`${prop}=${value} > ${limit}`)
}

function checkAtLeast(value: unknown, floor: number, prop: string, violations: string[]): void {
  if (typeof value === 'number' && value < floor) violations.push(`${prop}=${value} < ${floor}`)
}
