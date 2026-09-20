import { Pressable, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Text } from './Text'
import type { FieldAccessibilityState } from './TextField'

const TOUCH_MIN = nativeTokens.light.touchTarget.min

export interface SwitchProps {
  value: boolean
  onValueChange?: (value: boolean) => void
  disabled?: boolean
  /** Marks the control invalid for assistive technology. Set by `FormField`. */
  invalid?: boolean
  /** Visible label rendered beside the track. */
  label?: string
  testID?: string
  accessibilityLabel?: string
  accessibilityHint?: string
  className?: string
}

/**
 * A toggle switch. State is exposed through `accessibilityState.checked` (never colour alone) and
 * the thumb position. The whole row is the press target and meets the touch-target floor.
 */
export function Switch({
  value,
  onValueChange,
  disabled = false,
  invalid = false,
  label,
  testID,
  accessibilityLabel,
  accessibilityHint,
  className,
}: SwitchProps) {
  const state: FieldAccessibilityState = { checked: value, disabled, invalid }

  return (
    <Pressable
      testID={testID}
      accessibilityRole="switch"
      accessibilityState={state}
      accessibilityLabel={accessibilityLabel ?? label}
      accessibilityHint={accessibilityHint}
      disabled={disabled}
      onPress={() => onValueChange?.(!value)}
      className={['flex-row items-center gap-bb-2', className].filter(Boolean).join(' ')}
      style={{ minHeight: TOUCH_MIN, minWidth: TOUCH_MIN }}
    >
      <View
        className={[
          'h-bb-6 w-bb-10 flex-row items-center rounded-full px-bb-1',
          value ? 'justify-end bg-primary' : 'justify-start bg-muted',
        ].join(' ')}
      >
        <View className="h-bb-4 w-bb-4 rounded-full bg-background" />
      </View>
      {label ? <Text variant="body">{label}</Text> : null}
    </Pressable>
  )
}