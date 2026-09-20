import { afterEach, beforeEach } from '@jest/globals'
import { render, screen, fireEvent, act } from '@testing-library/react-native'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { AccessibilityInfo, Animated, StyleSheet, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Alert, type AlertVariant } from '../../components/ui/Alert'
import {
  Spinner,
  SPINNER_NORMAL_DURATION_MS,
  SPINNER_REDUCED_DURATION_MS,
  shouldUseNativeDriver,
} from '../../components/ui/Spinner'
import { ProgressBar } from '../../components/ui/ProgressBar'
import { Skeleton } from '../../components/ui/Skeleton'
import { FEEDBACK_REGISTRY } from '../../components/ui/feedback.registry'
import { setColorScheme } from '../color-scheme'
import { COLOR_SCHEMES, resolvedColor, runtimeTokenColors } from './helpers'

const ALERT_VARIANTS: readonly AlertVariant[] = ['info', 'success', 'warning', 'danger']

const ALERT_CUES: Record<AlertVariant, string> = {
  info: 'Info',
  success: 'Success',
  warning: 'Warning',
  danger: 'Error',
}

function fakeAnimation() {
  return { start: jest.fn(), stop: jest.fn(), reset: jest.fn() } as never
}

/** Capture the durations the primitives hand to `Animated.timing` without running real timers. */
function spyAnimations() {
  const timing = jest.spyOn(Animated, 'timing').mockImplementation(() => fakeAnimation())
  jest.spyOn(Animated, 'loop').mockImplementation(() => fakeAnimation())
  jest.spyOn(Animated, 'sequence').mockImplementation(() => fakeAnimation())
  return timing
}

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

describe('Alert', () => {
  it('renders role="alert" with an assertive live region for every variant', () => {
    render(
      <View>
        {ALERT_VARIANTS.map((variant) => (
          <Alert key={variant} variant={variant} testID={`alert-${variant}`}>
            body
          </Alert>
        ))}
      </View>,
    )

    for (const variant of ALERT_VARIANTS) {
      const node = screen.getByTestId(`alert-${variant}`)
      expect(node.props.accessibilityRole).toBe('alert')
      expect(node.props.accessibilityLiveRegion).toBe('assertive')
    }
  })

  it('carries a non-colour severity cue for every variant', () => {
    render(
      <View>
        {ALERT_VARIANTS.map((variant) => (
          <Alert key={variant} variant={variant}>
            body
          </Alert>
        ))}
      </View>,
    )

    for (const variant of ALERT_VARIANTS) {
      expect(screen.getByText(ALERT_CUES[variant])).toBeTruthy()
    }
  })

  it.each(COLOR_SCHEMES)('fills info with bb-info-strong and danger with bb-danger-strong (%s)', (scheme) => {
    // `runtimeTokenColors()` sweeps both schemes and leaves the global scheme on dark, so read it
    // before pinning the scheme under test.
    const runtime = runtimeTokenColors()[scheme]
    setColorScheme(scheme)
    render(
      <View>
        <Alert variant="info" testID="alert-info">
          body
        </Alert>
        <Alert variant="danger" testID="alert-danger">
          body
        </Alert>
      </View>,
    )

    expect(resolvedColor('alert-info', 'backgroundColor')).toBe(runtime['bb-info-strong'])
    expect(resolvedColor('alert-danger', 'backgroundColor')).toBe(runtime['bb-danger-strong'])
  })

  it('renders an optional title', () => {
    render(
      <Alert variant="success" title="Saved">
        Body
      </Alert>,
    )

    expect(screen.getByText('Saved')).toBeTruthy()
  })

  it('renders a dismiss control with an accessible name and invokes onDismiss', () => {
    const onDismiss = jest.fn()
    render(
      <Alert variant="info" onDismiss={onDismiss}>
        body
      </Alert>,
    )

    fireEvent.press(screen.getByLabelText('Dismiss'))
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })
})

describe('Spinner', () => {
  it('requires a label at the type level', () => {
    // @ts-expect-error `label` is required — a Spinner without an accessible name must not compile.
    const element = <Spinner />
    expect(element).toBeDefined()
  })

  it('exposes the label as its accessible name', () => {
    render(<Spinner label="Loading accounts" testID="spinner" />)
    expect(screen.getByTestId('spinner').props.accessibilityLabel).toBe('Loading accounts')
  })

  it('spins at the normal duration when reduce motion is off', async () => {
    const flushMotion = mockReducedMotion(false)
    const timing = spyAnimations()
    render(<Spinner label="Loading" />)
    await flushMotion()

    expect(timing).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ duration: SPINNER_NORMAL_DURATION_MS }),
    )
  })

  it('slows to the reduced-motion duration when reduce motion is on', async () => {
    const flushMotion = mockReducedMotion(true)
    const timing = spyAnimations()
    render(<Spinner label="Loading" />)
    await flushMotion()

    expect(timing).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ duration: SPINNER_REDUCED_DURATION_MS }),
    )
    // Slowed, never frozen: the reduced duration is longer than the normal one.
    expect(SPINNER_REDUCED_DURATION_MS).toBeGreaterThan(SPINNER_NORMAL_DURATION_MS)
  })
})

describe('ProgressBar', () => {
  it('exposes the progressbar role and a numeric accessibilityValue', () => {
    render(<ProgressBar value={0.42} testID="progress" />)
    const node = screen.getByTestId('progress')
    expect(node.props.accessibilityRole).toBe('progressbar')
    expect(node.props.accessibilityValue).toEqual({ min: 0, max: 100, now: 42 })
  })

  it('exposes an indeterminate accessibilityValue without a numeric now', () => {
    render(<ProgressBar indeterminate testID="progress" />)
    const node = screen.getByTestId('progress')
    expect(node.props.accessibilityRole).toBe('progressbar')
    expect(node.props.accessibilityValue).toBeDefined()
    expect(node.props.accessibilityValue.now).toBeUndefined()
  })

  it('clamps value into the 0..1 range', () => {
    render(
      <View>
        <ProgressBar value={2} testID="progress-high" />
        <ProgressBar value={-1} testID="progress-low" />
      </View>,
    )

    expect(screen.getByTestId('progress-high').props.accessibilityValue.now).toBe(100)
    expect(screen.getByTestId('progress-low').props.accessibilityValue.now).toBe(0)
  })
})

describe('Skeleton', () => {
  it('is hidden from the accessibility tree', () => {
    render(<Skeleton testID="skeleton" />)
    // The element is intentionally hidden from the a11y tree, so the query must opt in.
    const node = screen.getByTestId('skeleton', { includeHiddenElements: true })
    expect(node.props.accessibilityElementsHidden).toBe(true)
    expect(node.props.importantForAccessibility).toBe('no-hide-descendants')
  })

  it('applies width, height and radius', () => {
    render(<Skeleton testID="skeleton" width={120} height={16} radius="full" />)
    const node = screen.getByTestId('skeleton', { includeHiddenElements: true })
    const style = StyleSheet.flatten(node.props.style) as Record<string, unknown>
    expect(style.width).toBe(120)
    expect(style.height).toBe(16)
    expect(style.borderRadius as number).toBeGreaterThan(1000)
  })

  it('slows its pulse to the reduced-motion duration when reduce motion is on', async () => {
    const flushMotion = mockReducedMotion(true)
    const timing = spyAnimations()
    render(<Skeleton testID="skeleton" />)
    await flushMotion()

    expect(timing).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ duration: SPINNER_REDUCED_DURATION_MS }),
    )
  })
})

describe('native-driver guard', () => {
  it('uses the JS driver on web and the native driver elsewhere', () => {
    expect(shouldUseNativeDriver('web')).toBe(false)
    expect(shouldUseNativeDriver('ios')).toBe(true)
    expect(shouldUseNativeDriver('android')).toBe(true)
  })

  it('is wired into every animated feedback primitive', () => {
    const uiDir = resolve(dirname(require.resolve('../../../package.json')), 'src/components/ui')
    for (const file of ['Spinner.tsx', 'Skeleton.tsx', 'ToastProvider.tsx']) {
      expect({ file, source: readFileSync(resolve(uiDir, file), 'utf8') }).toEqual({
        file,
        source: expect.stringContaining('shouldUseNativeDriver(Platform.OS)'),
      })
    }
  })
})

describe('FEEDBACK_REGISTRY', () => {
  it('has exactly one entry per Alert variant', () => {
    expect(FEEDBACK_REGISTRY).toHaveLength(ALERT_VARIANTS.length)
    const names = FEEDBACK_REGISTRY.map((entry) => entry.name)
    for (const variant of ALERT_VARIANTS) expect(names).toContain(`Alert.${variant}`)
  })

  it('pins info and danger to the contrast remediation tokens', () => {
    expect(FEEDBACK_REGISTRY.find((entry) => entry.name === 'Alert.info')).toMatchObject({
      foregroundToken: 'info-foreground',
      backgroundToken: 'bb-info-strong',
      ratioClass: 'body',
    })
    expect(FEEDBACK_REGISTRY.find((entry) => entry.name === 'Alert.danger')).toMatchObject({
      foregroundToken: 'destructive-foreground',
      backgroundToken: 'bb-danger-strong',
      ratioClass: 'body',
    })
  })

  it('resolves every foreground and background token in both themes', () => {
    for (const entry of FEEDBACK_REGISTRY) {
      for (const scheme of COLOR_SCHEMES) {
        const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>
        expect(section[entry.foregroundToken]).toBeDefined()
        expect(section[entry.backgroundToken]).toBeDefined()
      }
    }
  })
})