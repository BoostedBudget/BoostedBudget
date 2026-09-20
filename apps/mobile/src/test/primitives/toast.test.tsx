import { afterEach, beforeEach } from '@jest/globals'
import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { AccessibilityInfo, Animated, StyleSheet, View } from 'react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Toast, type ToastVariant } from '../../components/ui/Toast'
import { ToastProvider, useToast, type ToastOptions } from '../../components/ui/ToastProvider'
import { TOAST_REGISTRY } from '../../components/ui/toast.registry'
import { COLOR_SCHEMES } from './helpers'

const VARIANTS: readonly ToastVariant[] = ['info', 'success', 'warning', 'danger']

const CUES: Record<ToastVariant, string> = {
  info: 'Info',
  success: 'Success',
  warning: 'Warning',
  danger: 'Error',
}

const TOUCH_MIN = nativeTokens.light.touchTarget.min

const INSETS = { top: 47, bottom: 34, left: 0, right: 0 }
const FRAME = { x: 0, y: 0, width: 390, height: 844 }

function withSafeArea(children: React.ReactNode) {
  return (
    <SafeAreaProvider initialMetrics={{ frame: FRAME, insets: INSETS }}>{children}</SafeAreaProvider>
  )
}

/**
 * `Animated.timing(...).start(callback)` is mocked so the exit callback fires synchronously. The
 * real native driver never completes under jest, so without this the toast would never leave the
 * queue and the dismissal assertions would be untestable.
 */
function spyAnimations() {
  const timing = jest.spyOn(Animated, 'timing').mockImplementation(
    () =>
      ({
        start: (callback?: (result: { finished: boolean }) => void) => callback?.({ finished: true }),
        stop: jest.fn(),
        reset: jest.fn(),
      }) as never,
  )
  jest.spyOn(Animated, 'loop').mockImplementation(
    () => ({ start: jest.fn(), stop: jest.fn(), reset: jest.fn() }) as never,
  )
  jest.spyOn(Animated, 'sequence').mockImplementation(
    () => ({ start: jest.fn(), stop: jest.fn(), reset: jest.fn() }) as never,
  )
  return timing
}

function mockReducedMotion(enabled: boolean) {
  let resolveMotion: (value: boolean) => void = () => {}
  const pending = new Promise<boolean>((resolve) => {
    resolveMotion = resolve
  })
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockReturnValue(pending)
  return async () => {
    await act(async () => {
      resolveMotion(enabled)
    })
  }
}

function mockScreenReader(enabled: boolean) {
  jest.spyOn(AccessibilityInfo, 'isScreenReaderEnabled').mockResolvedValue(enabled)
}

/** Captures the context `show` so a test can enqueue toasts imperatively. */
function ToastCapture({ onReady }: { onReady: (show: (options: ToastOptions) => number) => void }) {
  const { show } = useToast()
  onReady(show)
  return null
}

function renderProvider(options: { screenReader?: boolean } = {}) {
  // The mock must be installed before render: the provider reads the screen-reader state in a
  // mount effect.
  if (options.screenReader !== undefined) mockScreenReader(options.screenReader)
  let show: ((options: ToastOptions) => number) | undefined
  const utils = render(
    withSafeArea(
      <ToastProvider>
        <ToastCapture onReady={(fn) => (show = fn)} />
      </ToastProvider>,
    ),
  )
  return { ...utils, show: (options: ToastOptions) => show!(options) }
}

beforeEach(() => {
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(false)
  jest.spyOn(AccessibilityInfo, 'isScreenReaderEnabled').mockResolvedValue(false)
  jest.spyOn(AccessibilityInfo, 'addEventListener').mockReturnValue({ remove: jest.fn() } as never)
})

afterEach(() => {
  jest.restoreAllMocks()
  jest.useRealTimers()
})

describe('Toast', () => {
  it('renders the message with the correct live region per variant', () => {
    render(
      <View>
        {VARIANTS.map((variant) => (
          <Toast key={variant} testID={`toast-${variant}`} variant={variant} message={`${variant} message`} />
        ))}
      </View>,
    )

    for (const variant of VARIANTS) {
      const node = screen.getByTestId(`toast-${variant}`)
      expect(node.props.accessibilityLiveRegion).toBe(variant === 'danger' ? 'assertive' : 'polite')
      expect(screen.getByText(`${variant} message`)).toBeTruthy()
    }
  })

  it('carries a non-colour severity cue for every variant', () => {
    render(
      <View>
        {VARIANTS.map((variant) => (
          <Toast key={variant} variant={variant} message="body" />
        ))}
      </View>,
    )

    for (const variant of VARIANTS) {
      expect(screen.getByText(CUES[variant])).toBeTruthy()
    }
  })

  it('renders a dismiss control that meets the touch floor and has an accessible name', () => {
    const onDismiss = jest.fn()
    render(<Toast variant="info" message="body" onDismiss={onDismiss} />)

    const dismiss = screen.getByLabelText('Dismiss')
    const style = StyleSheet.flatten(dismiss.props.style) as Record<string, unknown>
    expect(style.minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
    expect(style.minWidth as number).toBeGreaterThanOrEqual(TOUCH_MIN)

    fireEvent.press(dismiss)
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it('renders an optional action and invokes it', () => {
    const onPress = jest.fn()
    render(<Toast variant="success" message="Saved" action={{ label: 'Undo', onPress }} />)

    fireEvent.press(screen.getByLabelText('Undo'))
    expect(onPress).toHaveBeenCalledTimes(1)
  })
})

describe('ToastProvider', () => {
  it('renders its children', () => {
    render(
      withSafeArea(
        <ToastProvider>
          <View testID="child" />
        </ToastProvider>,
      ),
    )
    expect(screen.getByTestId('child')).toBeTruthy()
  })

  it('renders a shown message', async () => {
    const { show } = renderProvider()
    await act(async () => {
      show({ message: 'Account linked', variant: 'success' })
    })
    expect(screen.getByText('Account linked')).toBeTruthy()
  })

  it('renders the queue in order and dismisses the oldest first', async () => {
    jest.useFakeTimers()
    spyAnimations()
    const { show } = renderProvider()
    await act(async () => {
      show({ message: 'First', variant: 'info', duration: 1000 })
      show({ message: 'Second', variant: 'info', duration: 5000 })
    })

    expect(screen.getAllByTestId(/^toast-item-/).map((node) => node.props.testID)).toEqual([
      'toast-item-1',
      'toast-item-2',
    ])

    await act(async () => {
      jest.advanceTimersByTime(1000)
    })
    expect(screen.queryByTestId('toast-item-1')).toBeNull()
    expect(screen.getByTestId('toast-item-2')).toBeTruthy()

    await act(async () => {
      jest.advanceTimersByTime(4000)
    })
    expect(screen.queryByTestId('toast-item-2')).toBeNull()
  })

  it('does not auto-dismiss while a screen reader is active', async () => {
    jest.useFakeTimers()
    spyAnimations()
    const { show } = renderProvider({ screenReader: true })
    await act(async () => {
      show({ message: 'Read me', variant: 'info', duration: 1000 })
    })
    // Flush the async screen-reader read so the pause is in effect before time advances.
    await act(async () => {})

    await act(async () => {
      jest.advanceTimersByTime(10000)
    })
    expect(screen.getByText('Read me')).toBeTruthy()
  })

  it('never auto-dismisses a danger toast', async () => {
    jest.useFakeTimers()
    spyAnimations()
    const { show } = renderProvider()
    await act(async () => {
      show({ message: 'Payment failed', variant: 'danger', duration: 1000 })
    })

    await act(async () => {
      jest.advanceTimersByTime(60000)
    })
    expect(screen.getByText('Payment failed')).toBeTruthy()
  })

  it('cancels the pending timer when a toast is dismissed manually', async () => {
    jest.useFakeTimers()
    spyAnimations()
    const { show } = renderProvider()
    await act(async () => {
      show({ message: 'Dismiss me', variant: 'info', duration: 1000 })
    })

    await act(async () => {
      fireEvent.press(screen.getByLabelText('Dismiss'))
    })
    expect(screen.queryByText('Dismiss me')).toBeNull()

    // The cancelled timer must not fire a second dismissal.
    await act(async () => {
      jest.advanceTimersByTime(10000)
    })
    expect(screen.queryByText('Dismiss me')).toBeNull()
  })

  it('disables the enter transition under reduced motion', async () => {
    const flushMotion = mockReducedMotion(true)
    const timing = spyAnimations()
    const { show } = renderProvider()
    await act(async () => {
      show({ message: 'No motion', variant: 'info' })
    })
    await flushMotion()

    const lastCall = timing.mock.calls.at(-1)
    expect(lastCall?.[1]).toEqual(expect.objectContaining({ duration: 0 }))
  })

  it('renders the queue inside the safe area', async () => {
    const { show } = renderProvider()
    await act(async () => {
      show({ message: 'Safe', variant: 'info' })
    })

    const style = StyleSheet.flatten(screen.getByTestId('toast-viewport').props.style) as Record<string, unknown>
    expect(style.paddingTop).toBe(INSETS.top)
    expect(style.paddingBottom).toBe(INSETS.bottom)
  })

  it('throws when useToast is used outside a provider', () => {
    function Probe() {
      useToast()
      return null
    }
    expect(() => render(<Probe />)).toThrow(/ToastProvider/)
  })
})

describe('TOAST_REGISTRY', () => {
  it('has exactly one entry per Toast variant', () => {
    expect(TOAST_REGISTRY).toHaveLength(VARIANTS.length)
    const names = TOAST_REGISTRY.map((entry) => entry.name)
    for (const variant of VARIANTS) expect(names).toContain(`Toast.${variant}`)
  })

  it('pins info and danger to the contrast remediation tokens', () => {
    expect(TOAST_REGISTRY.find((entry) => entry.name === 'Toast.info')).toMatchObject({
      foregroundToken: 'info-foreground',
      backgroundToken: 'bb-info-strong',
      ratioClass: 'body',
    })
    expect(TOAST_REGISTRY.find((entry) => entry.name === 'Toast.danger')).toMatchObject({
      foregroundToken: 'destructive-foreground',
      backgroundToken: 'bb-danger-strong',
      ratioClass: 'body',
    })
  })

  it('resolves every foreground and background token in both themes', () => {
    for (const entry of TOAST_REGISTRY) {
      for (const scheme of COLOR_SCHEMES) {
        const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>
        expect(section[entry.foregroundToken]).toBeDefined()
        expect(section[entry.backgroundToken]).toBeDefined()
      }
    }
  })
})