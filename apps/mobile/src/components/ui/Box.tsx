import { View, type ViewProps } from 'react-native'
import {
  SPACING_CLASSES,
  type SpacingProp,
  type SpacingStep,
} from './spacing'

export type BoxBgTone =
  | 'none'
  | 'background'
  | 'card'
  | 'muted'
  | 'accent'
  | 'secondary'
  | 'primary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'

export type BoxRadiusTone = 'none' | 'lg' | 'full'

export type BoxBorderTone = 'none' | 'default' | 'strong' | 'primary' | 'danger'

const BG_CLASSES: Record<BoxBgTone, string> = {
  none: '',
  background: 'bg-background',
  card: 'bg-card',
  muted: 'bg-muted',
  accent: 'bg-accent',
  secondary: 'bg-secondary',
  primary: 'bg-primary',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-destructive',
  info: 'bg-info',
}

// Only `none`, `lg` and `full` resolve on native: `radius-lg` is a direct `var(--radius)` alias,
// while the calc-based `radius-sm`/`md`/`xl` tokens do not inline through react-native-css.
const RADIUS_CLASSES: Record<BoxRadiusTone, string> = {
  none: 'rounded-none',
  lg: 'rounded-lg',
  full: 'rounded-full',
}

const BORDER_CLASSES: Record<BoxBorderTone, string> = {
  none: 'border-0',
  default: 'border border-border',
  strong: 'border border-ring',
  primary: 'border border-primary',
  danger: 'border border-destructive',
}

export interface BoxProps extends ViewProps {
  p?: SpacingStep
  px?: SpacingStep
  py?: SpacingStep
  pt?: SpacingStep
  pb?: SpacingStep
  pl?: SpacingStep
  pr?: SpacingStep
  m?: SpacingStep
  mx?: SpacingStep
  my?: SpacingStep
  mt?: SpacingStep
  mb?: SpacingStep
  ml?: SpacingStep
  mr?: SpacingStep
  gap?: SpacingStep
  bg?: BoxBgTone
  radius?: BoxRadiusTone
  border?: BoxBorderTone
}

function spacingClass(prop: SpacingProp, step: SpacingStep | undefined): string {
  return step === undefined ? '' : SPACING_CLASSES[prop][step]
}

export function Box({
  p,
  px,
  py,
  pt,
  pb,
  pl,
  pr,
  m,
  mx,
  my,
  mt,
  mb,
  ml,
  mr,
  gap,
  bg,
  radius,
  border,
  className,
  ...rest
}: BoxProps) {
  const classes = [
    spacingClass('p', p),
    spacingClass('px', px),
    spacingClass('py', py),
    spacingClass('pt', pt),
    spacingClass('pb', pb),
    spacingClass('pl', pl),
    spacingClass('pr', pr),
    spacingClass('m', m),
    spacingClass('mx', mx),
    spacingClass('my', my),
    spacingClass('mt', mt),
    spacingClass('mb', mb),
    spacingClass('ml', ml),
    spacingClass('mr', mr),
    spacingClass('gap', gap),
    bg ? BG_CLASSES[bg] : '',
    radius ? RADIUS_CLASSES[radius] : '',
    border ? BORDER_CLASSES[border] : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return <View className={classes} {...rest} />
}
