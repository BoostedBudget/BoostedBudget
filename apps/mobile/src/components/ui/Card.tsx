import { createContext, useContext, useState } from 'react'
import { Pressable, View, type ViewProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Text, type TextProps } from './Text'
import { BUTTON_FOCUS_RING_COLOR_CLASS, BUTTON_FOCUS_RING_STYLE } from './Button'

export type CardVariant = 'default' | 'outlined' | 'elevated' | 'sunken'

/**
 * The touch floor is applied as an explicit `minHeight`, never as padding, so a pressable card
 * cannot fall below the 44px minimum. `touchTarget` is theme-invariant in the generated adapter.
 */
export const CARD_TOUCH_MIN = nativeTokens.light.touchTarget.min

const CARD_BASE_CLASSES = 'rounded-lg p-bb-4'

/**
 * Each variant maps to a surface token (`card` or `muted`) plus, for `elevated`, the `shadow-bb-md`
 * elevation token. `outlined` uses the stronger `ring` border colour so it is distinguishable from
 * `default` without a second surface token.
 */
const CARD_VARIANT_CLASSES: Record<CardVariant, string> = {
  default: 'bg-card border border-border',
  outlined: 'bg-card border border-ring',
  elevated: 'bg-card border border-border shadow-bb-md',
  sunken: 'bg-muted',
}

/**
 * The variant is shared with the text subcomponents so a `CardDescription` on a `sunken` surface
 * can fall back to the full-strength foreground token: `muted-foreground` on `muted` is only
 * 4.35:1 in light mode and would fail the body-text contrast floor.
 */
const CardVariantContext = createContext<CardVariant>('default')

export interface CardProps extends ViewProps {
  variant?: CardVariant
  /** When present the card becomes a button; when absent it stays a plain container. */
  onPress?: () => void
  disabled?: boolean
}

export function Card({
  variant = 'default',
  onPress,
  disabled = false,
  className,
  style,
  children,
  testID,
  accessibilityLabel,
  accessibilityHint,
  ...rest
}: CardProps) {
  const [focused, setFocused] = useState(false)

  const classes = [
    CARD_BASE_CLASSES,
    CARD_VARIANT_CLASSES[variant],
    focused ? BUTTON_FOCUS_RING_COLOR_CLASS : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  const content = <CardVariantContext.Provider value={variant}>{children}</CardVariantContext.Provider>

  if (onPress) {
    return (
      <Pressable
        testID={testID}
        disabled={disabled}
        onPress={onPress}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        accessibilityRole="button"
        accessibilityState={{ disabled }}
        accessibilityLabel={accessibilityLabel}
        accessibilityHint={accessibilityHint}
        className={classes}
        style={[
          { minHeight: CARD_TOUCH_MIN },
          focused ? BUTTON_FOCUS_RING_STYLE : null,
          style,
        ]}
      >
        {content}
      </Pressable>
    )
  }

  return (
    <View
      {...rest}
      testID={testID}
      accessibilityLabel={accessibilityLabel}
      accessibilityHint={accessibilityHint}
      className={classes}
      style={style}
    >
      {content}
    </View>
  )
}

export type CardHeaderProps = ViewProps

export function CardHeader({ className, ...rest }: CardHeaderProps) {
  return <View className={['flex-col gap-bb-1', className].filter(Boolean).join(' ')} {...rest} />
}

export type CardTitleProps = TextProps

export function CardTitle({ className, ...rest }: CardTitleProps) {
  return <Text variant="subtitle" tone="default" className={className} {...rest} />
}

export type CardDescriptionProps = TextProps

export function CardDescription({ className, ...rest }: CardDescriptionProps) {
  const variant = useContext(CardVariantContext)
  const tone = variant === 'sunken' ? 'default' : 'muted'

  return <Text variant="bodySmall" tone={tone} className={className} {...rest} />
}

export type CardContentProps = ViewProps

export function CardContent({ className, ...rest }: CardContentProps) {
  return <View className={['gap-bb-2', className].filter(Boolean).join(' ')} {...rest} />
}

export type CardFooterProps = ViewProps

export function CardFooter({ className, ...rest }: CardFooterProps) {
  return (
    <View className={['flex-row items-center gap-bb-2', className].filter(Boolean).join(' ')} {...rest} />
  )
}