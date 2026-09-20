import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import {
  Button,
  ButtonText,
  type ButtonSize,
  type ButtonVariant,
} from '../../components/ui/Button'
import { IconButton, type IconButtonProps } from '../../components/ui/IconButton'
import { BUTTON_REGISTRY } from '../../components/ui/button.registry'
import { setColorScheme } from '../color-scheme'
import {
  COLOR_SCHEMES,
  allowedColors,
  collectHexColors,
  resolvedColor,
  resolvedStyle,
  runtimeTokenColors,
  type ColorScheme,
} from './helpers'

const VARIANTS: readonly ButtonVariant[] = ['primary', 'secondary', 'outline', 'ghost', 'destructive', 'link']
const SIZES: readonly ButtonSize[] = ['sm', 'md', 'lg']

const TOUCH_MIN = nativeTokens.light.touchTarget.min
const FOCUS_WIDTH = nativeTokens.light.focus['bb-focus-ring-width']
const FOCUS_OFFSET = nativeTokens.light.focus['bb-focus-ring-offset']

// The token each variant's text must resolve to, and (where it has one) the fill behind it.
const VARIANT_TOKENS: Record<ButtonVariant, { foreground: string; background: string | null }> = {
  primary: { foreground: 'primary-foreground', background: 'primary' },
  secondary: { foreground: 'secondary-foreground', background: 'secondary' },
  outline: { foreground: 'foreground', background: 'background' },
  ghost: { foreground: 'foreground', background: null },
  destructive: { foreground: 'destructive-foreground', background: 'bb-danger-strong' },
  link: { foreground: 'primary', background: null },
}

const focusColor = (scheme: ColorScheme): string =>
  nativeTokens[scheme].focus['bb-focus-ring-color']

describe('Button touch targets', () => {
  it('meets the touch floor for every variant x size by minHeight', () => {
    render(
      <View>
        {VARIANTS.flatMap((variant) =>
          SIZES.map((size) => (
            <Button key={`${variant}-${size}`} testID={`btn-${variant}-${size}`} variant={variant} size={size} label="Go" />
          )),
        )}
      </View>,
    )

    for (const variant of VARIANTS) {
      for (const size of SIZES) {
        const style = resolvedStyle(`btn-${variant}-${size}`)
        expect({ variant, size, minHeight: style.minHeight }).toEqual({
          variant,
          size,
          minHeight: expect.any(Number),
        })
        expect(style.minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
      }
    }
  })

  it('meets the 44x44 floor for every IconButton size by minHeight and minWidth', () => {
    render(
      <View>
        {VARIANTS.flatMap((variant) =>
          SIZES.map((size) => (
            <IconButton
              key={`${variant}-${size}`}
              testID={`icon-${variant}-${size}`}
              variant={variant}
              size={size}
              label={`${variant} ${size}`}
            />
          )),
        )}
      </View>,
    )

    for (const variant of VARIANTS) {
      for (const size of SIZES) {
        const style = resolvedStyle(`icon-${variant}-${size}`)
        expect(style.minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
        expect(style.minWidth as number).toBeGreaterThanOrEqual(TOUCH_MIN)
      }
    }
  })
})

describe('Button variants', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`resolves every variant's foreground (and destructive fill) to its token (${scheme})`, () => {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      render(
        <View>
          {VARIANTS.map((variant) => (
            <Button key={variant} testID={`v-${variant}`} variant={variant} label="Go" />
          ))}
        </View>,
      )

      for (const variant of VARIANTS) {
        const tokens = VARIANT_TOKENS[variant]
        expect(resolvedColor(`v-${variant}-label`, 'color')).toBe(runtime[tokens.foreground])
        if (tokens.background) {
          expect(resolvedColor(`v-${variant}`, 'backgroundColor')).toBe(runtime[tokens.background])
        }
      }
    })
  }

  it('fills destructive with danger-strong, never the raw destructive value', () => {
    for (const scheme of COLOR_SCHEMES) {
      const runtime = runtimeTokenColors()[scheme]
      const adapter = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>
      act(() => setColorScheme(scheme))
      render(<Button testID={`d-${scheme}`} variant="destructive" label="Delete" />)

      expect(resolvedColor(`d-${scheme}`, 'backgroundColor')).toBe(runtime['bb-danger-strong'])
      expect(resolvedColor(`d-${scheme}`, 'backgroundColor')).not.toBe(runtime.destructive)
      expect(resolvedColor(`d-${scheme}-label`, 'color')).toBe(runtime['destructive-foreground'])
      expect(adapter['bb-danger-strong']).not.toBe(adapter.destructive)
    }
  })
})

describe('Button layout', () => {
  it('stretches fullWidth to 100%', () => {
    render(
      <View>
        <Button testID="full" fullWidth label="Wide" />
        <Button testID="not-full" label="Narrow" />
      </View>,
    )

    expect(resolvedStyle('full').width).toBe('100%')
    expect(resolvedStyle('not-full').width).toBeUndefined()
  })

  it('renders a label prop and a ButtonText child', () => {
    render(
      <View>
        <Button testID="labelled" label="Save changes" />
        <Button testID="child">
          <ButtonText>Custom child</ButtonText>
        </Button>
      </View>,
    )

    expect(screen.getByText('Save changes')).toBeTruthy()
    expect(screen.getByText('Custom child')).toBeTruthy()
  })
})

describe('Button disabled state', () => {
  it('sets accessibilityState.disabled and blocks onPress', () => {
    const onPress = jest.fn()
    render(<Button testID="disabled" disabled label="Go" onPress={onPress} />)

    const node = screen.getByTestId('disabled')
    expect(node.props.accessibilityRole).toBe('button')
    expect(node.props.accessibilityState).toMatchObject({ disabled: true, busy: false })

    fireEvent.press(node)
    expect(onPress).not.toHaveBeenCalled()
  })

  it('conveys disabled with a non-colour cue as well as colour', () => {
    render(
      <View>
        <Button testID="disabled-cue" disabled label="Go" />
        <Button testID="enabled-cue" label="Go" />
      </View>,
    )

    const disabled = resolvedStyle('disabled-cue')
    expect(disabled.opacity as number).toBeLessThan(1)
    expect(resolvedStyle('enabled-cue').opacity).toBe(1)
    expect(screen.getByTestId('disabled-cue').props.accessibilityState.disabled).toBe(true)
  })

  it('still calls onPress when enabled', () => {
    const onPress = jest.fn()
    render(<Button testID="enabled" label="Go" onPress={onPress} />)

    fireEvent.press(screen.getByTestId('enabled'))
    expect(onPress).toHaveBeenCalledTimes(1)
  })
})

describe('Button loading state', () => {
  it('sets accessibilityState.busy and renders a spinner with an accessible name', () => {
    render(<Button testID="load" loading label="Save" />)

    const node = screen.getByTestId('load')
    expect(node.props.accessibilityState).toMatchObject({ busy: true, disabled: true })
    expect(screen.getByTestId('load-spinner')).toBeTruthy()
    expect(screen.getByLabelText('Loading')).toBeTruthy()
  })

  it('keeps the label rendered while loading and blocks onPress', () => {
    const onPress = jest.fn()
    render(<Button testID="load2" loading label="Save" onPress={onPress} />)

    expect(screen.getByText('Save')).toBeTruthy()
    fireEvent.press(screen.getByTestId('load2'))
    expect(onPress).not.toHaveBeenCalled()
  })

  it('does not convey loading by colour alone', () => {
    render(
      <View>
        <Button testID="loading-cue" loading label="Save" />
        <Button testID="idle-cue" label="Save" />
      </View>,
    )

    // The non-colour cue is the spinner plus the busy state, not a colour swap.
    expect(screen.getByTestId('loading-cue-spinner')).toBeTruthy()
    expect(screen.getByTestId('loading-cue').props.accessibilityState.busy).toBe(true)
    expect(screen.getByTestId('idle-cue').props.accessibilityState.busy).toBe(false)
  })
})

describe('Button accessible name', () => {
  it('keeps the accessible name when a leading icon is present', () => {
    render(
      <View>
        <Button testID="lead" label="Add account" leadingIcon={<View testID="lead-icon" />} />
        <Button testID="trail" label="Next" trailingIcon={<View testID="trail-icon" />} />
      </View>,
    )

    expect(screen.getByTestId('lead').props.accessibilityLabel).toBe('Add account')
    expect(screen.getByTestId('lead').props.accessibilityRole).toBe('button')
    expect(screen.getByTestId('trail').props.accessibilityLabel).toBe('Next')
    expect(screen.getByTestId('trail').props.accessibilityRole).toBe('button')
  })
})

describe('IconButton', () => {
  it('requires a label for the accessible name at the type level', () => {
    // @ts-expect-error `label` is required — an icon-only button must carry an accessible name.
    const props: IconButtonProps = { onPress: () => {} }
    expect(props.label).toBeUndefined()
  })

  it('exposes the label as the accessible name with a button role', () => {
    render(<IconButton testID="icon" label="Close" />)

    const node = screen.getByTestId('icon')
    expect(node.props.accessibilityRole).toBe('button')
    expect(node.props.accessibilityLabel).toBe('Close')
  })

  it('blocks onPress when disabled', () => {
    const onPress = jest.fn()
    render(<IconButton testID="icon-disabled" label="Close" disabled onPress={onPress} />)

    fireEvent.press(screen.getByTestId('icon-disabled'))
    expect(onPress).not.toHaveBeenCalled()
  })
})

describe('Button focus ring', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`shows the focusTokens ring on focus and clears it on blur (${scheme})`, () => {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      render(
        <View>
          <Button testID="focus-btn" label="Go" />
          <IconButton testID="focus-icon" label="Go" />
        </View>,
      )

      // The focus-ring colour token resolves to the semantic ring colour in both themes.
      expect(focusColor(scheme)).toBe(runtime.ring)

      for (const id of ['focus-btn', 'focus-icon']) {
        fireEvent(screen.getByTestId(id), 'focus')
        const focused = resolvedStyle(id)
        expect(focused.outlineWidth).toBe(FOCUS_WIDTH)
        expect(focused.outlineOffset).toBe(FOCUS_OFFSET)
        expect(resolvedColor(id, 'outlineColor')).toBe(runtime.ring)

        fireEvent(screen.getByTestId(id), 'blur')
        expect(resolvedStyle(id).outlineWidth).toBeUndefined()
      }
    })
  }
})

describe('BUTTON_REGISTRY', () => {
  it('has exactly one interactive entry per Button variant', () => {
    expect(BUTTON_REGISTRY).toHaveLength(VARIANTS.length)
    for (const variant of VARIANTS) {
      const entry = BUTTON_REGISTRY.find((item) => item.name === `Button.${variant}`)
      expect(entry).toBeDefined()
      expect(entry?.interactive).toBe(true)
    }
  })

  it('pins destructive to destructive-foreground on bb-danger-strong', () => {
    const entry = BUTTON_REGISTRY.find((item) => item.name === 'Button.destructive')
    expect(entry).toMatchObject({
      foregroundToken: 'destructive-foreground',
      backgroundToken: 'bb-danger-strong',
      ratioClass: 'body',
    })
    expect(entry?.component).toBe(Button)
  })

  it('resolves every foreground/background token in both themes', () => {
    for (const entry of BUTTON_REGISTRY) {
      for (const scheme of COLOR_SCHEMES) {
        const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>
        expect(section[entry.foregroundToken]).toBeDefined()
        expect(section[entry.backgroundToken]).toBeDefined()
      }
    }
  })
})

describe('Button colour provenance', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`renders only token colours (${scheme})`, () => {
      const allowed = allowedColors()
      setColorScheme(scheme)
      render(
        <View>
          {VARIANTS.flatMap((variant) =>
            SIZES.map((size) => (
              <Button key={`${variant}-${size}`} testID={`p-${variant}-${size}`} variant={variant} size={size} label="Go" />
            )),
          )}
          {VARIANTS.flatMap((variant) =>
            SIZES.map((size) => (
              <IconButton
                key={`${variant}-${size}`}
                testID={`pi-${variant}-${size}`}
                variant={variant}
                size={size}
                label="Go"
              />
            )),
          )}
        </View>,
      )

      const ids = [
        ...VARIANTS.flatMap((variant) => SIZES.map((size) => `p-${variant}-${size}`)),
        ...VARIANTS.flatMap((variant) => SIZES.map((size) => `p-${variant}-${size}-label`)),
        ...VARIANTS.flatMap((variant) => SIZES.map((size) => `pi-${variant}-${size}`)),
      ]

      for (const id of ids) {
        for (const color of collectHexColors(resolvedStyle(id))) {
          expect({ id, color, allowed: allowed.has(color) }).toEqual({ id, color, allowed: true })
        }
      }
    })
  }
})

describe('Button types', () => {
  it('exposes the documented prop unions', () => {
    const variant: ButtonVariant = 'outline'
    const size: ButtonSize = 'lg'
    expect(variant).toBe('outline')
    expect(size).toBe('lg')
  })
})
