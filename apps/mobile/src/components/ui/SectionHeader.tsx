import type { ReactNode } from 'react'
import { View, type ViewProps } from 'react-native'
import { Text } from './Text'

export interface SectionHeaderProps extends ViewProps {
  title: string
  /** Optional trailing slot, e.g. a "See all" action. */
  action?: ReactNode
}

/**
 * A section label that groups the rows beneath it. The title is exposed as a `header` so a screen
 * reader can jump between sections.
 */
export function SectionHeader({ title, action, className, testID, ...rest }: SectionHeaderProps) {
  const classes = [
    'flex-row items-center justify-between px-bb-4 py-bb-2',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <View {...rest} testID={testID} className={classes}>
      <Text
        variant="label"
        tone="muted"
        accessibilityRole="header"
        testID={testID ? `${testID}-title` : undefined}
      >
        {title}
      </Text>
      {action}
    </View>
  )
}