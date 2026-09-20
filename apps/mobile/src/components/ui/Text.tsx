import { Text as RNText, type TextProps as RNTextProps } from 'react-native'

export type TextVariant =
  | 'display'
  | 'title'
  | 'heading'
  | 'subtitle'
  | 'body'
  | 'bodySmall'
  | 'caption'
  | 'label'
  | 'mono'

export type TextTone = 'default' | 'muted' | 'subtle' | 'primary' | 'success' | 'warning' | 'danger' | 'info'

export type TextAlign = 'left' | 'center' | 'right'

export const TEXT_VARIANT_CLASSES: Record<TextVariant, string> = {
  display: 'font-bb-sans font-bb-bold text-bb-4xl leading-bb-tight tracking-bb-tight',
  title: 'font-bb-sans font-bb-bold text-bb-3xl leading-bb-tight tracking-bb-tight',
  heading: 'font-bb-sans font-bb-semibold text-bb-2xl leading-bb-tight tracking-bb-tight',
  subtitle: 'font-bb-sans font-bb-semibold text-bb-xl leading-bb-snug tracking-bb-normal',
  body: 'font-bb-sans font-bb-regular text-bb-body leading-bb-normal tracking-bb-normal',
  bodySmall: 'font-bb-sans font-bb-regular text-bb-sm leading-bb-normal tracking-bb-normal',
  caption: 'font-bb-sans font-bb-regular text-bb-xs leading-bb-normal tracking-bb-normal',
  label: 'font-bb-sans font-bb-medium text-bb-sm leading-bb-snug tracking-bb-normal',
  mono: 'font-bb-mono font-bb-regular text-bb-sm leading-bb-normal tracking-bb-normal',
}

// `warning` cannot be legible as plain text on the neutral surface in light mode (2.16:1), so it
// renders as a filled warning surface instead. `danger`/`info` use the darker contrast tokens for
// body text on a neutral surface; the raw `destructive`/`info` fills are reserved for Todo 9/12.
export const TEXT_TONE_CLASSES: Record<TextTone, string> = {
  default: 'text-foreground',
  muted: 'text-muted-foreground',
  subtle: 'text-muted-foreground',
  primary: 'text-primary',
  success: 'text-success',
  warning: 'bg-warning text-warning-foreground',
  danger: 'text-bb-danger-text',
  info: 'text-bb-info-text',
}

export const TEXT_ALIGN_CLASSES: Record<TextAlign, string> = {
  left: 'text-left',
  center: 'text-center',
  right: 'text-right',
}

export interface TextProps extends RNTextProps {
  variant?: TextVariant
  tone?: TextTone
  align?: TextAlign
  maxFontSizeMultiplier?: number
}

export function Text({
  variant = 'body',
  tone = 'default',
  align,
  className,
  allowFontScaling = true,
  maxFontSizeMultiplier,
  ...rest
}: TextProps) {
  const classes = [
    TEXT_VARIANT_CLASSES[variant],
    TEXT_TONE_CLASSES[tone],
    align ? TEXT_ALIGN_CLASSES[align] : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <RNText
      {...rest}
      className={classes}
      allowFontScaling={allowFontScaling}
      maxFontSizeMultiplier={maxFontSizeMultiplier}
    />
  )
}
