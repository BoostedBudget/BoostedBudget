import { useState, type ReactNode } from 'react'
import {
  Pressable,
  View,
  type PressableProps,
  type StyleProp,
  type ViewStyle,
} from 'react-native'
import {
  BUTTON_FOCUS_RING_COLOR_CLASS,
  BUTTON_FOCUS_RING_STYLE,
  BUTTON_VARIANT_CLASSES,
  BUTTON_SIZE_MIN_HEIGHT,
  type ButtonSize,
  type ButtonVariant,
} from './Button'

const DISABLED_OPACITY = 0.5
const ICON_BUTTON_BASE_CLASSES = 'items-center justify-center rounded-full'

export interface IconButtonProps
  extends Omit<PressableProps, 'children' | 'style' | 'accessibilityRole' | 'accessibilityState'> {
  /** Required: an icon-only affordance must always carry an accessible name. */
  label: string
  variant?: ButtonVariant
  size?: ButtonSize
  children?: ReactNode
  style?: StyleProp<ViewStyle>
}

export function IconButton({
  label,
  variant = 'primary',
  size = 'md',
  children,
  style,
  testID,
  disabled = false,
  onPress,
  onFocus,
  onBlur,
  ...rest
}: IconButtonProps) {
  const [focused, setFocused] = useState(false)

  const classes = [
    ICON_BUTTON_BASE_CLASSES,
    BUTTON_VARIANT_CLASSES[variant],
    focused ? BUTTON_FOCUS_RING_COLOR_CLASS : '',
  ]
    .filter(Boolean)
    .join(' ')

  // A square floor: both `minHeight` and `minWidth` are the touch target, never padding.
  const minSize = BUTTON_SIZE_MIN_HEIGHT[size]

  return (
    <Pressable
      {...rest}
      testID={testID}
      disabled={disabled}
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
      accessibilityState={{ disabled: disabled ?? false, busy: false }}
      accessibilityLabel={label}
      className={classes}
      style={[
        { minHeight: minSize, minWidth: minSize, opacity: disabled ? DISABLED_OPACITY : 1 },
        focused ? BUTTON_FOCUS_RING_STYLE : null,
        style,
      ]}
    >
      <View
        pointerEvents="none"
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
      >
        {children}
      </View>
    </Pressable>
  )
}
