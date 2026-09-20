import { useState } from 'react'
import { Pressable, Text as RNText } from 'react-native'
import { nativeNavigationTokens } from '@boostedbudget/design-tokens'
import { useTheme } from '../../theme/ThemeProvider'
import { BUTTON_FOCUS_RING_COLOR_CLASS, BUTTON_FOCUS_RING_STYLE, TOUCH_TARGET_MIN } from './Button'

export interface BackButtonProps {
  /**
   * Injected navigation callback. The primitive deliberately never imports the router, so it stays
   * testable and the caller owns the navigation seam.
   */
  onPress: () => void
  /** Accessible name. Defaults to "Go back". */
  label?: string
  testID?: string
}

/**
 * The leading back affordance for a `ScreenHeader`.
 *
 * It is icon-only, so the accessible name is mandatory — it defaults to "Go back" and can be
 * overridden per screen. The touch floor is an explicit `minHeight`/`minWidth` (never padding), and
 * the focus ring is React focus state so it is visible on web as well as native.
 */
export function BackButton({ onPress, label = 'Go back', testID = 'back-button' }: BackButtonProps) {
  const { resolvedTheme } = useTheme()
  const nav = nativeNavigationTokens[resolvedTheme]
  const [focused, setFocused] = useState(false)

  const classes = [
    'flex-row items-center justify-center rounded-lg',
    focused ? BUTTON_FOCUS_RING_COLOR_CLASS : '',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
      className={classes}
      style={[
        { minHeight: TOUCH_TARGET_MIN, minWidth: TOUCH_TARGET_MIN },
        focused ? BUTTON_FOCUS_RING_STYLE : null,
      ]}
    >
      <RNText
        testID={`${testID}-glyph`}
        className="text-bb-xl leading-bb-tight"
        style={{ color: nav.text }}
      >
        ‹
      </RNText>
    </Pressable>
  )
}