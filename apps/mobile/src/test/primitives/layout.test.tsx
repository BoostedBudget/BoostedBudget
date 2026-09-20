import { render, screen } from '@testing-library/react-native'
import { View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import {
  Box,
  Inline,
  Stack,
  Surface,
  Text,
  type BoxBgTone,
  type BoxBorderTone,
  type BoxProps,
  type BoxRadiusTone,
  type InlineProps,
  type SpacingStep,
  type StackProps,
  type SurfaceVariant,
  type TextTone,
  type TextVariant,
} from '../../components/ui'
import { setColorScheme } from '../color-scheme'
import { COLOR_SCHEMES, allowedColors, collectHexColors, resolvedColor, resolvedStyle, runtimeTokenColors } from './helpers'

const STEPS: readonly SpacingStep[] = [0, 1, 2, 3, 4, 5, 6, 8, 10, 12, 16]

const SPACING_PROP_TARGETS = {
  p: 'padding',
  px: 'paddingInline',
  py: 'paddingBlock',
  pt: 'paddingTop',
  pb: 'paddingBottom',
  pl: 'paddingLeft',
  pr: 'paddingRight',
  m: 'margin',
  mx: 'marginInline',
  my: 'marginBlock',
  mt: 'marginTop',
  mb: 'marginBottom',
  ml: 'marginLeft',
  mr: 'marginRight',
  gap: 'gap',
} as const

type SpacingProp = keyof typeof SPACING_PROP_TARGETS

const spacingValue = (step: SpacingStep): number =>
  (nativeTokens.light.spacing as Record<string, number>)[`spacing-bb-${step}`]

const SURFACES: Record<SurfaceVariant, { background: string; border?: string }> = {
  canvas: { background: 'background' },
  card: { background: 'card', border: 'border' },
  raised: { background: 'card', border: 'border' },
  sunken: { background: 'muted' },
  overlay: { background: 'popover', border: 'border' },
}

const BOX_BG_TOKENS: Record<BoxBgTone, string | null> = {
  none: null,
  background: 'background',
  card: 'card',
  muted: 'muted',
  accent: 'accent',
  secondary: 'secondary',
  primary: 'primary',
  success: 'success',
  warning: 'warning',
  danger: 'destructive',
  info: 'info',
}

const BOX_BORDER_TOKENS: Record<BoxBorderTone, string | null> = {
  none: null,
  default: 'border',
  strong: 'ring',
  primary: 'primary',
  danger: 'destructive',
}

const BOX_RADIUS_TONES: readonly BoxRadiusTone[] = ['none', 'lg', 'full']

const ALL_VARIANTS: readonly TextVariant[] = [
  'display',
  'title',
  'heading',
  'subtitle',
  'body',
  'bodySmall',
  'caption',
  'label',
  'mono',
]
const ALL_TONES: readonly TextTone[] = ['default', 'muted', 'subtle', 'primary', 'success', 'warning', 'danger', 'info']

describe('Box spacing', () => {
  it('resolves every spacing prop and scale step to the spacing token', () => {
    const props = Object.keys(SPACING_PROP_TARGETS) as SpacingProp[]
    render(
      <View>
        {props.flatMap((prop) =>
          STEPS.map((step) => (
            <Box
              key={`${prop}-${step}`}
              testID={`box-${prop}-${step}`}
              {...({ [prop]: step } as BoxProps)}
            />
          )),
        )}
      </View>,
    )

    for (const prop of props) {
      for (const step of STEPS) {
        const target = SPACING_PROP_TARGETS[prop]
        expect({ prop, step, value: resolvedStyle(`box-${prop}-${step}`)[target] }).toEqual({
          prop,
          step,
          value: spacingValue(step),
        })
      }
    }
  })
})

describe('Box surface tones', () => {
  it('resolves every bg tone to its semantic token in both themes', () => {
    for (const scheme of COLOR_SCHEMES) {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      const tones = Object.keys(BOX_BG_TOKENS) as BoxBgTone[]
      render(
        <View>
          {tones.map((tone) => (
            <Box key={tone} testID={`bg-${tone}`} bg={tone} />
          ))}
        </View>,
      )

      for (const tone of tones) {
        const token = BOX_BG_TOKENS[tone]
        const background = resolvedColor(`bg-${tone}`, 'backgroundColor')
        if (token === null) {
          expect(background).toBeUndefined()
        } else {
          expect(background).toBe(runtime[token])
        }
      }
    }
  })

  it('resolves every border tone to its semantic token', () => {
    for (const scheme of COLOR_SCHEMES) {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      const tones = Object.keys(BOX_BORDER_TOKENS) as BoxBorderTone[]
      render(
        <View>
          {tones.map((tone) => (
            <Box key={tone} testID={`border-${tone}`} border={tone} />
          ))}
        </View>,
      )

      for (const tone of tones) {
        const token = BOX_BORDER_TOKENS[tone]
        const style = resolvedStyle(`border-${tone}`)
        if (token === null) {
          expect(style.borderWidth).toBe(0)
        } else {
          expect(style.borderWidth).toBe(1)
          expect(resolvedColor(`border-${tone}`, 'borderColor')).toBe(runtime[token])
        }
      }
    }
  })

  it('resolves every radius tone to a border radius', () => {
    render(
      <View>
        {BOX_RADIUS_TONES.map((tone) => (
          <Box key={tone} testID={`radius-${tone}`} radius={tone} />
        ))}
      </View>,
    )

    expect(resolvedStyle('radius-none').borderRadius).toBe(0)
    for (const tone of BOX_RADIUS_TONES.filter((value) => value !== 'none')) {
      expect(typeof resolvedStyle(`radius-${tone}`).borderRadius).toBe('number')
    }
    expect(resolvedStyle('radius-full').borderRadius as number).toBeGreaterThan(
      resolvedStyle('radius-lg').borderRadius as number,
    )
  })
})

describe('Surface variants', () => {
  it('maps every variant to its surface tokens in both themes', () => {
    for (const scheme of COLOR_SCHEMES) {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      const variants = Object.keys(SURFACES) as SurfaceVariant[]
      render(
        <View>
          {variants.map((variant) => (
            <Surface key={variant} testID={`surface-${variant}`} variant={variant} />
          ))}
        </View>,
      )

      for (const variant of variants) {
        const spec = SURFACES[variant]
        const style = resolvedStyle(`surface-${variant}`)
        expect(resolvedColor(`surface-${variant}`, 'backgroundColor')).toBe(runtime[spec.background])
        if (spec.border) {
          expect(style.borderWidth).toBe(1)
          expect(resolvedColor(`surface-${variant}`, 'borderColor')).toBe(runtime[spec.border])
        }
      }
    }
  })
})

describe('Stack and Inline', () => {
  it('Stack lays out vertically with token gap, align and justify', () => {
    const align = ['start', 'center', 'end', 'stretch', 'baseline'] as const
    const justify = ['start', 'center', 'end', 'between', 'around', 'evenly'] as const
    render(
      <View>
        {align.map((value) => (
          <Stack key={`align-${value}`} testID={`stack-align-${value}`} align={value} />
        ))}
        {justify.map((value) => (
          <Stack key={`justify-${value}`} testID={`stack-justify-${value}`} justify={value} />
        ))}
        <Stack testID="stack-gap" gap={4} />
      </View>,
    )

    expect(resolvedStyle('stack-gap').flexDirection).toBe('column')
    expect(resolvedStyle('stack-gap').gap).toBe(spacingValue(4))
    expect(resolvedStyle('stack-align-center').alignItems).toBe('center')
    expect(resolvedStyle('stack-align-baseline').alignItems).toBe('baseline')
    expect(resolvedStyle('stack-justify-between').justifyContent).toBe('space-between')
    expect(resolvedStyle('stack-justify-evenly').justifyContent).toBe('space-evenly')
  })

  it('Inline lays out horizontally with token gap, align, justify and wrap', () => {
    render(
      <View>
        <Inline testID="inline-base" gap={2} align="center" justify="center" />
        <Inline testID="inline-wrap" wrap />
      </View>,
    )

    const base = resolvedStyle('inline-base')
    expect(base.flexDirection).toBe('row')
    expect(base.gap).toBe(spacingValue(2))
    expect(base.alignItems).toBe('center')
    expect(base.justifyContent).toBe('center')
    expect(resolvedStyle('inline-wrap').flexWrap).toBe('wrap')
    expect(resolvedStyle('inline-base').flexWrap).toBe('nowrap')
  })
})

describe('Primitive colour provenance', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`every primitive renders only token colours (${scheme})`, () => {
      const allowed = allowedColors()
      setColorScheme(scheme)
      render(
        <View>
          {ALL_VARIANTS.flatMap((variant) =>
            ALL_TONES.map((tone) => (
              <Text key={`${variant}-${tone}`} testID={`p-text-${variant}-${tone}`} variant={variant} tone={tone}>
                x
              </Text>
            )),
          )}
          {(Object.keys(SURFACES) as SurfaceVariant[]).map((variant) => (
            <Surface key={variant} testID={`p-surface-${variant}`} variant={variant} />
          ))}
          {(Object.keys(BOX_BG_TOKENS) as BoxBgTone[]).map((tone) => (
            <Box key={tone} testID={`p-box-${tone}`} bg={tone} />
          ))}
          <Box testID="p-box" />
          <Stack testID="p-stack" gap={4} />
          <Inline testID="p-inline" gap={4} wrap />
        </View>,
      )

      const ids = [
        ...ALL_VARIANTS.flatMap((variant) => ALL_TONES.map((tone) => `p-text-${variant}-${tone}`)),
        ...(Object.keys(SURFACES) as SurfaceVariant[]).map((variant) => `p-surface-${variant}`),
        ...(Object.keys(BOX_BG_TOKENS) as BoxBgTone[]).map((tone) => `p-box-${tone}`),
        'p-box',
        'p-stack',
        'p-inline',
      ]

      for (const id of ids) {
        for (const color of collectHexColors(resolvedStyle(id))) {
          expect({ id, color, allowed: allowed.has(color) }).toEqual({ id, color, allowed: true })
        }
      }
    })
  }
})

describe('Primitive accessibility passthrough', () => {
  it('forwards role, label and testID from every layout primitive', () => {
    render(
      <View>
        <Box testID="a-box" accessibilityRole="summary" accessibilityLabel="box" />
        <Stack testID="a-stack" accessibilityRole="list" accessibilityLabel="stack" />
        <Inline testID="a-inline" accessibilityRole="toolbar" accessibilityLabel="inline" />
        <Surface testID="a-surface" accessibilityRole="summary" accessibilityLabel="surface" />
      </View>,
    )

    const expected: Record<string, string> = {
      'a-box': 'summary',
      'a-stack': 'list',
      'a-inline': 'toolbar',
      'a-surface': 'summary',
    }
    for (const [id, role] of Object.entries(expected)) {
      const node = screen.getByTestId(id)
      expect(node.props.accessibilityRole).toBe(role)
      expect(node.props.accessibilityLabel).toBe(id.replace('a-', ''))
    }
  })

  it('accepts a className passthrough on every primitive', () => {
    render(
      <View>
        <Box testID="c-box" className="p-bb-2" />
        <Stack testID="c-stack" className="gap-bb-2" />
        <Inline testID="c-inline" className="gap-bb-2" />
        <Surface testID="c-surface" className="rounded-full" />
      </View>,
    )

    expect(resolvedStyle('c-box').padding).toBe(spacingValue(2))
    expect(resolvedStyle('c-stack').gap).toBe(spacingValue(2))
    expect(resolvedStyle('c-inline').gap).toBe(spacingValue(2))
    expect(resolvedStyle('c-surface').borderRadius as number).toBeGreaterThan(1000)
  })
})

describe('Primitive types', () => {
  it('exposes the documented prop unions', () => {
    const stackProps: StackProps = { gap: 4, align: 'center', justify: 'between' }
    const inlineProps: InlineProps = { gap: 4, align: 'start', justify: 'evenly', wrap: true }
    expect(stackProps.gap).toBe(4)
    expect(inlineProps.wrap).toBe(true)
  })
})
