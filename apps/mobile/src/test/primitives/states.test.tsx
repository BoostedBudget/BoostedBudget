import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fireEvent, render, screen, within } from '@testing-library/react-native'
import { Platform, Pressable, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Button } from '../../components/ui/Button'
import { EmptyState } from '../../components/ui/EmptyState'
import { ErrorState } from '../../components/ui/ErrorState'
import { STATES_REGISTRY } from '../../components/ui/states.registry'
import { setColorScheme } from '../color-scheme'
import {
  COLOR_SCHEMES,
  allowedColors,
  collectHexColors,
  resolvedStyle,
} from './helpers'

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))
const UI_DIR = resolve(MOBILE_ROOT, 'src/components/ui')

// A stack-trace-shaped string: the disclosure must keep it out of the tree until expanded.
const STACK_TRACE_DETAIL = 'Error: boom\n    at Object.<anonymous> (/app/src/api.ts:42:7)'

describe('EmptyState', () => {
  it('renders one grouped accessible summary containing the title and description', () => {
    render(
      <EmptyState testID="empty" title="No accounts" description="Link an account to get started" />,
    )

    const summary = screen.getByTestId('empty-summary')
    expect(summary.props.accessible).toBe(true)
    expect(summary.props.accessibilityRole).toBe('summary')
    // The text lives INSIDE the grouped summary, not as scattered sibling nodes.
    expect(within(summary).getByText('No accounts')).toBeTruthy()
    expect(within(summary).getByText('Link an account to get started')).toBeTruthy()
  })

  it('renders the title and an optional description', () => {
    render(<EmptyState testID="empty" title="No accounts" description="Link one" />)

    expect(screen.getByText('No accounts')).toBeTruthy()
    expect(screen.getByText('Link one')).toBeTruthy()
  })

  it('omits the description when it is not provided', () => {
    render(<EmptyState testID="empty" title="No accounts" />)

    expect(screen.getByText('No accounts')).toBeTruthy()
    expect(screen.queryByTestId('empty-description')).toBeNull()
  })

  it('hides a decorative icon from the accessibility tree', () => {
    render(
      <EmptyState
        testID="empty"
        title="No accounts"
        icon={<View testID="empty-glyph" />}
      />,
    )

    const wrapper = screen.getByTestId('empty-icon', { includeHiddenElements: true })
    expect(wrapper.props.accessibilityElementsHidden).toBe(true)
    expect(wrapper.props.importantForAccessibility).toBe('no-hide-descendants')
  })

  it('keeps the primary action present, reachable and focusable', () => {
    const onPress = jest.fn()
    render(
      <EmptyState
        testID="empty"
        title="No accounts"
        action={<Button testID="empty-action" label="Link account" onPress={onPress} />}
      />,
    )

    // A default query excludes hidden elements, so finding it proves it is not a11y-hidden.
    const action = screen.getByTestId('empty-action')
    expect(action.props.accessibilityRole).toBe('button')

    fireEvent(action, 'focus')
    expect(resolvedStyle('empty-action').outlineWidth).toBe(
      nativeTokens.light.focus['bb-focus-ring-width'],
    )

    fireEvent.press(action)
    expect(onPress).toHaveBeenCalledTimes(1)
  })

  it('renders no action slot when no action is provided', () => {
    render(<EmptyState testID="empty" title="No accounts" />)

    expect(screen.queryByTestId('empty-action')).toBeNull()
  })
})

describe('ErrorState', () => {
  it('renders one grouped accessible summary that announces the error', () => {
    render(
      <ErrorState
        testID="error"
        title="Could not load accounts"
        description="Something went wrong."
        onRetry={() => {}}
      />,
    )

    const summary = screen.getByTestId('error-summary')
    expect(summary.props.accessible).toBe(true)
    expect(summary.props.accessibilityRole).toBe('alert')
    expect(summary.props.accessibilityLiveRegion).toBe('assertive')
    expect(within(summary).getByText('Could not load accounts')).toBeTruthy()
    expect(within(summary).getByText('Something went wrong.')).toBeTruthy()
  })

  it('invokes onRetry from the retry action and never swallows it', () => {
    const onRetry = jest.fn()
    render(
      <ErrorState
        testID="error"
        title="Could not load"
        description="Try again."
        onRetry={onRetry}
      />,
    )

    const retry = screen.getByTestId('error-retry')
    expect(retry.props.accessibilityLabel).toBe('Retry')

    fireEvent.press(retry)
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('uses a custom retry label as the accessible name', () => {
    render(
      <ErrorState
        testID="error"
        title="Could not load"
        description="Try again."
        onRetry={() => {}}
        retryLabel="Reload"
      />,
    )

    expect(screen.getByTestId('error-retry').props.accessibilityLabel).toBe('Reload')
  })

  it('keeps the retry action focusable', () => {
    render(
      <ErrorState
        testID="error"
        title="Could not load"
        description="Try again."
        onRetry={() => {}}
      />,
    )

    fireEvent(screen.getByTestId('error-retry'), 'focus')
    expect(resolvedStyle('error-retry').outlineWidth).toBe(
      nativeTokens.light.focus['bb-focus-ring-width'],
    )
  })

  it('does not render a detail disclosure when no detail is provided', () => {
    render(
      <ErrorState
        testID="error"
        title="Could not load"
        description="Try again."
        onRetry={() => {}}
      />,
    )

    expect(screen.queryByTestId('error-detail-toggle')).toBeNull()
    expect(screen.queryByTestId('error-detail')).toBeNull()
  })

  it('keeps a stack-trace-shaped detail out of the tree until the disclosure is expanded', () => {
    render(
      <ErrorState
        testID="error"
        title="Could not load"
        description="Try again."
        onRetry={() => {}}
        detail={STACK_TRACE_DETAIL}
      />,
    )

    // Collapsed: absent from the tree entirely, even when hidden elements are included.
    expect(screen.queryByTestId('error-detail', { includeHiddenElements: true })).toBeNull()
    expect(screen.queryByText(/at Object\.<anonymous>/)).toBeNull()

    const toggle = screen.getByTestId('error-detail-toggle')
    expect(toggle.props.accessibilityState.expanded).toBe(false)

    fireEvent.press(toggle)

    // Expanded: the detail is now rendered and readable.
    expect(screen.getByTestId('error-detail')).toBeTruthy()
    expect(screen.getByText(/at Object\.<anonymous>/)).toBeTruthy()
    expect(screen.getByTestId('error-detail-toggle').props.accessibilityState.expanded).toBe(true)

    fireEvent.press(screen.getByTestId('error-detail-toggle'))

    // Collapsed again: gone from the tree.
    expect(screen.queryByTestId('error-detail', { includeHiddenElements: true })).toBeNull()
  })

  it('gives the disclosure toggle a text accessible name, not a colour-only cue', () => {
    render(
      <ErrorState
        testID="error"
        title="Could not load"
        description="Try again."
        onRetry={() => {}}
        detail="Request failed"
      />,
    )

    const toggle = screen.getByTestId('error-detail-toggle')
    expect(toggle.props.accessibilityRole).toBe('button')
    expect(toggle.props.accessibilityLabel).toBe('Show details')

    fireEvent.press(toggle)
    expect(screen.getByTestId('error-detail-toggle').props.accessibilityLabel).toBe('Hide details')
  })
})

describe('web ARIA state', () => {
  it('puts aria-expanded on the ErrorState detail toggle on web', () => {
    const platform = jest.replaceProperty(Platform, 'OS', 'web')
    try {
      render(
        <ErrorState
          testID="web-error"
          title="Could not load"
          description="Try again."
          onRetry={() => {}}
          detail="Request failed"
        />,
      )

      // React Native's Pressable folds `aria-expanded` into `accessibilityState` before the host
      // node, so the raw prop is read off the composite Pressable (see button.test.tsx).
      const toggle = () =>
        screen
          .UNSAFE_getAllByType(Pressable)
          .find((node) => node.props.testID === 'web-error-detail-toggle')?.props['aria-expanded']

      expect(toggle()).toBe(false)
      fireEvent.press(screen.getByTestId('web-error-detail-toggle'))
      expect(toggle()).toBe(true)
    } finally {
      platform.restore()
    }
  })

  it('does not put aria-expanded on the detail toggle on native', () => {
    render(
      <ErrorState
        testID="native-error"
        title="Could not load"
        description="Try again."
        onRetry={() => {}}
        detail="Request failed"
      />,
    )

    const toggle = screen
      .UNSAFE_getAllByType(Pressable)
      .find((node) => node.props.testID === 'native-error-detail-toggle')
    expect(toggle?.props['aria-expanded']).toBeUndefined()
  })
})

describe('STATES_REGISTRY', () => {
  it('has one entry per text-bearing role', () => {
    const names = STATES_REGISTRY.map((entry) => entry.name)
    for (const name of [
      'EmptyState.title',
      'EmptyState.description',
      'ErrorState.title',
      'ErrorState.description',
      'ErrorState.retry',
      'ErrorState.detailToggle',
      'ErrorState.detail',
    ]) {
      expect(names).toContain(name)
    }
    expect(STATES_REGISTRY).toHaveLength(7)
  })

  it('resolves every foreground and background token in both themes', () => {
    for (const entry of STATES_REGISTRY) {
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

  it('references the EmptyState and ErrorState primitives', () => {
    expect(STATES_REGISTRY.find((entry) => entry.name === 'EmptyState.title')?.component).toBe(
      EmptyState,
    )
    expect(STATES_REGISTRY.find((entry) => entry.name === 'ErrorState.retry')?.component).toBe(
      ErrorState,
    )
  })

  it('marks the retry and disclosure toggle as interactive', () => {
    expect(STATES_REGISTRY.find((entry) => entry.name === 'ErrorState.retry')?.interactive).toBe(true)
    expect(STATES_REGISTRY.find((entry) => entry.name === 'ErrorState.detailToggle')?.interactive).toBe(
      true,
    )
    expect(STATES_REGISTRY.find((entry) => entry.name === 'EmptyState.title')?.interactive).toBe(false)
  })
})

describe('state colour provenance', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`renders only token colours (${scheme})`, () => {
      const allowed = allowedColors()
      setColorScheme(scheme)
      render(
        <View>
          <EmptyState
            testID="prov-empty"
            title="No accounts"
            description="Link one"
            icon={<View testID="prov-empty-glyph" />}
            action={<Button testID="prov-empty-action" label="Link" onPress={() => {}} />}
          />
          <ErrorState
            testID="prov-error"
            title="Could not load"
            description="Try again."
            onRetry={() => {}}
            detail="Request failed"
          />
        </View>,
      )

      fireEvent.press(screen.getByTestId('prov-error-detail-toggle'))

      const ids = [
        'prov-empty-title',
        'prov-empty-description',
        'prov-empty-action',
        'prov-empty-action-label',
        'prov-error-title',
        'prov-error-description',
        'prov-error-retry',
        'prov-error-retry-label',
        'prov-error-detail-toggle',
        'prov-error-detail',
      ]

      for (const id of ids) {
        for (const color of collectHexColors(resolvedStyle(id))) {
          expect({ id, color, allowed: allowed.has(color) }).toEqual({ id, color, allowed: true })
        }
      }
    })
  }
})

describe('state source bans', () => {
  const read = (file: string) => readFileSync(resolve(UI_DIR, file), 'utf8')

  it('never renders a raw stack trace or request body from an internal error object', () => {
    for (const file of ['EmptyState.tsx', 'ErrorState.tsx']) {
      expect({ file, source: read(file) }).toEqual({
        file,
        source: expect.not.stringMatching(/\.stack\b|JSON\.stringify/),
      })
    }
  })

  it('never adds a PII or session-replay marker', () => {
    for (const file of ['EmptyState.tsx', 'ErrorState.tsx']) {
      expect({ file, source: read(file) }).toEqual({
        file,
        source: expect.not.stringMatching(/data-ph-mask|ph-mask|maskValue/),
      })
    }
  })
})