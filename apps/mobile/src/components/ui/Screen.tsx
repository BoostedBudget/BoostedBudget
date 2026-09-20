import { useContext, type ReactNode } from 'react'
import { ScrollView, View, type StyleProp, type ViewStyle } from 'react-native'
import { SafeAreaInsetsContext } from 'react-native-safe-area-context'
import { nativeNavigationTokens } from '@boostedbudget/design-tokens'
import { useTheme } from '../../theme/ThemeProvider'

/**
 * `useSafeAreaInsets()` throws without a `<SafeAreaProvider>` ancestor, and `jest.setup.ts` is
 * off-limits, so its jest mock cannot be registered globally. Reading the context directly with a
 * zero fallback is the same pattern the package's own `jest/mock.tsx` uses; real navigation and
 * production supply real insets.
 */
const FALLBACK_INSETS = { top: 0, right: 0, bottom: 0, left: 0 }

export interface ScreenProps {
  children?: ReactNode
  /** Optional header rendered above the content (e.g. a `ScreenHeader`). */
  header?: ReactNode
  /** When true the content scrolls; otherwise it fills the remaining space. */
  scrollable?: boolean
  testID?: string
  style?: StyleProp<ViewStyle>
  contentContainerStyle?: StyleProp<ViewStyle>
}

/**
 * The safe-area screen wrapper.
 *
 * The root is padded by the top inset and painted with the navigation background; the content is
 * padded by the bottom inset so it never sits under the home indicator. An optional header is
 * rendered above the content, and `scrollable` swaps the content container for a `ScrollView`.
 */
export function Screen({
  children,
  header,
  scrollable = false,
  testID = 'screen',
  style,
  contentContainerStyle,
}: ScreenProps) {
  const { resolvedTheme } = useTheme()
  const nav = nativeNavigationTokens[resolvedTheme]
  const insets = useContext(SafeAreaInsetsContext) ?? FALLBACK_INSETS

  const content = scrollable ? (
    <ScrollView
      testID={`${testID}-scroll`}
      className="flex-1"
      contentContainerStyle={[{ paddingBottom: insets.bottom }, contentContainerStyle]}
    >
      {children}
    </ScrollView>
  ) : (
    <View
      testID={`${testID}-content`}
      className="flex-1"
      style={[{ paddingBottom: insets.bottom }, contentContainerStyle]}
    >
      {children}
    </View>
  )

  return (
    <View
      testID={testID}
      className="flex-1"
      style={[{ backgroundColor: nav.background, paddingTop: insets.top }, style]}
    >
      {header}
      {content}
    </View>
  )
}