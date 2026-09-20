import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fireEvent, render, screen, within } from '@testing-library/react-native'
import { Text, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { List } from '../../components/ui/List'
import { ListItem, type ListItemRole } from '../../components/ui/ListItem'
import { Separator, type SeparatorOrientation } from '../../components/ui/Separator'
import { SectionHeader } from '../../components/ui/SectionHeader'
import { LIST_REGISTRY } from '../../components/ui/list.registry'
import { setColorScheme } from '../color-scheme'
import {
  COLOR_SCHEMES,
  allowedColors,
  collectHexColors,
  resolvedColor,
  resolvedStyle,
  runtimeTokenColors,
} from './helpers'

const TOUCH_MIN = nativeTokens.light.touchTarget.min
const SPACING = nativeTokens.light.spacing

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))
const UI_DIR = resolve(MOBILE_ROOT, 'src/components/ui')

describe('ListItem pressability', () => {
  it('exposes a button role and the touch floor when onPress is present', () => {
    const onPress = jest.fn()
    render(<ListItem testID="row" title="Checking" onPress={onPress} />)

    const node = screen.getByTestId('row')
    expect(node.props.accessibilityRole).toBe('button')
    expect(resolvedStyle('row').minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)

    fireEvent.press(node)
    expect(onPress).toHaveBeenCalledTimes(1)
  })

  it('exposes a link role when the row navigates', () => {
    render(<ListItem testID="row-link" title="Open" onPress={() => {}} role="link" />)

    expect(screen.getByTestId('row-link').props.accessibilityRole).toBe('link')
  })

  it('maps selected to accessibilityState.selected', () => {
    render(<ListItem testID="row-selected" title="Checking" onPress={() => {}} selected />)

    expect(screen.getByTestId('row-selected').props.accessibilityState).toMatchObject({
      selected: true,
    })
  })

  it('blocks onPress when disabled', () => {
    const onPress = jest.fn()
    render(<ListItem testID="row-disabled" title="Checking" onPress={onPress} disabled />)

    fireEvent.press(screen.getByTestId('row-disabled'))
    expect(onPress).not.toHaveBeenCalled()
  })

  it('is a plain row with no button role when onPress is absent', () => {
    render(<ListItem testID="row-static" title="Checking" />)

    const node = screen.getByTestId('row-static')
    expect(node.props.accessibilityRole).toBeUndefined()
    expect(resolvedStyle('row-static').minHeight).toBeUndefined()
  })

  it('renders title, subtitle, leading and trailing content', () => {
    render(
      <ListItem
        testID="row-full"
        title="Checking"
        subtitle="Everyday account"
        leading={<Text>L</Text>}
        trailing={<Text>T</Text>}
      />,
    )

    expect(screen.getByText('Checking')).toBeTruthy()
    expect(screen.getByText('Everyday account')).toBeTruthy()
    expect(screen.getByText('L')).toBeTruthy()
    expect(screen.getByText('T')).toBeTruthy()
  })

  it('shows the focus ring on a pressable row and clears it on blur', () => {
    render(<ListItem testID="row-focus" title="Checking" onPress={() => {}} />)

    fireEvent(screen.getByTestId('row-focus'), 'focus')
    expect(resolvedStyle('row-focus').outlineWidth).toBe(
      nativeTokens.light.focus['bb-focus-ring-width'],
    )

    fireEvent(screen.getByTestId('row-focus'), 'blur')
    expect(resolvedStyle('row-focus').outlineWidth).toBeUndefined()
  })
})

describe('Separator', () => {
  it('renders a horizontal rule by default', () => {
    render(<Separator testID="sep-h" />)

    expect(resolvedStyle('sep-h').height).toBe(1)
    expect(resolvedStyle('sep-h').width).toBe('100%')
  })

  it('renders a vertical rule when orientation is vertical', () => {
    render(<Separator testID="sep-v" orientation="vertical" />)

    expect(resolvedStyle('sep-v').width).toBe(1)
  })

  it('insets a horizontal rule against the spacing scale', () => {
    render(<Separator testID="sep-inset" inset={4} />)

    expect(resolvedStyle('sep-inset').marginInline).toBe(SPACING['spacing-bb-4'])
  })

  it('insets a vertical rule against the spacing scale', () => {
    render(<Separator testID="sep-v-inset" orientation="vertical" inset={2} />)

    expect(resolvedStyle('sep-v-inset').marginBlock).toBe(SPACING['spacing-bb-2'])
  })

  it('applies no inset margin when inset is omitted', () => {
    render(<Separator testID="sep-plain" />)

    expect(resolvedStyle('sep-plain').marginInline).toBeUndefined()
    expect(resolvedStyle('sep-plain').marginBlock).toBeUndefined()
  })
})

describe('SectionHeader', () => {
  it('renders its title as a header', () => {
    render(<SectionHeader testID="section" title="Accounts" />)

    expect(screen.getByText('Accounts')).toBeTruthy()
    expect(screen.getByText('Accounts').props.accessibilityRole).toBe('header')
  })

  it('renders an optional action slot', () => {
    render(<SectionHeader testID="section-action" title="Accounts" action={<Text>See all</Text>} />)

    expect(screen.getByText('See all')).toBeTruthy()
  })
})

describe('List', () => {
  it('renders its children inside a list role', () => {
    render(
      <List testID="list">
        <ListItem testID="list-row" title="Checking" />
      </List>,
    )

    expect(screen.getByTestId('list').props.accessibilityRole).toBe('list')
    expect(screen.getByText('Checking')).toBeTruthy()
  })

  it('wraps every child in a role="listitem" so the rendered list is structurally valid', () => {
    render(
      <List testID="list-items">
        <ListItem testID="list-items-row" title="Checking" onPress={() => {}} />
        <ListItem testID="list-items-row-2" title="Savings" />
      </List>,
    )

    const list = screen.getByTestId('list-items')
    const wrappers = list.children.filter(
      (child): child is typeof list =>
        typeof child !== 'string' && child.props.role === 'listitem',
    )
    expect(wrappers).toHaveLength(2)
    expect(within(wrappers[0]).getByTestId('list-items-row')).toBeTruthy()
    expect(within(wrappers[1]).getByTestId('list-items-row-2')).toBeTruthy()
  })
})

describe('LIST_REGISTRY', () => {
  it('has one entry per text-bearing role', () => {
    for (const name of [
      'ListItem.title',
      'ListItem.subtitle',
      'ListItem.title.selected',
      'SectionHeader.title',
    ]) {
      expect(LIST_REGISTRY.find((entry) => entry.name === name)).toBeDefined()
    }
    expect(LIST_REGISTRY).toHaveLength(4)
  })

  it('resolves every foreground and background token in both themes', () => {
    for (const entry of LIST_REGISTRY) {
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

  it('references the ListItem and SectionHeader primitives', () => {
    expect(LIST_REGISTRY.find((entry) => entry.name === 'ListItem.title')?.component).toBe(ListItem)
    expect(LIST_REGISTRY.find((entry) => entry.name === 'SectionHeader.title')?.component).toBe(
      SectionHeader,
    )
  })

  it('pins the selected row to the accent surface', () => {
    expect(LIST_REGISTRY.find((entry) => entry.name === 'ListItem.title.selected')).toMatchObject({
      foregroundToken: 'accent-foreground',
      backgroundToken: 'accent',
      ratioClass: 'body',
    })
  })
})

describe('List colour provenance', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`renders only token colours (${scheme})`, () => {
      const allowed = allowedColors()
      setColorScheme(scheme)
      render(
        <View>
          <SectionHeader testID={`prov-section-${scheme}`} title="Accounts" />
          <ListItem
            testID={`prov-row-${scheme}`}
            title="Checking"
            subtitle="Everyday account"
            onPress={() => {}}
          />
          <ListItem
            testID={`prov-row-selected-${scheme}`}
            title="Checking"
            subtitle="Everyday account"
            onPress={() => {}}
            selected
          />
        </View>,
      )

      const ids = [
        `prov-section-${scheme}`,
        `prov-section-${scheme}-title`,
        `prov-row-${scheme}`,
        `prov-row-${scheme}-title`,
        `prov-row-${scheme}-subtitle`,
        `prov-row-selected-${scheme}`,
        `prov-row-selected-${scheme}-title`,
        `prov-row-selected-${scheme}-subtitle`,
      ]

      for (const id of ids) {
        for (const color of collectHexColors(resolvedStyle(id))) {
          expect({ id, color, allowed: allowed.has(color) }).toEqual({ id, color, allowed: true })
        }
      }
    })
  }

  it('paints the selected row with the accent surface', () => {
    for (const scheme of COLOR_SCHEMES) {
      const runtime = runtimeTokenColors()[scheme]
      setColorScheme(scheme)
      render(<ListItem testID={`sel-${scheme}`} title="Checking" onPress={() => {}} selected />)

      expect(resolvedColor(`sel-${scheme}`, 'backgroundColor')).toBe(runtime.accent)
    }
  })
})

describe('List source bans', () => {
  const read = (file: string) => readFileSync(resolve(UI_DIR, file), 'utf8')
  const FILES = ['List.tsx', 'ListItem.tsx', 'Separator.tsx', 'SectionHeader.tsx']

  it('never adds a PII or session-replay marker', () => {
    for (const file of FILES) {
      expect({ file, source: read(file) }).toEqual({
        file,
        source: expect.not.stringMatching(/data-ph-mask|ph-mask|maskValue/),
      })
    }
  })

  it('never hand-rolls a table', () => {
    for (const file of FILES) {
      expect({ file, source: read(file) }).toEqual({
        file,
        source: expect.not.stringMatching(/<table|<Table|DataTable/),
      })
    }
  })

  it('never uses a `dark:` utility', () => {
    for (const file of FILES) {
      expect({ file, source: read(file) }).toEqual({
        file,
        source: expect.not.stringMatching(/dark:/),
      })
    }
  })
})

// Keep the exported union types referenced so a rename is a compile error, not a silent drift.
const ROLES: readonly ListItemRole[] = ['button', 'link']
const ORIENTATIONS: readonly SeparatorOrientation[] = ['horizontal', 'vertical']
void ROLES
void ORIENTATIONS