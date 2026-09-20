import { act, render, screen } from '@testing-library/react-native'
import { StyleSheet } from 'react-native'
import type { ReactElement } from 'react'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { ThemeProvider } from '../../theme/ThemeProvider'
import {
  PRIMITIVE_REGISTRY,
  type ContrastRatioClass,
} from '../../components/ui'
import {
  Alert,
} from '../../components/ui/Alert'
import { ActionSheet } from '../../components/ui/ActionSheet'
import { BackButton } from '../../components/ui/BackButton'
import { Box } from '../../components/ui/Box'
import { Button } from '../../components/ui/Button'
import { Card } from '../../components/ui/Card'
import { Checkbox } from '../../components/ui/Checkbox'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog'
import { Dialog } from '../../components/ui/Dialog'
import { EmptyState } from '../../components/ui/EmptyState'
import { ErrorState } from '../../components/ui/ErrorState'
import { FormField } from '../../components/ui/FormField'
import { IconButton } from '../../components/ui/IconButton'
import { Inline } from '../../components/ui/Inline'
import { Label } from '../../components/ui/Label'
import { List } from '../../components/ui/List'
import { ListItem } from '../../components/ui/ListItem'
import { ProgressBar } from '../../components/ui/ProgressBar'
import { Screen } from '../../components/ui/Screen'
import { ScreenHeader } from '../../components/ui/ScreenHeader'
import { SectionHeader } from '../../components/ui/SectionHeader'
import { Separator } from '../../components/ui/Separator'
import { Skeleton } from '../../components/ui/Skeleton'
import { Spinner } from '../../components/ui/Spinner'
import { Stack } from '../../components/ui/Stack'
import { StatCard } from '../../components/ui/StatCard'
import { Surface } from '../../components/ui/Surface'
import { Switch } from '../../components/ui/Switch'
import { TabBar } from '../../components/ui/TabBar'
import { Text } from '../../components/ui/Text'
import { TextArea } from '../../components/ui/TextArea'
import { TextField } from '../../components/ui/TextField'
import { ThemeSelector } from '../../components/ui/ThemeSelector'
import { Toast } from '../../components/ui/Toast'

export const COLOR_SCHEMES = ['light', 'dark'] as const
export type ColorScheme = (typeof COLOR_SCHEMES)[number]

// ── contrast, computed in pure JS from the generated adapter ─────────────────────────────────────

type RGB = [number, number, number]

const WHITE: RGB = [255, 255, 255]

function parseHex(value: string): { rgb: RGB; alpha: number } {
  const hex = value.trim().replace(/^#/, '').toLowerCase()
  const expanded = hex.length === 3 ? hex.split('').map((c) => c + c).join('') : hex
  if (!/^[0-9a-f]{6}([0-9a-f]{2})?$/.test(expanded)) {
    throw new Error(`not a hex colour: ${value}`)
  }
  return {
    rgb: [
      parseInt(expanded.slice(0, 2), 16),
      parseInt(expanded.slice(2, 4), 16),
      parseInt(expanded.slice(4, 6), 16),
    ],
    alpha: expanded.length === 8 ? parseInt(expanded.slice(6, 8), 16) / 255 : 1,
  }
}

function composite(foreground: RGB, alpha: number, background: RGB): RGB {
  return foreground.map((channel, index) =>
    Math.round(channel * alpha + background[index] * (1 - alpha)),
  ) as RGB
}

function relativeLuminance([r, g, b]: RGB): number {
  const linear = (channel: number) => {
    const value = channel / 255
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b)
}

/**
 * WCAG 2.1 contrast ratio, computed in pure JS. A translucent surface is composited over white and
 * a translucent foreground over that surface, so an alpha token is measured rather than ignored.
 */
export function contrastRatio(foreground: string, background: string): number {
  const fg = parseHex(foreground)
  const bg = parseHex(background)
  const bgRgb = bg.alpha < 1 ? composite(bg.rgb, bg.alpha, WHITE) : bg.rgb
  const fgRgb = fg.alpha < 1 ? composite(fg.rgb, fg.alpha, bgRgb) : fg.rgb
  const lighter = Math.max(relativeLuminance(fgRgb), relativeLuminance(bgRgb))
  const darker = Math.min(relativeLuminance(fgRgb), relativeLuminance(bgRgb))
  return (lighter + 0.05) / (darker + 0.05)
}

/** WCAG 2.1 AA floors: 4.5:1 for body text, 3:1 for large text and UI boundaries. */
export const CONTRAST_FLOORS: Record<ContrastRatioClass, number> = {
  body: 4.5,
  large: 3,
  ui: 3,
}

/**
 * Resolve a token name from the generated adapter's `color` OR `contrast` section. The
 * `*-text` / `*-strong` remediation tokens live in `contrast`, so both sections are searched.
 * A name that resolves nowhere THROWS rather than silently measuring `undefined`.
 */
export function resolveToken(scheme: ColorScheme, token: string): string {
  const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<
    string,
    string
  >
  const value = section[token]
  if (value === undefined) throw new Error(`token "${token}" is not defined for ${scheme}`)
  return value
}

export interface ContrastPair {
  /** The registry entry that produced the pair. */
  entry: string
  scheme: ColorScheme
  foregroundToken: string
  backgroundToken: string
  ratioClass: ContrastRatioClass
  floor: number
  ratio: number
}

/**
 * One pair per registry entry, per theme — the extraction rule from the plan. The pair set is
 * DERIVED from the registry, never hand-listed, so a new primitive automatically joins the gate.
 */
export function derivedContrastPairs(): ContrastPair[] {
  const pairs: ContrastPair[] = []
  for (const scheme of COLOR_SCHEMES) {
    for (const entry of PRIMITIVE_REGISTRY) {
      pairs.push({
        entry: entry.name,
        scheme,
        foregroundToken: entry.foregroundToken,
        backgroundToken: entry.backgroundToken,
        ratioClass: entry.ratioClass,
        floor: CONTRAST_FLOORS[entry.ratioClass],
        ratio: contrastRatio(
          resolveToken(scheme, entry.foregroundToken),
          resolveToken(scheme, entry.backgroundToken),
        ),
      })
    }
  }
  return pairs
}

/**
 * The four pairs the additive contrast remediation exists to fix. They are a MANDATORY enumerated
 * assertion set on top of the derived pairs: the derived set could stop exercising them if a
 * registry entry were re-pointed, so these are pinned by token name and asserted at >= 4.5:1 in the
 * acceptance. They must NEVER be allowlisted.
 */
export const MANDATORY_REMEDIATED_PAIRS: ReadonlyArray<{
  name: string
  scheme: ColorScheme
  foregroundToken: string
  backgroundToken: string
}> = [
  { name: 'light info-text on background', scheme: 'light', foregroundToken: 'bb-info-text', backgroundToken: 'background' },
  { name: 'light info-foreground on info-strong', scheme: 'light', foregroundToken: 'info-foreground', backgroundToken: 'bb-info-strong' },
  { name: 'dark info-foreground on info-strong', scheme: 'dark', foregroundToken: 'info-foreground', backgroundToken: 'bb-info-strong' },
  { name: 'dark destructive-foreground on danger-strong', scheme: 'dark', foregroundToken: 'destructive-foreground', backgroundToken: 'bb-danger-strong' },
]

/**
 * Documented DECORATIVE pairs only — a pair that carries no body text. It is empty because every
 * derived pair currently clears its floor. It must never be used to skip a pair that renders body
 * text: the four remediated pairs above are asserted at >= 4.5:1, not allowlisted.
 */
export const DECORATIVE_ALLOWLIST: ReadonlyArray<{ entry: string; scheme: ColorScheme; reason: string }> = []

// ── the primitive table (criterion 1, 7 and 9 iterate it) ────────────────────────────────────────

type TestNode = ReturnType<typeof screen.getByTestId>

export interface PrimitiveCase {
  /** The component identity, for the "every interactive registry component is covered" check. */
  component: unknown
  name: string
  /** Root render (one per registry component) or a nested interactive affordance. */
  kind: 'root' | 'affordance'
  interactive: boolean
  /** Explicit `accessibilityRole`, or null when the primitive relies on an implicit native role. */
  role: string | null
  /** Host component type asserted when `role` is null (e.g. a `TextInput`). */
  implicitHostType?: string
  /** Explicit accessible name asserted when defined. */
  accessibleName?: string
  /** Visible text that supplies the accessible name when no explicit label is set. */
  childText?: string
  /** Icon-only / square affordances must also meet the 44px `minWidth`. */
  requiresMinWidth?: boolean
  render: () => ReactElement
  /** Locates the interactive node after render. */
  query?: () => TestNode
  /** Locates the root node for the style walk. */
  testID?: string
}

const byTestId = (testID: string) => () => screen.getByTestId(testID)

export const PRIMITIVE_CASES: PrimitiveCase[] = [
  // typography / layout / surface
  { component: Text, name: 'Text', kind: 'root', interactive: false, role: null, testID: 'c-text', render: () => <Text testID="c-text">Body</Text> },
  { component: Surface, name: 'Surface', kind: 'root', interactive: false, role: null, testID: 'c-surface', render: () => <Surface testID="c-surface" /> },
  { component: Box, name: 'Box', kind: 'root', interactive: false, role: null, testID: 'c-box', render: () => <Box testID="c-box" /> },
  { component: Stack, name: 'Stack', kind: 'root', interactive: false, role: null, testID: 'c-stack', render: () => <Stack testID="c-stack" /> },
  { component: Inline, name: 'Inline', kind: 'root', interactive: false, role: null, testID: 'c-inline', render: () => <Inline testID="c-inline" /> },

  // actions
  { component: Button, name: 'Button', kind: 'root', interactive: true, role: 'button', accessibleName: 'Save', testID: 'c-button', render: () => <Button testID="c-button" label="Save" /> },
  { component: IconButton, name: 'IconButton', kind: 'root', interactive: true, role: 'button', accessibleName: 'Close', requiresMinWidth: true, testID: 'c-icon-button', render: () => <IconButton testID="c-icon-button" label="Close" /> },
  { component: BackButton, name: 'BackButton', kind: 'root', interactive: true, role: 'button', accessibleName: 'Go back', requiresMinWidth: true, testID: 'c-back-button', render: () => <BackButton testID="c-back-button" onPress={() => {}} /> },

  // surfaces / cards
  { component: Card, name: 'Card (pressable)', kind: 'root', interactive: true, role: 'button', childText: 'Card body', testID: 'c-card', render: () => (<Card testID="c-card" onPress={() => {}}><Text>Card body</Text></Card>) },
  { component: StatCard, name: 'StatCard', kind: 'root', interactive: false, role: null, testID: 'c-stat-card', render: () => <StatCard testID="c-stat-card" label="Balance" value="$1.00" /> },

  // fields
  { component: TextField, name: 'TextField', kind: 'root', interactive: true, role: null, implicitHostType: 'TextInput', accessibleName: 'Email', testID: 'c-text-field', render: () => (<FormField label="Email"><TextField testID="c-text-field" /></FormField>) },
  { component: TextArea, name: 'TextArea', kind: 'root', interactive: true, role: null, implicitHostType: 'TextInput', accessibleName: 'Notes', testID: 'c-text-area', render: () => (<FormField label="Notes"><TextArea testID="c-text-area" /></FormField>) },
  { component: Label, name: 'Label', kind: 'root', interactive: false, role: null, testID: 'c-label', render: () => <Label testID="c-label">Email</Label> },
  { component: FormField, name: 'FormField', kind: 'root', interactive: false, role: null, testID: 'c-form-field', render: () => (<FormField testID="c-form-field" label="Email"><TextField /></FormField>) },
  { component: Switch, name: 'Switch', kind: 'root', interactive: true, role: 'switch', accessibleName: 'Notify', requiresMinWidth: true, testID: 'c-switch', render: () => <Switch testID="c-switch" value={false} label="Notify" /> },
  { component: Checkbox, name: 'Checkbox', kind: 'root', interactive: true, role: 'checkbox', accessibleName: 'Agree', requiresMinWidth: true, testID: 'c-checkbox', render: () => <Checkbox testID="c-checkbox" checked={false} label="Agree" /> },

  // feedback
  { component: Alert, name: 'Alert', kind: 'root', interactive: false, role: 'alert', testID: 'c-alert', render: () => (<Alert testID="c-alert" onDismiss={() => {}}>body</Alert>) },
  { component: Alert, name: 'Alert dismiss', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'Dismiss', requiresMinWidth: true, query: () => screen.getByLabelText('Dismiss'), render: () => (<Alert testID="c-alert-aff" onDismiss={() => {}}>body</Alert>) },
  { component: Spinner, name: 'Spinner', kind: 'root', interactive: false, role: 'progressbar', testID: 'c-spinner', render: () => <Spinner testID="c-spinner" label="Loading" /> },
  { component: ProgressBar, name: 'ProgressBar', kind: 'root', interactive: false, role: 'progressbar', testID: 'c-progress-bar', render: () => <ProgressBar testID="c-progress-bar" value={0.5} /> },
  { component: Skeleton, name: 'Skeleton', kind: 'root', interactive: false, role: null, testID: 'c-skeleton', render: () => <Skeleton testID="c-skeleton" /> },
  { component: Toast, name: 'Toast', kind: 'root', interactive: false, role: 'alert', testID: 'c-toast', render: () => (<Toast testID="c-toast" message="Saved" onDismiss={() => {}} />) },
  { component: Toast, name: 'Toast dismiss', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'Dismiss', requiresMinWidth: true, query: () => screen.getByLabelText('Dismiss'), render: () => (<Toast testID="c-toast-aff" message="Saved" onDismiss={() => {}} />) },
  { component: Toast, name: 'Toast action', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'Undo', query: () => screen.getByLabelText('Undo'), render: () => (<Toast testID="c-toast-aff2" message="Saved" action={{ label: 'Undo', onPress: () => {} }} />) },

  // lists
  { component: List, name: 'List', kind: 'root', interactive: false, role: 'list', testID: 'c-list', render: () => <List testID="c-list" /> },
  { component: ListItem, name: 'ListItem (pressable)', kind: 'root', interactive: true, role: 'button', childText: 'Row', testID: 'c-list-item', render: () => (<ListItem testID="c-list-item" title="Row" onPress={() => {}} />) },
  { component: SectionHeader, name: 'SectionHeader', kind: 'root', interactive: false, role: null, testID: 'c-section-header', render: () => <SectionHeader testID="c-section-header" title="Section" /> },
  { component: Separator, name: 'Separator', kind: 'root', interactive: false, role: null, testID: 'c-separator', render: () => <Separator testID="c-separator" /> },

  // states
  { component: EmptyState, name: 'EmptyState', kind: 'root', interactive: false, role: null, testID: 'c-empty-state', render: () => <EmptyState testID="c-empty-state" title="Nothing here" /> },
  { component: ErrorState, name: 'ErrorState', kind: 'root', interactive: false, role: null, testID: 'c-error-state', render: () => (<ErrorState testID="c-error-state" title="Oops" description="Try again" onRetry={() => {}} detail="Detail" />) },
  { component: ErrorState, name: 'ErrorState retry', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'Retry', query: byTestId('c-error-state-aff-retry'), render: () => (<ErrorState testID="c-error-state-aff" title="Oops" description="Try again" onRetry={() => {}} />) },
  { component: ErrorState, name: 'ErrorState detail toggle', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'Show details', query: byTestId('c-error-state-aff2-detail-toggle'), render: () => (<ErrorState testID="c-error-state-aff2" title="Oops" description="Try again" onRetry={() => {}} detail="Detail" />) },

  // overlays
  { component: Dialog, name: 'Dialog', kind: 'root', interactive: false, role: null, testID: 'c-dialog', render: () => (<Dialog visible onRequestClose={() => {}} title="Title" testID="c-dialog" />) },
  { component: ConfirmDialog, name: 'ConfirmDialog', kind: 'root', interactive: false, role: null, testID: 'c-confirm-dialog', render: () => (<ConfirmDialog visible title="Title" message="Message" confirmLabel="Save" onConfirm={() => {}} onCancel={() => {}} testID="c-confirm-dialog" />) },
  { component: ConfirmDialog, name: 'ConfirmDialog confirm', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'Save', query: byTestId('c-confirm-dialog-aff-confirm'), render: () => (<ConfirmDialog visible title="Title" message="Message" confirmLabel="Save" onConfirm={() => {}} onCancel={() => {}} testID="c-confirm-dialog-aff" />) },
  { component: ConfirmDialog, name: 'ConfirmDialog cancel', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'Cancel', query: byTestId('c-confirm-dialog-aff2-cancel'), render: () => (<ConfirmDialog visible title="Title" message="Message" confirmLabel="Save" onConfirm={() => {}} onCancel={() => {}} testID="c-confirm-dialog-aff2" />) },
  { component: ActionSheet, name: 'ActionSheet', kind: 'root', interactive: false, role: null, testID: 'c-action-sheet', render: () => (<ActionSheet visible title="Title" actions={[{ label: 'A', onPress: () => {} }]} onCancel={() => {}} testID="c-action-sheet" />) },
  { component: ActionSheet, name: 'ActionSheet action', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'A', query: byTestId('c-action-sheet-aff-action-0'), render: () => (<ActionSheet visible actions={[{ label: 'A', onPress: () => {} }]} onCancel={() => {}} testID="c-action-sheet-aff" />) },
  { component: ActionSheet, name: 'ActionSheet cancel', kind: 'affordance', interactive: true, role: 'button', accessibleName: 'Cancel', query: byTestId('c-action-sheet-aff2-cancel'), render: () => (<ActionSheet visible actions={[{ label: 'A', onPress: () => {} }]} onCancel={() => {}} testID="c-action-sheet-aff2" />) },

  // navigation
  { component: Screen, name: 'Screen', kind: 'root', interactive: false, role: null, testID: 'c-screen', render: () => <Screen testID="c-screen" /> },
  { component: ScreenHeader, name: 'ScreenHeader', kind: 'root', interactive: false, role: null, testID: 'c-screen-header', render: () => <ScreenHeader testID="c-screen-header" title="Home" /> },
  { component: TabBar, name: 'TabBar', kind: 'root', interactive: false, role: 'tablist', testID: 'c-tab-bar', render: () => (<TabBar testID="c-tab-bar" items={[{ key: 'home', label: 'Home' }]} />) },
  { component: TabBar, name: 'TabBar item', kind: 'affordance', interactive: true, role: 'tab', accessibleName: 'Home', requiresMinWidth: true, query: byTestId('c-tab-bar-aff-item-home'), render: () => (<TabBar testID="c-tab-bar-aff" items={[{ key: 'home', label: 'Home' }]} />) },

  // theme
  { component: ThemeSelector, name: 'ThemeSelector', kind: 'root', interactive: false, role: 'radiogroup', testID: 'c-theme-selector', render: () => <ThemeSelector testID="c-theme-selector" /> },
  { component: ThemeSelector, name: 'ThemeSelector option', kind: 'affordance', interactive: true, role: 'radio', accessibleName: 'Light', query: byTestId('c-theme-selector-aff-light'), render: () => <ThemeSelector testID="c-theme-selector-aff" /> },
]

/** Render a case inside the theme provider and flush the async preference hydration. */
export async function renderCase(item: PrimitiveCase): Promise<void> {
  render(<ThemeProvider>{item.render()}</ThemeProvider>)
  await act(async () => {})
}

/** Every style object in the rendered tree, flattened. */
export function collectStyles(): Record<string, unknown>[] {
  const styles: Record<string, unknown>[] = []
  const walk = (node: unknown): void => {
    if (!node || typeof node !== 'object') return
    const element = node as { props?: { style?: unknown }; children?: unknown }
    const flat = element.props?.style === undefined ? undefined : StyleSheet.flatten(element.props.style)
    if (flat) styles.push(flat as Record<string, unknown>)
    const children = element.children
    if (Array.isArray(children)) children.forEach(walk)
  }
  walk(screen.toJSON())
  return styles
}

/**
 * Representative native dimensions. `320x568` is the narrowest common phone still in the support
 * matrix (iPhone SE 1st gen / older Android), so it is the width every primitive must survive;
 * `390x844` is the current standard iPhone; `834x1112` is a tablet. See `device-checks.md`.
 */
export const DIMENSION_PROFILES: ReadonlyArray<{ name: string; width: number; height: number }> = [
  { name: 'small phone', width: 320, height: 568 },
  { name: 'standard phone', width: 390, height: 844 },
  { name: 'tablet', width: 834, height: 1112 },
]

/** The interactive components the registry marks interactive, for the coverage cross-check. */
export function registryInteractiveComponents(): unknown[] {
  return [...new Set(PRIMITIVE_REGISTRY.filter((entry) => entry.interactive).map((entry) => entry.component))]
}

/** The unique components the registry references, for the criterion-9 render walk. */
export function registryComponents(): unknown[] {
  return [...new Set(PRIMITIVE_REGISTRY.map((entry) => entry.component))]
}
