import type { ReactNode } from 'react'
import { View, type ViewProps } from 'react-native'
import { Card } from './Card'
import { Text, type TextTone } from './Text'

export type StatTrend = 'up' | 'down' | 'flat'

/**
 * `down` uses the darker `bb-danger-text` contrast token rather than the raw `destructive` fill,
 * so the delta clears the body-text contrast floor on the card surface in both themes.
 */
const TREND_TONES: Record<StatTrend, TextTone> = {
  up: 'success',
  down: 'danger',
  flat: 'muted',
}

export interface StatCardProps extends Omit<ViewProps, 'children'> {
  label: string
  /**
   * A preformatted, caller-supplied string. The primitive never parses, rounds or reformats it —
   * currency formatting is the caller's responsibility.
   */
  value: string
  /** A preformatted, caller-supplied delta string (e.g. `"+$12.00"`). */
  delta?: string
  trend?: StatTrend
  icon?: ReactNode
}

export function StatCard({
  label,
  value,
  delta,
  trend = 'flat',
  icon,
  testID,
  className,
  ...rest
}: StatCardProps) {
  return (
    <Card testID={testID} className={['gap-bb-1', className].filter(Boolean).join(' ')} {...rest}>
      <View className="flex-row items-center justify-between gap-bb-2">
        <Text
          testID={testID ? `${testID}-label` : undefined}
          variant="caption"
          tone="muted"
          className="uppercase tracking-bb-wide"
        >
          {label}
        </Text>
        {icon ? (
          <View
            testID={testID ? `${testID}-icon` : undefined}
            accessibilityElementsHidden
            importantForAccessibility="no-hide-descendants"
          >
            {icon}
          </View>
        ) : null}
      </View>
      <Text testID={testID ? `${testID}-value` : undefined} variant="heading" tone="default">
        {value}
      </Text>
      {delta ? (
        <Text
          testID={testID ? `${testID}-delta` : undefined}
          variant="bodySmall"
          tone={TREND_TONES[trend]}
        >
          {delta}
        </Text>
      ) : null}
    </Card>
  )
}