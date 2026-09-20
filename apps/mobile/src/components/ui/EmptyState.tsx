import type { ReactNode } from 'react'
import { View, type ViewProps } from 'react-native'
import { Text } from './Text'

export interface EmptyStateProps extends ViewProps {
  title: string
  description?: string
  /** Decorative slot; hidden from the accessibility tree so it never overrides the summary. */
  icon?: ReactNode
  /** Primary action slot. Rendered OUTSIDE the grouped summary so it stays focusable. */
  action?: ReactNode
}

/**
 * A calm, non-colour-dependent empty state.
 *
 * The title and description live inside a single `accessible` summary node so a screen reader
 * announces one grouped message rather than a scatter of text nodes. The action is a sibling of
 * that node — grouping it would hide the action from the accessibility tree on native and make it
 * unreachable on web.
 */
export function EmptyState({
  title,
  description,
  icon,
  action,
  className,
  testID,
  ...rest
}: EmptyStateProps) {
  return (
    <View
      {...rest}
      testID={testID}
      className={['items-center justify-center gap-bb-3 p-bb-6', className].filter(Boolean).join(' ')}
    >
      <View
        accessible
        accessibilityRole="summary"
        testID={testID ? `${testID}-summary` : undefined}
        className="items-center gap-bb-1"
      >
        {icon ? (
          <View
            testID={testID ? `${testID}-icon` : undefined}
            accessibilityElementsHidden
            importantForAccessibility="no-hide-descendants"
          >
            {icon}
          </View>
        ) : null}
        <Text
          variant="subtitle"
          tone="default"
          align="center"
          testID={testID ? `${testID}-title` : undefined}
        >
          {title}
        </Text>
        {description ? (
          <Text
            variant="bodySmall"
            tone="muted"
            align="center"
            testID={testID ? `${testID}-description` : undefined}
          >
            {description}
          </Text>
        ) : null}
      </View>
      {action ? <View>{action}</View> : null}
    </View>
  )
}