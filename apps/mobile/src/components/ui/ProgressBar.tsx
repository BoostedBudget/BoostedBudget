import { View, type ViewProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'

const TRACK_HEIGHT = nativeTokens.light.spacing['spacing-bb-2']

// A static placeholder fill for the indeterminate state. It deliberately does not animate, so the
// "never animate under reduced motion" contract holds without a second motion branch.
const INDETERMINATE_FILL_PERCENT = 40

export interface ProgressBarProps extends ViewProps {
  /** Progress in the range 0..1. Ignored when `indeterminate`. */
  value?: number
  indeterminate?: boolean
  label?: string
}

export function ProgressBar({
  value = 0,
  indeterminate = false,
  label,
  className,
  style,
  ...rest
}: ProgressBarProps) {
  const clamped = Math.min(1, Math.max(0, value))
  const percent = indeterminate ? INDETERMINATE_FILL_PERCENT : Math.round(clamped * 100)

  return (
    <View
      {...rest}
      accessibilityRole="progressbar"
      accessibilityLabel={label}
      accessibilityValue={indeterminate ? { min: 0, max: 100 } : { min: 0, max: 100, now: percent }}
      className={['bg-muted rounded-full overflow-hidden', className].filter(Boolean).join(' ')}
      style={[{ height: TRACK_HEIGHT }, style]}
    >
      <View className="bg-primary rounded-full h-full" style={{ width: `${percent}%` }} />
    </View>
  )
}