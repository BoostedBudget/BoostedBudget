import type { ComponentType } from 'react'
import { Box } from './Box'
import { Inline } from './Inline'
import { Stack } from './Stack'
import { Surface } from './Surface'
import { Text } from './Text'
import { BUTTON_REGISTRY } from './button.registry'
import { CARD_REGISTRY } from './card.registry'
import { DIALOG_REGISTRY } from './dialog.registry'
import { FEEDBACK_REGISTRY } from './feedback.registry'
import { FIELDS_REGISTRY } from './fields.registry'
import { LIST_REGISTRY } from './list.registry'
import { NAVIGATION_REGISTRY } from './navigation.registry'
import { SHEET_REGISTRY } from './sheet.registry'
import { STATES_REGISTRY } from './states.registry'
import { THEME_REGISTRY } from './theme.registry'
import { TOAST_REGISTRY } from './toast.registry'

export { Box, Inline, Stack, Surface, Text }
export type { BoxProps, BoxBgTone, BoxBorderTone, BoxRadiusTone } from './Box'
export type { InlineProps, InlineAlign, InlineJustify } from './Inline'
export type { StackProps, StackAlign, StackJustify } from './Stack'
export type { SurfaceProps, SurfaceVariant } from './Surface'
export type { TextProps, TextVariant, TextTone, TextAlign } from './Text'
export type { SpacingStep, SpacingProp, FlexAlign, FlexJustify } from './spacing'

export { Button, ButtonText } from './Button'
export type { ButtonProps, ButtonTextProps, ButtonVariant, ButtonSize } from './Button'
export { IconButton } from './IconButton'
export type { IconButtonProps } from './IconButton'

export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './Card'
export type {
  CardProps,
  CardHeaderProps,
  CardTitleProps,
  CardDescriptionProps,
  CardContentProps,
  CardFooterProps,
  CardVariant,
} from './Card'
export { StatCard } from './StatCard'
export type { StatCardProps, StatTrend } from './StatCard'

export { TextField } from './TextField'
export type { TextFieldProps, FieldAccessibilityState } from './TextField'
export { TextArea } from './TextArea'
export type { TextAreaProps } from './TextArea'
export { Label } from './Label'
export type { LabelProps } from './Label'
export { FormField } from './FormField'
export type { FormFieldProps } from './FormField'
export { Switch } from './Switch'
export type { SwitchProps } from './Switch'
export { Checkbox } from './Checkbox'
export type { CheckboxProps } from './Checkbox'

export { Alert } from './Alert'
export type { AlertProps, AlertVariant } from './Alert'
export { Spinner } from './Spinner'
export type { SpinnerProps, SpinnerSize } from './Spinner'
export { ProgressBar } from './ProgressBar'
export type { ProgressBarProps } from './ProgressBar'
export { Skeleton } from './Skeleton'
export type { SkeletonProps, SkeletonRadius } from './Skeleton'
export { Toast } from './Toast'
export type { ToastProps, ToastVariant, ToastAction } from './Toast'
export { ToastProvider, useToast } from './ToastProvider'
export type { ToastOptions, ToastContextValue } from './ToastProvider'

export { List } from './List'
export type { ListProps, ListVariant } from './List'
export { ListItem } from './ListItem'
export type { ListItemProps, ListItemRole } from './ListItem'
export { SectionHeader } from './SectionHeader'
export type { SectionHeaderProps } from './SectionHeader'
export { Separator } from './Separator'
export type { SeparatorProps, SeparatorOrientation } from './Separator'

export { EmptyState } from './EmptyState'
export type { EmptyStateProps } from './EmptyState'
export { ErrorState } from './ErrorState'
export type { ErrorStateProps } from './ErrorState'

export { Dialog } from './Dialog'
export type { DialogProps, DialogRole } from './Dialog'
export { ConfirmDialog } from './ConfirmDialog'
export type { ConfirmDialogProps } from './ConfirmDialog'
export { ActionSheet } from './ActionSheet'
export type { ActionSheetProps, ActionSheetAction } from './ActionSheet'
export { BottomSheet } from './BottomSheet'
export type { BottomSheetProps } from './BottomSheet'

export { Screen } from './Screen'
export type { ScreenProps } from './Screen'
export { ScreenHeader } from './ScreenHeader'
export type { ScreenHeaderProps } from './ScreenHeader'
export { AppBar } from './AppBar'
export type { AppBarProps } from './AppBar'
export { BackButton } from './BackButton'
export type { BackButtonProps } from './BackButton'
export { TabBar } from './TabBar'
export type { TabBarProps, TabBarItem } from './TabBar'

export { ThemeSelector } from './ThemeSelector'
export type { ThemeSelectorProps, ThemeOption } from './ThemeSelector'

export type PrimitiveCategory =
  | 'typography'
  | 'layout'
  | 'surface'
  | 'action'
  | 'field'
  | 'feedback'
  | 'overlay'
  | 'navigation'
  | 'list'
  | 'theme'
  | 'state'
export type ContrastRatioClass = 'body' | 'large' | 'ui'

export interface PrimitiveRegistryEntry {
  component: ComponentType<Record<string, unknown>>
  name: string
  category: PrimitiveCategory
  interactive: boolean
  foregroundToken: string
  backgroundToken: string
  ratioClass: ContrastRatioClass
}

/**
 * One entry per (primitive x tone/variant) that renders text on a surface, plus one entry per
 * layout primitive. Todo 20 derives one WCAG pair per entry from `foregroundToken`/
 * `backgroundToken` (resolved against `nativeTokens[theme].color` or `.contrast`) and checks it
 * against `ratioClass`.
 *
 * The typography/surface/layout entries live here; every later primitive contributes its entries
 * from its own `<name>.registry.ts` module, concatenated below so the barrel stays the single
 * source of truth and parallel workers never raced on this file.
 */
export const PRIMITIVE_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: Text, name: 'Text.default', category: 'typography', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.muted', category: 'typography', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.subtle', category: 'typography', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.primary', category: 'typography', interactive: false, foregroundToken: 'primary', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.success', category: 'typography', interactive: false, foregroundToken: 'success', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.warning', category: 'typography', interactive: false, foregroundToken: 'warning-foreground', backgroundToken: 'warning', ratioClass: 'body' },
  { component: Text, name: 'Text.danger', category: 'typography', interactive: false, foregroundToken: 'bb-danger-text', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.info', category: 'typography', interactive: false, foregroundToken: 'bb-info-text', backgroundToken: 'background', ratioClass: 'body' },

  { component: Surface, name: 'Surface.canvas', category: 'surface', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Surface, name: 'Surface.card', category: 'surface', interactive: false, foregroundToken: 'card-foreground', backgroundToken: 'card', ratioClass: 'body' },
  { component: Surface, name: 'Surface.raised', category: 'surface', interactive: false, foregroundToken: 'card-foreground', backgroundToken: 'card', ratioClass: 'body' },
  { component: Surface, name: 'Surface.sunken', category: 'surface', interactive: false, foregroundToken: 'foreground', backgroundToken: 'muted', ratioClass: 'body' },
  { component: Surface, name: 'Surface.overlay', category: 'surface', interactive: false, foregroundToken: 'popover-foreground', backgroundToken: 'popover', ratioClass: 'body' },

  { component: Box, name: 'Box', category: 'layout', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'ui' },
  { component: Stack, name: 'Stack', category: 'layout', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'ui' },
  { component: Inline, name: 'Inline', category: 'layout', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'ui' },

  ...BUTTON_REGISTRY,
  ...CARD_REGISTRY,
  ...DIALOG_REGISTRY,
  ...FEEDBACK_REGISTRY,
  ...FIELDS_REGISTRY,
  ...LIST_REGISTRY,
  ...NAVIGATION_REGISTRY,
  ...SHEET_REGISTRY,
  ...STATES_REGISTRY,
  ...THEME_REGISTRY,
  ...TOAST_REGISTRY,
]
