import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fireEvent, render, screen } from '@testing-library/react-native'
import { Text, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  type CardVariant,
} from '../../components/ui/Card'
import { StatCard, type StatTrend } from '../../components/ui/StatCard'
import { CARD_REGISTRY } from '../../components/ui/card.registry'
import { setColorScheme } from '../color-scheme'
import {
  COLOR_SCHEMES,
  allowedColors,
  collectHexColors,
  resolvedColor,
  resolvedStyle,
  runtimeTokenColors,
} from './helpers'

const VARIANTS: readonly CardVariant[] = ['default', 'outlined', 'elevated', 'sunken']
const TRENDS: readonly StatTrend[] = ['up', 'down', 'flat']

const TOUCH_MIN = nativeTokens.light.touchTarget.min

// The surface token each variant paints behind its text.
const VARIANT_BACKGROUND: Record<CardVariant, string> = {
  default: 'card',
  outlined: 'card',
  elevated: 'card',
  sunken: 'muted',
}

// The foreground token each StatCard delta trend resolves to.
const TREND_FOREGROUND: Record<StatTrend, string> = {
  up: 'success',
  down: 'bb-danger-text',
  flat: 'muted-foreground',
}

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))
const UI_DIR = resolve(MOBILE_ROOT, 'src/components/ui')

describe('Card variants', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`maps each variant to its surface token (${scheme})`, () => {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      render(
        <View>
          {VARIANTS.map((variant) => (
            <Card key={variant} testID={`card-${variant}`} variant={variant} />
          ))}
        </View>,
      )

      for (const variant of VARIANTS) {
        expect(resolvedColor(`card-${variant}`, 'backgroundColor')).toBe(
          runtime[VARIANT_BACKGROUND[variant]],
        )
      }
    })
  }

  it('gives only the elevated variant an elevation shadow', () => {
    render(
      <View>
        {VARIANTS.map((variant) => (
          <Card key={variant} testID={`elev-${variant}`} variant={variant} />
        ))}
      </View>,
    )

    expect(resolvedStyle('elev-elevated').boxShadow).toBeDefined()
    for (const variant of ['default', 'outlined', 'sunken'] as const) {
      expect(resolvedStyle(`elev-${variant}`).boxShadow).toBeUndefined()
    }
  })
})

describe('Card pressability', () => {
  it('exposes a button role and the touch floor when onPress is present', () => {
    const onPress = jest.fn()
    render(<Card testID="pressable" onPress={onPress} />)

    const node = screen.getByTestId('pressable')
    expect(node.props.accessibilityRole).toBe('button')
    expect(resolvedStyle('pressable').minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)

    fireEvent.press(node)
    expect(onPress).toHaveBeenCalledTimes(1)
  })

  it('is a plain container with no button role when onPress is absent', () => {
    render(<Card testID="static" />)

    const node = screen.getByTestId('static')
    expect(node.props.accessibilityRole).toBeUndefined()
    expect(resolvedStyle('static').minHeight).toBeUndefined()
  })

  it('shows the focus ring on a pressable card and clears it on blur', () => {
    render(<Card testID="focus-card" onPress={() => {}} />)

    fireEvent(screen.getByTestId('focus-card'), 'focus')
    expect(resolvedStyle('focus-card').outlineWidth).toBe(
      nativeTokens.light.focus['bb-focus-ring-width'],
    )

    fireEvent(screen.getByTestId('focus-card'), 'blur')
    expect(resolvedStyle('focus-card').outlineWidth).toBeUndefined()
  })
})

describe('Card subcomponents', () => {
  it('renders header, title, description, content and footer', () => {
    render(
      <Card testID="composed">
        <CardHeader testID="composed-header">
          <CardTitle testID="composed-title">Account</CardTitle>
          <CardDescription testID="composed-description">Updated today</CardDescription>
        </CardHeader>
        <CardContent testID="composed-content">
          <Text>Body</Text>
        </CardContent>
        <CardFooter testID="composed-footer">
          <Text>Footer</Text>
        </CardFooter>
      </Card>,
    )

    expect(screen.getByText('Account')).toBeTruthy()
    expect(screen.getByText('Updated today')).toBeTruthy()
    expect(screen.getByText('Body')).toBeTruthy()
    expect(screen.getByText('Footer')).toBeTruthy()
    expect(screen.getByTestId('composed-header')).toBeTruthy()
    expect(screen.getByTestId('composed-footer')).toBeTruthy()
  })

  it('keeps the description legible on a sunken surface', () => {
    for (const scheme of COLOR_SCHEMES) {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      render(
        <View>
          <Card testID={`desc-card-${scheme}`} variant="default">
            <CardDescription testID={`desc-card-${scheme}-text`}>On card</CardDescription>
          </Card>
          <Card testID={`desc-sunken-${scheme}`} variant="sunken">
            <CardDescription testID={`desc-sunken-${scheme}-text`}>On muted</CardDescription>
          </Card>
        </View>,
      )

      expect(resolvedColor(`desc-card-${scheme}-text`, 'color')).toBe(runtime['muted-foreground'])
      // `muted-foreground` on `muted` is only 4.35:1 in light mode, so a sunken card's
      // secondary text falls back to the full-strength foreground token.
      expect(resolvedColor(`desc-sunken-${scheme}-text`, 'color')).toBe(runtime.foreground)
    }
  })
})

describe('StatCard', () => {
  it('renders the exact preformatted value string it is given', () => {
    render(<StatCard testID="stat" label="Balance" value="$1,234.56" />)

    const value = screen.getByText('$1,234.56')
    expect(value.props.children).toBe('$1,234.56')
  })

  it('does not reformat a value with a non-US grouping or a unicode minus', () => {
    const raw = '1.234,56 €'
    render(<StatCard testID="stat-raw" label="Balance" value={raw} />)

    expect(screen.getByText(raw).props.children).toBe(raw)
  })

  it('renders the label and an optional delta', () => {
    render(<StatCard testID="stat-full" label="Net worth" value="$10.00" delta="+$1.00" trend="up" />)

    expect(screen.getByText('Net worth')).toBeTruthy()
    expect(screen.getByText('+$1.00')).toBeTruthy()
  })

  for (const scheme of COLOR_SCHEMES) {
    it(`colours the delta by trend (${scheme})`, () => {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      render(
        <View>
          {TRENDS.map((trend) => (
            <StatCard
              key={trend}
              testID={`trend-${trend}`}
              label="Label"
              value="Value"
              delta="Delta"
              trend={trend}
            />
          ))}
        </View>,
      )

      for (const trend of TRENDS) {
        expect(resolvedColor(`trend-${trend}-delta`, 'color')).toBe(runtime[TREND_FOREGROUND[trend]])
      }
    })
  }

  it('hides a decorative icon from the accessibility tree', () => {
    render(
      <StatCard
        testID="stat-icon"
        label="Balance"
        value="$1.00"
        icon={<View testID="stat-icon-glyph" />}
      />,
    )

    const wrapper = screen.getByTestId('stat-icon-icon', { includeHiddenElements: true })
    expect(wrapper.props.accessibilityElementsHidden).toBe(true)
    expect(wrapper.props.importantForAccessibility).toBe('no-hide-descendants')
  })
})

describe('CARD_REGISTRY', () => {
  it('has one entry per Card variant x text role and one per StatCard text role', () => {
    for (const variant of VARIANTS) {
      expect(CARD_REGISTRY.find((entry) => entry.name === `Card.${variant}.title`)).toBeDefined()
      expect(CARD_REGISTRY.find((entry) => entry.name === `Card.${variant}.description`)).toBeDefined()
    }
    for (const name of [
      'StatCard.label',
      'StatCard.value',
      'StatCard.delta.up',
      'StatCard.delta.down',
      'StatCard.delta.flat',
    ]) {
      expect(CARD_REGISTRY.find((entry) => entry.name === name)).toBeDefined()
    }
    expect(CARD_REGISTRY).toHaveLength(VARIANTS.length * 2 + 5)
  })

  it('resolves every foreground and background token in both themes', () => {
    for (const entry of CARD_REGISTRY) {
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

  it('references the Card and StatCard primitives', () => {
    expect(CARD_REGISTRY.find((entry) => entry.name === 'Card.default.title')?.component).toBe(Card)
    expect(CARD_REGISTRY.find((entry) => entry.name === 'StatCard.value')?.component).toBe(StatCard)
  })

  it('pins the sunken description to the full-strength foreground on muted', () => {
    expect(CARD_REGISTRY.find((entry) => entry.name === 'Card.sunken.description')).toMatchObject({
      foregroundToken: 'foreground',
      backgroundToken: 'muted',
      ratioClass: 'body',
    })
  })
})

describe('Card colour provenance', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`renders only token colours (${scheme})`, () => {
      const allowed = allowedColors()
      setColorScheme(scheme)
      render(
        <View>
          {VARIANTS.map((variant) => (
            <Card key={variant} testID={`prov-${variant}`} variant={variant}>
              <CardHeader>
                <CardTitle testID={`prov-${variant}-title`}>Title</CardTitle>
                <CardDescription testID={`prov-${variant}-description`}>Description</CardDescription>
              </CardHeader>
              <CardContent testID={`prov-${variant}-content`}>Body</CardContent>
              <CardFooter testID={`prov-${variant}-footer`}>Footer</CardFooter>
            </Card>
          ))}
          <StatCard testID="prov-stat" label="Label" value="$1,234.56" delta="+1" trend="up" />
        </View>,
      )

      const ids = [
        ...VARIANTS.map((variant) => `prov-${variant}`),
        ...VARIANTS.map((variant) => `prov-${variant}-title`),
        ...VARIANTS.map((variant) => `prov-${variant}-description`),
        'prov-stat',
        'prov-stat-label',
        'prov-stat-value',
        'prov-stat-delta',
      ]

      for (const id of ids) {
        for (const color of collectHexColors(resolvedStyle(id))) {
          expect({ id, color, allowed: allowed.has(color) }).toEqual({ id, color, allowed: true })
        }
      }
    })
  }
})

describe('Card source bans', () => {
  const read = (file: string) => readFileSync(resolve(UI_DIR, file), 'utf8')

  it('never adds a PII or session-replay marker', () => {
    for (const file of ['Card.tsx', 'StatCard.tsx']) {
      expect({ file, source: read(file) }).toEqual({
        file,
        source: expect.not.stringMatching(/data-ph-mask|ph-mask|maskValue/),
      })
    }
  })

  it('never performs currency formatting or arithmetic', () => {
    for (const file of ['Card.tsx', 'StatCard.tsx']) {
      expect({ file, source: read(file) }).toEqual({
        file,
        source: expect.not.stringMatching(/Intl\.NumberFormat|toLocaleString|toFixed|formatCurrency/),
      })
    }
  })
})