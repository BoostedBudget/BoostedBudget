import { useState, type ReactNode } from 'react'
import { Pressable, View } from 'react-native'
import { nativeNavigationTokens } from '@boostedbudget/design-tokens'
import { useTheme } from '../../theme/ThemeProvider'
import { BUTTON_FOCUS_RING_COLOR_CLASS, BUTTON_FOCUS_RING_STYLE, TOUCH_TARGET_MIN } from './Button'
import { Text } from './Text'

export interface TabBarItem {
  key: string
  /** Visible label; also the accessible name. Required, so a tab is never icon-only. */
  label: string
  icon?: ReactNode
  selected?: boolean
  disabled?: boolean
  onPress?: () => void
}

export interface TabBarProps {
  items: TabBarItem[]
  /** Fired with the item key after the item's own handler. */
  onSelect?: (key: string) => void
  testID?: string
}

type NavigationTokens = (typeof nativeNavigationTokens)[keyof typeof nativeNavigationTokens]

function TabBarItemView({
  item,
  nav,
  onSelect,
  testID,
}: {
  item: TabBarItem
  nav: NavigationTokens
  onSelect?: (key: string) => void
  testID: string
}) {
  const [focused, setFocused] = useState(false)
  const selected = item.selected ?? false

  const classes = [
    'flex-1 items-center justify-center gap-bb-1 rounded-lg',
    focused ? BUTTON_FOCUS_RING_COLOR_CLASS : '',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <Pressable
      testID={testID}
      accessibilityRole="tab"
      accessibilityState={{ selected, disabled: item.disabled ?? false }}
      accessibilityLabel={item.label}
      disabled={item.disabled}
      onPress={() => {
        item.onPress?.()
        onSelect?.(item.key)
      }}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
      className={classes}
      style={[
        { minHeight: TOUCH_TARGET_MIN, minWidth: TOUCH_TARGET_MIN },
        focused ? BUTTON_FOCUS_RING_STYLE : null,
      ]}
    >
      {item.icon ? (
        <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
          {item.icon}
        </View>
      ) : null}
      <Text
        testID={`${testID}-label`}
        variant="caption"
        style={{ color: selected ? nav.primary : nav.text, opacity: selected ? 1 : 0.6 }}
      >
        {item.label}
      </Text>
    </Pressable>
  )
}

/**
 * A bottom tab bar.
 *
 * Each item exposes the `tab` role and its `selected` state, carries a mandatory text label (the
 * accessible name), meets the touch floor, and shows a focus ring driven by React focus state so it
 * is visible on web. Colours come from `nativeNavigationTokens` for the resolved theme.
 */
export function TabBar({ items, onSelect, testID = 'tab-bar' }: TabBarProps) {
  const { resolvedTheme } = useTheme()
  const nav = nativeNavigationTokens[resolvedTheme]

  return (
    <View
      testID={testID}
      accessibilityRole="tablist"
      className="flex-row items-stretch"
      style={{ backgroundColor: nav.card, borderTopWidth: 1, borderTopColor: nav.border }}
    >
      {items.map((item) => (
        <TabBarItemView
          key={item.key}
          item={item}
          nav={nav}
          onSelect={onSelect}
          testID={`${testID}-item-${item.key}`}
        />
      ))}
    </View>
  )
}