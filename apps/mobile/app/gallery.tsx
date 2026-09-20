import { useState, type ReactNode } from 'react'
import { VariableContextProvider } from 'nativewind'
import { ScrollView, View } from 'react-native'
import {
  ActionSheet,
  Button,
  CardDescription,
  CardHeader,
  CardTitle,
  ConfirmDialog,
  Dialog,
  IconButton,
  Inline,
  List,
  ListItem,
  PRIMITIVE_REGISTRY,
  ProgressBar,
  Separator,
  Skeleton,
  Spinner,
  Text,
  TextField,
  type PrimitiveRegistryEntry,
} from '../src/components/ui'
import { themeVariables, type ResolvedTheme } from '../src/theme/runtime'

/**
 * The gallery is a fixture-only review surface: every primitive in the registry, rendered with
 * fixture props in a light section and a dark section, at a phone width. It fetches nothing, reads
 * no environment, and renders no account, transaction or balance. It is reachable by route only —
 * nothing links to it from the production home screen — and exists so the Playwright visual/axe
 * layer (and a human reviewer) can see every primitive at once.
 *
 * The registry is the source of truth: the sections iterate `PRIMITIVE_REGISTRY` directly, so a new
 * primitive cannot be added without appearing here.
 */

/** A phone-width column so the sections render at the shape they ship at, even on a desktop. */
const PHONE_WIDTH = 390

const noop = () => undefined

const SECTION_STYLE = { width: '100%', maxWidth: PHONE_WIDTH, alignSelf: 'center' } as const

const CARD_BODY: ReactNode = (
  <CardHeader>
    <CardTitle>Card title</CardTitle>
    <CardDescription>Card description</CardDescription>
  </CardHeader>
)

const DIALOG_ACTIONS = [
  { label: 'Share', onPress: noop },
  { label: 'Delete', onPress: noop, destructive: true },
]

const TAB_ITEMS = [
  { key: 'home', label: 'Home' },
  { key: 'reports', label: 'Reports' },
]

/**
 * Fixture props per registry entry. Entries whose component has no required props (or whose
 * default rendering already demonstrates the tone) fall back to a generic child label, so a new
 * registry entry is still rendered without a fixture — the gallery cannot silently fall behind.
 *
 * ⚠ Every value here is a fixture. `StatCard`'s `"$1,234.56"` is deliberately a sample string, not
 * a balance read from anywhere.
 */
const GALLERY_FIXTURES: Record<string, Record<string, unknown>> = {
  'Text.default': { children: 'Body text', tone: 'default' },
  'Text.muted': { children: 'Muted text', tone: 'muted' },
  'Text.subtle': { children: 'Subtle text', tone: 'subtle' },
  'Text.primary': { children: 'Primary text', tone: 'primary' },
  'Text.success': { children: 'Success text', tone: 'success' },
  'Text.warning': { children: 'Warning text', tone: 'warning' },
  'Text.danger': { children: 'Danger text', tone: 'danger' },
  'Text.info': { children: 'Info text', tone: 'info' },

  'Surface.canvas': { variant: 'canvas', children: <Text>Canvas surface</Text> },
  'Surface.card': { variant: 'card', children: <Text>Card surface</Text> },
  'Surface.raised': { variant: 'raised', children: <Text>Raised surface</Text> },
  'Surface.sunken': { variant: 'sunken', children: <Text>Sunken surface</Text> },
  'Surface.overlay': { variant: 'overlay', children: <Text>Overlay surface</Text> },

  Box: { p: 4, children: <Text>Box</Text> },
  Stack: { gap: 2, children: <Text>Stack</Text> },
  Inline: { gap: 2, children: <Text>Inline</Text> },

  'Button.primary': { label: 'Primary', variant: 'primary' },
  'Button.secondary': { label: 'Secondary', variant: 'secondary' },
  'Button.outline': { label: 'Outline', variant: 'outline' },
  'Button.ghost': { label: 'Ghost', variant: 'ghost' },
  'Button.destructive': { label: 'Delete', variant: 'destructive' },
  'Button.link': { label: 'Link', variant: 'link' },

  'Card.default.title': { variant: 'default', children: CARD_BODY },
  'Card.default.description': { variant: 'default', children: CARD_BODY },
  'Card.outlined.title': { variant: 'outlined', children: CARD_BODY },
  'Card.outlined.description': { variant: 'outlined', children: CARD_BODY },
  'Card.elevated.title': { variant: 'elevated', children: CARD_BODY },
  'Card.elevated.description': { variant: 'elevated', children: CARD_BODY },
  'Card.sunken.title': { variant: 'sunken', children: CARD_BODY },
  'Card.sunken.description': { variant: 'sunken', children: CARD_BODY },

  'StatCard.label': { label: 'Net worth', value: '$1,234.56' },
  'StatCard.value': { label: 'Net worth', value: '$1,234.56' },
  'StatCard.delta.up': { label: 'Income', value: '$1,234.56', delta: '+$12.00', trend: 'up' },
  'StatCard.delta.down': { label: 'Spending', value: '$1,234.56', delta: '-$12.00', trend: 'down' },
  'StatCard.delta.flat': { label: 'Balance', value: '$1,234.56', delta: '$0.00', trend: 'flat' },

  'Dialog.title': {
    visible: false,
    onRequestClose: noop,
    title: 'Dialog title',
    description: 'Dialog description',
  },
  'Dialog.description': {
    visible: false,
    onRequestClose: noop,
    title: 'Dialog title',
    description: 'Dialog description',
  },

  'ConfirmDialog.title': {
    visible: false,
    title: 'Confirm action',
    message: 'This action cannot be undone.',
    confirmLabel: 'Confirm',
    onConfirm: noop,
    onCancel: noop,
  },
  'ConfirmDialog.message': {
    visible: false,
    title: 'Confirm action',
    message: 'This action cannot be undone.',
    confirmLabel: 'Confirm',
    onConfirm: noop,
    onCancel: noop,
  },
  'ConfirmDialog.confirm': {
    visible: false,
    title: 'Confirm action',
    message: 'This action cannot be undone.',
    confirmLabel: 'Confirm',
    onConfirm: noop,
    onCancel: noop,
  },
  'ConfirmDialog.confirmDestructive': {
    visible: false,
    title: 'Delete item',
    message: 'This action cannot be undone.',
    confirmLabel: 'Delete',
    destructive: true,
    onConfirm: noop,
    onCancel: noop,
  },
  'ConfirmDialog.cancel': {
    visible: false,
    title: 'Confirm action',
    message: 'This action cannot be undone.',
    confirmLabel: 'Confirm',
    onConfirm: noop,
    onCancel: noop,
  },

  'Alert.info': { variant: 'info', title: 'Alert title', children: <Text>Alert body</Text> },
  'Alert.success': { variant: 'success', title: 'Alert title', children: <Text>Alert body</Text> },
  'Alert.warning': { variant: 'warning', title: 'Alert title', children: <Text>Alert body</Text> },
  'Alert.danger': { variant: 'danger', title: 'Alert title', children: <Text>Alert body</Text> },

  'Label.default': { children: 'Label text' },
  'Label.required': { children: 'Label text', required: true },
  'Label.disabled': { children: 'Label text', disabled: true },

  'FormField.label': { label: 'Field label', children: <TextField onChangeText={noop} /> },
  'FormField.hint': {
    label: 'Field label',
    hint: 'Supporting hint',
    children: <TextField onChangeText={noop} />,
  },
  'FormField.error': {
    label: 'Field label',
    error: 'This field is required',
    children: <TextField onChangeText={noop} />,
  },

  'TextField.value': { value: 'Fixture value', onChangeText: noop },
  'TextField.placeholder': { placeholder: 'Placeholder', onChangeText: noop },
  'TextArea.value': { value: 'Fixture value', onChangeText: noop },
  'TextArea.placeholder': { placeholder: 'Placeholder', onChangeText: noop },
  'Switch.label': { value: true, onValueChange: noop, label: 'Switch label' },
  'Checkbox.label': { checked: true, onCheckedChange: noop, label: 'Checkbox label' },

  'ListItem.title': { title: 'List item title', onPress: noop },
  'ListItem.subtitle': { title: 'List item title', subtitle: 'List item subtitle', onPress: noop },
  'ListItem.title.selected': {
    title: 'Selected item',
    subtitle: 'Selected subtitle',
    onPress: noop,
    selected: true,
  },
  'SectionHeader.title': { title: 'Section header' },

  'ScreenHeader.title': { title: 'Screen header' },
  'TabBar.label': { items: TAB_ITEMS },
  'BackButton.label': { onPress: noop },

  'ActionSheet.title': { visible: false, title: 'Action sheet', actions: DIALOG_ACTIONS, onCancel: noop },
  'ActionSheet.action': { visible: false, title: 'Action sheet', actions: DIALOG_ACTIONS, onCancel: noop },
  'ActionSheet.actionDestructive': {
    visible: false,
    title: 'Action sheet',
    actions: DIALOG_ACTIONS,
    onCancel: noop,
  },
  'ActionSheet.cancel': { visible: false, title: 'Action sheet', actions: DIALOG_ACTIONS, onCancel: noop },

  'EmptyState.title': { title: 'Nothing here yet', description: 'Add something to get started' },
  'EmptyState.description': { title: 'Nothing here yet', description: 'Add something to get started' },
  'ErrorState.title': {
    title: 'Something went wrong',
    description: 'Try again in a moment',
    onRetry: noop,
  },
  'ErrorState.description': {
    title: 'Something went wrong',
    description: 'Try again in a moment',
    onRetry: noop,
  },
  'ErrorState.retry': {
    title: 'Something went wrong',
    description: 'Try again in a moment',
    onRetry: noop,
  },
  'ErrorState.detailToggle': {
    title: 'Something went wrong',
    description: 'Try again in a moment',
    onRetry: noop,
    detail: 'Additional fixture detail',
  },
  'ErrorState.detail': {
    title: 'Something went wrong',
    description: 'Try again in a moment',
    onRetry: noop,
    detail: 'Additional fixture detail',
  },

  'ThemeSelector.option': {},
  'ThemeSelector.optionSelected': {},

  'Toast.info': { message: 'Fixture toast', variant: 'info' },
  'Toast.success': { message: 'Fixture toast', variant: 'success' },
  'Toast.warning': { message: 'Fixture toast', variant: 'warning' },
  'Toast.danger': { message: 'Fixture toast', variant: 'danger' },
}

function fixtureFor(entry: PrimitiveRegistryEntry): Record<string, unknown> {
  return GALLERY_FIXTURES[entry.name] ?? { children: entry.name }
}

function renderEntry(entry: PrimitiveRegistryEntry, theme: ResolvedTheme) {
  const Component = entry.component
  const props = { ...fixtureFor(entry), testID: `gallery-${theme}-node-${entry.name}` }

  return (
    <View
      key={entry.name}
      testID={`gallery-${theme}-entry-${entry.name}`}
      className="gap-bb-1 rounded-lg border border-border p-bb-2"
    >
      <Text variant="caption" tone="muted">
        {entry.name}
      </Text>
      <Component {...props} />
    </View>
  )
}

/**
 * Primitives the contrast registry deliberately exempts because they render no text on a surface
 * (see `registry.test.ts`'s `NON_TEXT_BEARING_FILES`). They are rendered here so the visual/axe
 * surface is complete — notably the icon-only affordance and the animated feedback primitives.
 */
function ExemptPrimitives({ theme }: { theme: ResolvedTheme }) {
  return (
    <View testID={`gallery-${theme}-exempt`} className="gap-bb-3 rounded-lg border border-border p-bb-2">
      <Text variant="subtitle">Outside the contrast registry</Text>
      <Inline gap={2} wrap>
        <Button label="Small" size="sm" />
        <Button label="Medium" size="md" />
        <Button label="Large" size="lg" />
      </Inline>
      <Inline gap={2}>
        <IconButton label="Add" size="sm">
          <Text className="text-primary-foreground">+</Text>
        </IconButton>
        <IconButton label="Edit" size="md">
          <Text className="text-primary-foreground">~</Text>
        </IconButton>
        <IconButton label="Delete" size="lg">
          <Text className="text-primary-foreground">x</Text>
        </IconButton>
      </Inline>
      <Inline gap={3}>
        <Spinner size="sm" label="Loading small" />
        <Spinner size="md" label="Loading medium" />
        <Spinner size="lg" label="Loading large" />
      </Inline>
      <ProgressBar value={0.4} label="Progress" />
      <ProgressBar indeterminate label="Loading progress" />
      <Skeleton width="100%" height={16} />
      <Separator />
      <Separator orientation="vertical" inset={2} />
      <List variant="card">
        <ListItem title="Pressable row" subtitle="With a subtitle" onPress={noop} />
        <ListItem title="Selected row" onPress={noop} selected />
        <ListItem title="Plain row" />
      </List>
    </View>
  )
}

/**
 * Overlay primitives are modal, so they cannot be shown open alongside the rest of the gallery.
 * They are rendered closed in the registry loop above and opened here from a labelled trigger —
 * which is also the seam the Playwright focus-trap/restore pass (Todo 22e) drives.
 */
function OverlayShowcase({ theme }: { theme: ResolvedTheme }) {
  const [open, setOpen] = useState<'dialog' | 'confirm' | 'sheet' | null>(null)
  const close = () => setOpen(null)

  return (
    <View testID={`gallery-${theme}-overlays`} className="gap-bb-3 rounded-lg border border-border p-bb-2">
      <Text variant="subtitle">Overlays</Text>
      <Inline gap={2} wrap>
        <Button label="Open dialog" onPress={() => setOpen('dialog')} />
        <Button label="Open confirm" onPress={() => setOpen('confirm')} />
        <Button label="Open action sheet" onPress={() => setOpen('sheet')} />
      </Inline>

      <Dialog
        visible={open === 'dialog'}
        onRequestClose={close}
        title="Dialog title"
        description="Dialog description"
        testID={`gallery-${theme}-dialog`}
      >
        <Text>Dialog body content.</Text>
      </Dialog>

      <ConfirmDialog
        visible={open === 'confirm'}
        title="Delete item"
        message="This action cannot be undone."
        confirmLabel="Delete"
        destructive
        onConfirm={close}
        onCancel={close}
        testID={`gallery-${theme}-confirm`}
      />

      <ActionSheet
        visible={open === 'sheet'}
        title="Actions"
        actions={DIALOG_ACTIONS}
        onCancel={close}
        testID={`gallery-${theme}-sheet`}
      />
    </View>
  )
}

function ThemeSection({ theme }: { theme: ResolvedTheme }) {
  return (
    <VariableContextProvider value={themeVariables(theme)}>
      <View
        testID={`gallery-${theme}`}
        style={SECTION_STYLE}
        className="gap-bb-4 bg-background p-bb-4"
      >
        <Text accessibilityRole="header" variant="heading">
          {theme === 'light' ? 'Light section' : 'Dark section'}
        </Text>
        {PRIMITIVE_REGISTRY.map((entry) => renderEntry(entry, theme))}
        <ExemptPrimitives theme={theme} />
        <OverlayShowcase theme={theme} />
      </View>
    </VariableContextProvider>
  )
}

export default function GalleryScreen() {
  return (
    <ScrollView testID="gallery" className="flex-1 bg-background">
      <ThemeSection theme="light" />
      <ThemeSection theme="dark" />
    </ScrollView>
  )
}
