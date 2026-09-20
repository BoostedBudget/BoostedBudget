import { createContext, useContext, useState, type ReactNode } from 'react'
import {
  ActivityIndicator,
  Pressable,
  Text as RNText,
  View,
  type PressableProps,
  type StyleProp,
  type TextProps as RNTextProps,
  type ViewStyle,
} from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'

export type ButtonVariant = 'primary' | 'secondary' | 'outline' | 'ghost' | 'destructive' | 'link'
export type ButtonSize = 'sm' | 'md' | 'lg'

/**
 * The touch floor is applied as an explicit `minHeight`/`minWidth`, never as padding, so the
 * 44x44 minimum survives a zero-content button and cannot be undone by a padding override.
 * `touchTarget` is theme-invariant in the generated adapter (44/48 in both themes).
 */
export const TOUCH_TARGET_MIN = nativeTokens.light.touchTarget.min

/**
 * The focus indicator resolves its colour from the `outline-ring` utility (which flips with the
 * theme through `--ring`) and its geometry from `focusTokens`. `button.test.tsx` pins the
 * resolved ring against `nativeTokens[theme].focus` in both themes so the two cannot drift.
 */
const FOCUS_RING_COLOR_CLASS = 'outline-ring'
const FOCUS_RING_STYLE: ViewStyle = {
  outlineStyle: 'solid',
  outlineWidth: nativeTokens.light.focus['bb-focus-ring-width'],
  outlineOffset: nativeTokens.light.focus['bb-focus-ring-offset'],
}

export const BUTTON_FOCUS_RING_COLOR_CLASS = FOCUS_RING_COLOR_CLASS
export const BUTTON_FOCUS_RING_STYLE = FOCUS_RING_STYLE

const DISABLED_OPACITY = 0.5

const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary: 'bg-primary',
  secondary: 'bg-secondary',
  outline: 'border border-border bg-background',
  ghost: '',
  destructive: 'bg-bb-danger-strong',
  link: '',
}

const VARIANT_TEXT_CLASSES: Record<ButtonVariant, string> = {
  primary: 'text-primary-foreground',
  secondary: 'text-secondary-foreground',
  outline: 'text-foreground',
  ghost: 'text-foreground',
  destructive: 'text-destructive-foreground',
  link: 'text-primary underline',
}

const SIZE_MIN_HEIGHT: Record<ButtonSize, number> = {
  sm: TOUCH_TARGET_MIN,
  md: nativeTokens.light.touchTarget.comfortable,
  lg: nativeTokens.light.touchTarget.comfortable,
}

const SIZE_PADDING_CLASSES: Record<ButtonSize, string> = {
  sm: 'px-bb-3 py-bb-1',
  md: 'px-bb-4 py-bb-2',
  lg: 'px-bb-5 py-bb-3',
}

const SIZE_TEXT_CLASSES: Record<ButtonSize, string> = {
  sm: 'font-bb-sans font-bb-medium text-bb-sm leading-bb-snug',
  md: 'font-bb-sans font-bb-medium text-bb-body leading-bb-snug',
  lg: 'font-bb-sans font-bb-semibold text-bb-lg leading-bb-snug',
}

const BASE_CLASSES = 'flex-row items-center justify-center gap-bb-2 rounded-lg'

export const BUTTON_VARIANT_CLASSES = VARIANT_CLASSES
export const BUTTON_VARIANT_TEXT_CLASSES = VARIANT_TEXT_CLASSES
export const BUTTON_SIZE_MIN_HEIGHT = SIZE_MIN_HEIGHT
export const BUTTON_SIZE_PADDING_CLASSES = SIZE_PADDING_CLASSES
export const BUTTON_SIZE_TEXT_CLASSES = SIZE_TEXT_CLASSES
export const BUTTON_BASE_CLASSES = BASE_CLASSES

interface ButtonContextValue {
  variant: ButtonVariant
  size: ButtonSize
}

const ButtonContext = createContext<ButtonContextValue>({ variant: 'primary', size: 'md' })

export interface ButtonTextProps extends RNTextProps {
  variant?: ButtonVariant
  size?: ButtonSize
}

/**
 * The button's text node. It reads the surrounding `Button`'s variant/size from context so a
 * `ButtonText` child inherits the correct foreground token; outside a `Button` it falls back to
 * the primary/md defaults. Dynamic type stays on (RN's default) — `allowFontScaling` is never
 * forced to `false`.
 */
export function ButtonText({ variant, size, className, ...rest }: ButtonTextProps) {
  const context = useContext(ButtonContext)
  const resolvedVariant = variant ?? context.variant
  const resolvedSize = size ?? context.size
  const classes = [
    SIZE_TEXT_CLASSES[resolvedSize],
    VARIANT_TEXT_CLASSES[resolvedVariant],
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return <RNText {...rest} className={classes} />
}

export interface ButtonProps
  extends Omit<PressableProps, 'children' | 'style' | 'accessibilityRole' | 'accessibilityState'> {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
  fullWidth?: boolean
  leadingIcon?: ReactNode
  trailingIcon?: ReactNode
  /** Visible label; also the accessible name when provided. */
  label?: string
  /** Accessible name of the loading indicator. */
  loadingLabel?: string
  children?: ReactNode
  style?: StyleProp<ViewStyle>
}

function IconSlot({
  icon,
  testID,
  position,
}: {
  icon: ReactNode
  testID: string | undefined
  position: 'leading' | 'trailing'
}) {
  if (!icon) return null
  return (
    <View
      testID={testID ? `${testID}-${position}-icon` : undefined}
      pointerEvents="none"
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      {icon}
    </View>
  )
}

export function Button({
  variant = 'primary',
  size = 'md',
  loading = false,
  disabled = false,
  fullWidth = false,
  leadingIcon,
  trailingIcon,
  label,
  loadingLabel = 'Loading',
  children,
  style,
  testID,
  onPress,
  onFocus,
  onBlur,
  accessibilityLabel,
  ...rest
}: ButtonProps) {
  const [focused, setFocused] = useState(false)
  const isDisabled = disabled || loading

  const classes = [
    BASE_CLASSES,
    VARIANT_CLASSES[variant],
    SIZE_PADDING_CLASSES[size],
    fullWidth ? 'w-full' : '',
    focused ? FOCUS_RING_COLOR_CLASS : '',
  ]
    .filter(Boolean)
    .join(' ')

  const accessibleName =
    accessibilityLabel ?? label ?? (typeof children === 'string' ? children : undefined)

  const content =
    label !== undefined ? (
      <ButtonText testID={testID ? `${testID}-label` : undefined}>{label}</ButtonText>
    ) : (
      children
    )

  return (
    <ButtonContext.Provider value={{ variant, size }}>
      <Pressable
        {...rest}
        testID={testID}
        disabled={isDisabled}
        onPress={onPress}
        onFocus={(event) => {
          setFocused(true)
          onFocus?.(event)
        }}
        onBlur={(event) => {
          setFocused(false)
          onBlur?.(event)
        }}
        accessibilityRole="button"
        accessibilityState={{ disabled: isDisabled, busy: loading }}
        accessibilityLabel={accessibleName}
        className={classes}
        style={[
          { minHeight: SIZE_MIN_HEIGHT[size], opacity: isDisabled ? DISABLED_OPACITY : 1 },
          focused ? FOCUS_RING_STYLE : null,
          style,
        ]}
      >
        <IconSlot icon={leadingIcon} testID={testID} position="leading" />
        {content}
        <IconSlot icon={trailingIcon} testID={testID} position="trailing" />
        {loading ? (
          <ActivityIndicator
            testID={testID ? `${testID}-spinner` : 'button-spinner'}
            accessibilityLabel={loadingLabel}
          />
        ) : null}
      </Pressable>
    </ButtonContext.Provider>
  )
}
