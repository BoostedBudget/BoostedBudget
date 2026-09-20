import { TextInput, type AccessibilityState, type TextInputProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'

/** RN 0.86's `AccessibilityState` has no `invalid`; the runtime still forwards it. */
export type FieldAccessibilityState = AccessibilityState & { invalid?: boolean }

export const FIELD_TOUCH_MIN = nativeTokens.light.touchTarget.min

export interface TextFieldProps extends TextInputProps {
  /** Marks the control invalid for assistive technology. Set by `FormField`. */
  invalid?: boolean
  /** Disables the control. Takes precedence over `editable`. */
  disabled?: boolean
}

/**
 * A single-line text input. The touch-target floor is applied as `minHeight` (never padding
 * alone), dynamic type is left enabled, and the invalid state is exposed through
 * `accessibilityState.invalid` rather than colour alone.
 */
export function TextField({
  invalid = false,
  disabled = false,
  editable = true,
  accessibilityState,
  className,
  style,
  ...rest
}: TextFieldProps) {
  const isEditable = disabled ? false : editable
  const state: FieldAccessibilityState = {
    ...accessibilityState,
    invalid,
    disabled: !isEditable,
  }
  const classes = [
    'rounded-lg border bg-background px-bb-3 py-bb-2 text-foreground placeholder:text-muted-foreground',
    invalid ? 'border-destructive' : 'border-input',
    isEditable ? '' : 'opacity-50',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <TextInput
      {...rest}
      editable={isEditable}
      accessibilityState={state}
      className={classes}
      style={[{ minHeight: FIELD_TOUCH_MIN }, style]}
      allowFontScaling
    />
  )
}