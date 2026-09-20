import { render, screen } from '@testing-library/react-native'
import { View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Text, type TextAlign, type TextTone, type TextVariant } from '../../components/ui'
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

const VARIANTS: Record<
  TextVariant,
  { size: string; leading: string; weight: string; family: string; tracking: string }
> = {
  display: { size: 'text-bb-4xl', leading: 'leading-bb-tight', weight: 'font-weight-bb-bold', family: 'font-bb-sans', tracking: 'tracking-bb-tight' },
  title: { size: 'text-bb-3xl', leading: 'leading-bb-tight', weight: 'font-weight-bb-bold', family: 'font-bb-sans', tracking: 'tracking-bb-tight' },
  heading: { size: 'text-bb-2xl', leading: 'leading-bb-tight', weight: 'font-weight-bb-semibold', family: 'font-bb-sans', tracking: 'tracking-bb-tight' },
  subtitle: { size: 'text-bb-xl', leading: 'leading-bb-snug', weight: 'font-weight-bb-semibold', family: 'font-bb-sans', tracking: 'tracking-bb-normal' },
  body: { size: 'text-bb-body', leading: 'leading-bb-normal', weight: 'font-weight-bb-regular', family: 'font-bb-sans', tracking: 'tracking-bb-normal' },
  bodySmall: { size: 'text-bb-sm', leading: 'leading-bb-normal', weight: 'font-weight-bb-regular', family: 'font-bb-sans', tracking: 'tracking-bb-normal' },
  caption: { size: 'text-bb-xs', leading: 'leading-bb-normal', weight: 'font-weight-bb-regular', family: 'font-bb-sans', tracking: 'tracking-bb-normal' },
  label: { size: 'text-bb-sm', leading: 'leading-bb-snug', weight: 'font-weight-bb-medium', family: 'font-bb-sans', tracking: 'tracking-bb-normal' },
  mono: { size: 'text-bb-sm', leading: 'leading-bb-normal', weight: 'font-weight-bb-regular', family: 'font-bb-mono', tracking: 'tracking-bb-normal' },
}

const TONES: readonly TextTone[] = ['default', 'muted', 'subtle', 'primary', 'success', 'warning', 'danger', 'info']

const TONE_TOKENS: Record<TextTone, string> = {
  default: 'foreground',
  muted: 'muted-foreground',
  subtle: 'muted-foreground',
  primary: 'primary',
  success: 'success',
  warning: 'warning-foreground',
  danger: 'bb-danger-text',
  info: 'bb-info-text',
}

function typographyOf(scheme: ColorScheme): Record<string, number | string> {
  return nativeTokens[scheme].typography as unknown as Record<string, number | string>
}

function firstFamily(stack: string): string {
  return stack.split(',')[0].trim()
}

function round2(value: number): number {
  return Math.round((value + Number.EPSILON) * 100) / 100
}

describe('Text typography', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`maps every variant to its token typography entry (${scheme})`, () => {
      setColorScheme(scheme)
      const typography = typographyOf(scheme)
      render(
        <View>
          {(Object.keys(VARIANTS) as TextVariant[]).map((variant) => (
            <Text key={variant} testID={`variant-${variant}`} variant={variant}>
              x
            </Text>
          ))}
        </View>,
      )

      for (const variant of Object.keys(VARIANTS) as TextVariant[]) {
        const spec = VARIANTS[variant]
        const size = typography[spec.size] as number
        const leading = typography[spec.leading] as number
        const weight = typography[spec.weight] as number
        const tracking = typography[spec.tracking] as number
        const style = resolvedStyle(`variant-${variant}`)

        expect(style.fontSize).toBe(size)
        expect(String(style.fontWeight)).toBe(String(weight))
        expect(style.lineHeight).toBeCloseTo(round2(size * leading), 2)
        expect(style.letterSpacing).toBeCloseTo(round2(size * tracking), 2)
        expect(style.fontFamily).toBe(firstFamily(typography[spec.family] as string))
      }
    })

    it(`resolves every tone to its token colour (${scheme})`, () => {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      render(
        <View>
          {TONES.map((tone) => (
            <Text key={tone} testID={`tone-${tone}`} tone={tone}>
              x
            </Text>
          ))}
        </View>,
      )

      for (const tone of TONES) {
        expect(resolvedColor(`tone-${tone}`, 'color')).toBe(runtime[TONE_TOKENS[tone]])
      }
      expect(resolvedColor('tone-warning', 'backgroundColor')).toBe(runtime.warning)
    })

    it(`renders every variant x tone without any non-token colour (${scheme})`, () => {
      const allowed = allowedColors()
      setColorScheme(scheme)
      render(
        <View>
          {(Object.keys(VARIANTS) as TextVariant[]).flatMap((variant) =>
            TONES.map((tone) => (
              <Text key={`${variant}-${tone}`} testID={`grid-${variant}-${tone}`} variant={variant} tone={tone}>
                x
              </Text>
            )),
          )}
        </View>,
      )

      for (const variant of Object.keys(VARIANTS) as TextVariant[]) {
        for (const tone of TONES) {
          for (const color of collectHexColors(resolvedStyle(`grid-${variant}-${tone}`))) {
            expect({ variant, tone, color, allowed: allowed.has(color) }).toEqual({
              variant,
              tone,
              color,
              allowed: true,
            })
          }
        }
      }
    })
  }

  it('uses the contrast tokens for info/danger body text, not the raw info/destructive values', () => {
    for (const scheme of COLOR_SCHEMES) {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      const adapter = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>

      render(
        <View>
          <Text testID={`info-${scheme}`} tone="info">
            x
          </Text>
          <Text testID={`danger-${scheme}`} tone="danger">
            x
          </Text>
        </View>,
      )

      expect(resolvedColor(`info-${scheme}`, 'color')).toBe(runtime['bb-info-text'])
      expect(resolvedColor(`info-${scheme}`, 'color')).not.toBe(runtime.info)
      expect(resolvedColor(`danger-${scheme}`, 'color')).toBe(runtime['bb-danger-text'])
      expect(resolvedColor(`danger-${scheme}`, 'color')).not.toBe(runtime.destructive)

      expect(adapter['bb-info-text']).not.toBe(adapter.info)
      expect(adapter['bb-danger-text']).not.toBe(adapter.destructive)
    }
  })

  it('maps align to textAlign', () => {
    render(
      <View>
        {(['left', 'center', 'right'] as TextAlign[]).map((align) => (
          <Text key={align} testID={`align-${align}`} align={align}>
            x
          </Text>
        ))}
      </View>,
    )

    expect(resolvedStyle('align-left').textAlign).toBe('left')
    expect(resolvedStyle('align-center').textAlign).toBe('center')
    expect(resolvedStyle('align-right').textAlign).toBe('right')
  })

  it('honours dynamic type and forwards accessibility props', () => {
    render(
      <Text
        testID="dynamic"
        variant="body"
        maxFontSizeMultiplier={2}
        numberOfLines={3}
        accessibilityRole="header"
        accessibilityLabel="Section title"
      >
        x
      </Text>,
    )

    const node = screen.getByTestId('dynamic')
    expect(node.props.allowFontScaling).toBe(true)
    expect(node.props.maxFontSizeMultiplier).toBe(2)
    expect(node.props.numberOfLines).toBe(3)
    expect(node.props.accessibilityRole).toBe('header')
    expect(node.props.accessibilityLabel).toBe('Section title')
  })

  it('leaves maxFontSizeMultiplier unset by default rather than capping growth', () => {
    render(
      <Text testID="default-cap" variant="body">
        x
      </Text>,
    )

    const node = screen.getByTestId('default-cap')
    expect(node.props.allowFontScaling).toBe(true)
    expect(node.props.maxFontSizeMultiplier).toBeUndefined()
  })
})
