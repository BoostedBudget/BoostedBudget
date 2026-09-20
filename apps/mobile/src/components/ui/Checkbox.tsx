import { Pressable, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Text } from './Text'
import type { FieldAccessibilityState } from './TextField'

const TOUCH_MIN = nativeTokens.light.touchTarget.min

export interface CheckboxProps {
  checked: boolean
  onCheckedChange?: (checked: boolean) => void
  /** Renders the mixed state. Exposed as `accessibilityState.checked = 'mixed'`. */
  indeterminate?: boolean
  disabled?: boolean
  /** Marks the control invalid for assistive technology. Set by `FormField`. */
  invalid?: boolean
  /** Visible label rendered beside the box. */
  label?: string
  testID?: string
  accessibilityLabel?: string
  accessibilityHint?: string
  className?: string
}

/**
 * A checkbox. Checked/indeterminate state is exposed through `accessibilityState.checked` and a
 * literal glyph (never colour alone). The whole row is the press target and meets the
 * touch-target floor.
 */
export function Checkbox({
  checked,
  onCheckedChange,
  indeterminate = false,
  disabled = false,
  invalid = false,
  label,
  testID,
  accessibilityLabel,
  accessibilityHint,
  className,
}: CheckboxProps) {
  const active = checked || indeterminate
  const state: FieldAccessibilityState = {
    checked: indeterminate ? 'mixed' : checked,
    disabled,
    invalid,
  }
  const glyph = indeterminate ? '–' : checked ? '✓' : ''

  return (
    <Pressable
      testID={testID}
      accessibilityRole="checkbox"
      accessibilityState={state}
      accessibilityLabel={accessibilityLabel ?? label}
      accessibilityHint={accessibilityHint}
      disabled={disabled}
      onPress={() => onCheckedChange?.(!checked)}
      className={['flex-row items-center gap-bb-2', className].filter(Boolean).join(' ')}
      style={{ minHeight: TOUCH_MIN, minWidth: TOUCH_MIN }}
    >
      <View
        className={[
          'h-bb-6 w-bb-6 items-center justify-center rounded-lg border',
          active ? 'border-primary bg-primary' : 'border-input bg-background',
        ].join(' ')}
      >
        {/* `text-primary-foreground` is emitted after `text-foreground`, so it wins on the glyph. */}
        <Text variant="bodySmall" className="text-primary-foreground">
          {glyph}
        </Text>
      </View>
      {label ? <Text variant="body">{label}</Text> : null}
    </Pressable>
  )
}