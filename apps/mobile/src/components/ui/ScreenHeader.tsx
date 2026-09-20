import { type ReactNode } from 'react'
import { View } from 'react-native'
import { nativeNavigationTokens } from '@boostedbudget/design-tokens'
import { useTheme } from '../../theme/ThemeProvider'
import { BackButton } from './BackButton'
import { Text } from './Text'

export interface ScreenHeaderProps {
  title: string
  /** When provided, a back button is rendered on the leading edge. */
  onBack?: () => void
  /** Accessible name of the back button. Defaults to "Go back". */
  backLabel?: string
  /** Trailing controls (e.g. an `IconButton`). */
  actions?: ReactNode
  /** Renders a larger title for a root screen. */
  large?: boolean
  testID?: string
}

/**
 * The navigation header for a `Screen`.
 *
 * Every colour is derived from `nativeNavigationTokens` for the resolved theme, so the chrome stays
 * in sync with the React Navigation theme rather than drifting from a second palette. The title is
 * exposed with the `header` role; the optional back affordance is a `BackButton` (accessible name
 * "Go back" by default) and the actions slot is caller-supplied.
 */
export function ScreenHeader({
  title,
  onBack,
  backLabel,
  actions,
  large = false,
  testID = 'screen-header',
}: ScreenHeaderProps) {
  const { resolvedTheme } = useTheme()
  const nav = nativeNavigationTokens[resolvedTheme]

  return (
    <View
      testID={testID}
      className="flex-row items-center gap-bb-2 px-bb-4"
      style={{
        backgroundColor: nav.card,
        borderBottomWidth: 1,
        borderBottomColor: nav.border,
        minHeight: large ? 64 : 56,
      }}
    >
      {onBack ? (
        <BackButton onPress={onBack} label={backLabel} testID={`${testID}-back`} />
      ) : null}
      <Text
        testID={`${testID}-title`}
        accessibilityRole="header"
        variant={large ? 'title' : 'subtitle'}
        numberOfLines={1}
        className="flex-1"
        style={{ color: nav.text }}
      >
        {title}
      </Text>
      {actions ? (
        <View testID={`${testID}-actions`} className="flex-row items-center gap-bb-2">
          {actions}
        </View>
      ) : null}
    </View>
  )
}