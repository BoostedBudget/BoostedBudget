import { afterEach, beforeEach } from '@jest/globals'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { act, render, screen } from '@testing-library/react-native'
import { AccessibilityInfo } from 'react-native'
import GalleryScreen from '../../app/gallery'
import { PRIMITIVE_REGISTRY } from '../components/ui'
import { ThemeProvider as ThemePreferenceProvider } from '../theme/ThemeProvider'
import { COLOR_SCHEMES, normalizeHex, resolvedColor } from './primitives/helpers'

const MOBILE_ROOT = dirname(require.resolve('../../package.json'))

function readMobileFile(relativePath: string): string {
  return readFileSync(resolve(MOBILE_ROOT, relativePath), 'utf8')
}

async function renderGallery() {
  render(
    <ThemePreferenceProvider>
      <GalleryScreen />
    </ThemePreferenceProvider>,
  )
  // Flush the async theme-preference hydration so no update lands outside `act`.
  await act(async () => {})
}

beforeEach(() => {
  // A controlled reduced-motion read so `Spinner`/`Sheet` never leak an out-of-act state update.
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(false)
  jest.spyOn(AccessibilityInfo, 'addEventListener').mockReturnValue({ remove: jest.fn() } as never)
})

afterEach(() => {
  jest.restoreAllMocks()
})

describe('primitive gallery route', () => {
  it('renders one labelled instance of every registry entry in both theme sections', async () => {
    await renderGallery()

    for (const theme of COLOR_SCHEMES) {
      for (const entry of PRIMITIVE_REGISTRY) {
        expect(screen.getByTestId(`gallery-${theme}-entry-${entry.name}`)).toBeOnTheScreen()
      }
      // The count is derived from the registry, so the gallery cannot silently fall behind it.
      expect(screen.getAllByTestId(new RegExp(`^gallery-${theme}-entry-`))).toHaveLength(
        PRIMITIVE_REGISTRY.length,
      )
    }
  })

  it('renders a genuinely light section and a genuinely dark section', async () => {
    await renderGallery()

    expect(resolvedColor('gallery-light-node-Text.default', 'color')).toBe(
      normalizeHex(nativeTokens.light.color.foreground),
    )
    expect(resolvedColor('gallery-dark-node-Text.default', 'color')).toBe(
      normalizeHex(nativeTokens.dark.color.foreground),
    )
  })

  it('registers the gallery route in the app Stack', () => {
    expect(readMobileFile('app/_layout.tsx')).toMatch(/name="gallery"/)
  })

  it('is fixture-only: no environment reads and no network calls', () => {
    const source = readMobileFile('app/gallery.tsx')
    expect(source).not.toMatch(/EXPO_PUBLIC/)
    expect(source).not.toMatch(/fetch\(/)
  })

  it('is not linked from the production home screen', () => {
    expect(readMobileFile('app/index.tsx')).not.toMatch(/gallery/i)
  })
})
