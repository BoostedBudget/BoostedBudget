import { afterEach, beforeEach } from '@jest/globals'
import { act, fireEvent, render, screen, within } from '@testing-library/react-native'
import type { ReactElement } from 'react'
import { AccessibilityInfo, Dimensions } from 'react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { ActionSheet, type ActionSheetAction } from '../../components/ui/ActionSheet'
import { BottomSheet } from '../../components/ui/BottomSheet'
import { SHEET_REGISTRY } from '../../components/ui/sheet.registry'
import { Text } from '../../components/ui/Text'
import { setColorScheme } from '../color-scheme'
import { COLOR_SCHEMES, resolvedColor, resolvedStyle, runtimeTokenColors } from './helpers'

const TOUCH_MIN = nativeTokens.light.touchTarget.min
const BASE_PADDING = nativeTokens.light.spacing['spacing-bb-5']
const INSETS = { top: 47, right: 0, bottom: 34, left: 0 }
const FRAME = { x: 0, y: 0, width: 390, height: 844 }

/**
 * The focus TRAP and focus RESTORE are browser behaviours owned by react-native-web's
 * `ModalFocusTrap` and are asserted in Todo 22(e), not here — jest has no DOM and RN `Modal` does
 * not implement web focus management. This suite pins the mechanisms that produce them: the modal
 * region, the DOM order (cancel last), and the non-focusable backdrop.
 */
function mockReducedMotion(enabled: boolean) {
  let resolveMotion: (value: boolean) => void = () => {}
  const pending = new Promise<boolean>((resolve) => {
    resolveMotion = resolve
  })
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockReturnValue(pending)
  jest.spyOn(AccessibilityInfo, 'addEventListener').mockReturnValue({ remove: jest.fn() } as never)
  // Resolving inside `act` keeps the hook's async state update within the test's act scope.
  return async () => {
    await act(async () => {
      resolveMotion(enabled)
    })
  }
}

beforeEach(() => {
  // Every test gets a controlled reduced-motion read so no real async resolution leaks an
  // out-of-act state update into a later test.
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(false)
  jest.spyOn(AccessibilityInfo, 'addEventListener').mockReturnValue({ remove: jest.fn() } as never)
})

afterEach(() => {
  jest.restoreAllMocks()
})

const ACTIONS: ActionSheetAction[] = [
  { label: 'Edit', onPress: () => {} },
  { label: 'Duplicate', onPress: () => {} },
  { label: 'Delete', onPress: () => {}, destructive: true },
]

function renderWithInsets(node: ReactElement) {
  return render(
    <SafeAreaProvider initialMetrics={{ frame: FRAME, insets: INSETS }}>{node}</SafeAreaProvider>,
  )
}

describe('ActionSheet', () => {
  it('renders nothing while closed', () => {
    render(<ActionSheet visible={false} title="Options" actions={ACTIONS} onCancel={() => {}} />)

    expect(screen.queryByTestId('action-sheet-overlay')).toBeNull()
    expect(screen.queryByTestId('action-sheet-surface')).toBeNull()
  })

  it('removes the sheet when visible flips to false (no stale sheet)', () => {
    const { rerender } = render(
      <ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />,
    )
    expect(screen.getByTestId('action-sheet-overlay')).toBeTruthy()

    rerender(<ActionSheet visible={false} title="Options" actions={ACTIONS} onCancel={() => {}} />)
    expect(screen.queryByTestId('action-sheet-overlay')).toBeNull()
  })

  it('announces the sheet as a modal region with the dialog role', () => {
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    expect(screen.getByTestId('action-sheet-overlay').props.accessibilityViewIsModal).toBe(true)

    const surface = screen.getByTestId('action-sheet-surface')
    expect(surface.props.accessibilityViewIsModal).toBe(true)
    expect(surface.props.role).toBe('dialog')
    // RN 0.86's legacy `accessibilityRole` union omits `dialog`; the native shim still carries it.
    expect(surface.props.accessibilityRole).toBe('dialog')
  })

  it('renders the title on the sheet surface', () => {
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    expect(screen.getByText('Options')).toBeTruthy()
  })

  it('exposes every action as a button with an accessible name', () => {
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    const labels = screen.getAllByRole('button').map((node) => node.props.accessibilityLabel)
    expect(labels).toEqual(['Edit', 'Duplicate', 'Delete', 'Cancel'])
  })

  it('meets the touch floor on every action and the cancel action', () => {
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    for (const id of [
      'action-sheet-action-0',
      'action-sheet-action-1',
      'action-sheet-action-2',
      'action-sheet-cancel',
    ]) {
      expect(resolvedStyle(id).minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
    }
  })

  it('places the cancel action last and outside the scrollable action list', () => {
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    const scrollRegion = screen.getByTestId('action-sheet-actions')
    expect(within(scrollRegion).getByTestId('action-sheet-action-0')).toBeTruthy()
    expect(within(scrollRegion).queryByTestId('action-sheet-cancel')).toBeNull()

    const cancelRegion = screen.getByTestId('action-sheet-cancel-region')
    expect(within(cancelRegion).getByTestId('action-sheet-cancel')).toBeTruthy()

    const labels = screen.getAllByRole('button').map((node) => node.props.accessibilityLabel)
    expect(labels[labels.length - 1]).toBe('Cancel')
  })

  it('gives a destructive action the danger token and a visible text label, never colour alone', () => {
    const runtime = runtimeTokenColors().light
    setColorScheme('light')
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    expect(resolvedColor('action-sheet-action-2', 'backgroundColor')).toBe(runtime['bb-danger-strong'])
    // The non-colour cue is the visible text label, which is also the accessible name.
    expect(screen.getByText('Delete')).toBeTruthy()
    expect(screen.getByTestId('action-sheet-action-2').props.accessibilityLabel).toBe('Delete')
  })

  it('invokes onCancel from onRequestClose — Android hardware back and web Escape', () => {
    const onCancel = jest.fn()
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={onCancel} />)

    // react-native-web's Modal routes Escape to `onRequestClose` (ModalContent.js), and Android's
    // hardware back calls it natively, so both share this single close path.
    screen.getByTestId('action-sheet').props.onRequestClose()
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('invokes an action handler when pressed', () => {
    const onPress = jest.fn()
    render(
      <ActionSheet
        visible
        title="Options"
        actions={[{ label: 'Edit', onPress }]}
        onCancel={() => {}}
      />,
    )

    fireEvent.press(screen.getByTestId('action-sheet-action-0'))
    expect(onPress).toHaveBeenCalledTimes(1)
  })

  it('disables an action without invoking its handler', () => {
    const onPress = jest.fn()
    render(
      <ActionSheet
        visible
        title="Options"
        actions={[{ label: 'Edit', onPress, disabled: true }]}
        onCancel={() => {}}
      />,
    )

    fireEvent.press(screen.getByTestId('action-sheet-action-0'))
    expect(onPress).not.toHaveBeenCalled()
  })

  it('keeps the backdrop out of the focus order', () => {
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    // The backdrop is hidden from the accessibility tree, so it needs the hidden-element opt-in.
    const backdrop = screen.getByTestId('action-sheet-backdrop', { includeHiddenElements: true })
    expect(backdrop.props.accessible).toBe(false)
    expect(backdrop.props.accessibilityRole).toBeUndefined()
  })

  it('closes on a backdrop tap', () => {
    const onCancel = jest.fn()
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={onCancel} />)

    fireEvent(
      screen.getByTestId('action-sheet-backdrop', { includeHiddenElements: true }),
      'responderRelease',
    )
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('applies the safe-area insets to the sheet surface', () => {
    renderWithInsets(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    expect(resolvedStyle('action-sheet-surface').paddingBottom).toBe(INSETS.bottom + BASE_PADDING)
  })

  it('disables the slide animation under reduced motion', async () => {
    const resolve = mockReducedMotion(true)
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    await resolve()
    expect(screen.getByTestId('action-sheet').props.animationType).toBe('none')
  })

  it('slides in when motion is allowed', () => {
    render(<ActionSheet visible title="Options" actions={ACTIONS} onCancel={() => {}} />)

    expect(screen.getByTestId('action-sheet').props.animationType).toBe('slide')
  })
})

describe('BottomSheet', () => {
  it('renders nothing while closed', () => {
    render(
      <BottomSheet visible={false} onClose={() => {}}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    expect(screen.queryByTestId('bottom-sheet-overlay')).toBeNull()
  })

  it('announces the sheet as a modal region with the dialog role', () => {
    render(
      <BottomSheet visible onClose={() => {}}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    expect(screen.getByTestId('bottom-sheet-overlay').props.accessibilityViewIsModal).toBe(true)

    const surface = screen.getByTestId('bottom-sheet-surface')
    expect(surface.props.accessibilityViewIsModal).toBe(true)
    expect(surface.props.role).toBe('dialog')
    expect(surface.props.accessibilityRole).toBe('dialog')
  })

  it('renders its children', () => {
    render(
      <BottomSheet visible onClose={() => {}}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    expect(screen.getByText('Body')).toBeTruthy()
  })

  it('invokes onClose from onRequestClose — Android hardware back and web Escape', () => {
    const onClose = jest.fn()
    render(
      <BottomSheet visible onClose={onClose}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    screen.getByTestId('bottom-sheet').props.onRequestClose()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on a backdrop tap', () => {
    const onClose = jest.fn()
    render(
      <BottomSheet visible onClose={onClose}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    fireEvent(
      screen.getByTestId('bottom-sheet-backdrop', { includeHiddenElements: true }),
      'responderRelease',
    )
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('applies the safe-area insets to the sheet surface', () => {
    renderWithInsets(
      <BottomSheet visible onClose={() => {}}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    expect(resolvedStyle('bottom-sheet-surface').paddingBottom).toBe(INSETS.bottom + BASE_PADDING)
  })

  it('caps the sheet height at the first snap point', () => {
    const height = Dimensions.get('window').height
    render(
      <BottomSheet visible onClose={() => {}} snapPoints={[0.5]}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    expect(resolvedStyle('bottom-sheet-surface').maxHeight).toBe(0.5 * height)
  })

  it('never lets the sheet exceed the safe area', () => {
    const height = Dimensions.get('window').height
    renderWithInsets(
      <BottomSheet visible onClose={() => {}} snapPoints={[1]}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    expect(resolvedStyle('bottom-sheet-surface').maxHeight).toBe(height - INSETS.top)
  })

  it('disables the slide animation under reduced motion', async () => {
    const resolve = mockReducedMotion(true)
    render(
      <BottomSheet visible onClose={() => {}}>
        <Text>Body</Text>
      </BottomSheet>,
    )

    await resolve()
    expect(screen.getByTestId('bottom-sheet').props.animationType).toBe('none')
  })
})

describe('SHEET_REGISTRY', () => {
  it('has one entry per text-bearing sheet role', () => {
    const names = SHEET_REGISTRY.map((entry) => entry.name)
    for (const name of [
      'ActionSheet.title',
      'ActionSheet.action',
      'ActionSheet.actionDestructive',
      'ActionSheet.cancel',
    ]) {
      expect(names).toContain(name)
    }
  })

  it('pins the destructive action to destructive-foreground on bb-danger-strong', () => {
    const entry = SHEET_REGISTRY.find((item) => item.name === 'ActionSheet.actionDestructive')
    expect(entry).toMatchObject({
      foregroundToken: 'destructive-foreground',
      backgroundToken: 'bb-danger-strong',
      ratioClass: 'body',
      interactive: true,
    })
  })

  it('resolves every foreground/background token in both themes', () => {
    for (const entry of SHEET_REGISTRY) {
      for (const scheme of COLOR_SCHEMES) {
        const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<
          string,
          string
        >
        expect(section[entry.foregroundToken]).toBeDefined()
        expect(section[entry.backgroundToken]).toBeDefined()
      }
    }
  })
})

describe('ActionSheet types', () => {
  it('requires a text label on every action', () => {
    // @ts-expect-error a destructive action without a text label must not compile
    const action: ActionSheetAction = { onPress: () => {}, destructive: true }
    expect(action).toBeDefined()
  })
})