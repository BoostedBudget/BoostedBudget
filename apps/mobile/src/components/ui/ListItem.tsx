import { useState, type ReactNode } from 'react'
import { Pressable, View, type ViewProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Text } from './Text'
import { BUTTON_FOCUS_RING_COLOR_CLASS, BUTTON_FOCUS_RING_STYLE } from './Button'

export type ListItemRole = 'button' | 'link'

/**
 * The touch floor is applied as an explicit `minHeight`, never as padding, so a pressable row
 * cannot fall below the 44px minimum. `touchTarget` is theme-invariant in the generated adapter.
 */
export const LIST_ITEM_TOUCH_MIN = nativeTokens.light.touchTarget.min

const LIST_ITEM_BASE_CLASSES = 'flex-row items-center gap-bb-3 px-bb-4 py-bb-3'

export interface ListItemProps extends ViewProps {
  title: string
  subtitle?: string
  leading?: ReactNode
  trailing?: ReactNode
  /** When present the row becomes a button (or link); when absent it stays a plain row. */
  onPress?: () => void
  disabled?: boolean
  selected?: boolean
  /** The role a pressable row exposes. Defaults to `button`; use `link` when it navigates. */
  role?: ListItemRole
}

/**
 * A single list row. A pressable row exposes `accessibilityRole` (`button` by default, `link` when
 * it navigates), meets the touch-target floor and reports `accessibilityState.selected`; a
 * non-pressable row is a plain container with no button role and no touch floor.
 */
export function ListItem({
  title,
  subtitle,
  leading,
  trailing,
  onPress,
  disabled = false,
  selected,
  role = 'button',
  className,
  style,
  testID,
  accessibilityLabel,
  accessibilityHint,
  ...rest
}: ListItemProps) {
  const [focused, setFocused] = useState(false)

  const classes = [
    LIST_ITEM_BASE_CLASSES,
    selected ? 'bg-accent' : '',
    focused ? BUTTON_FOCUS_RING_COLOR_CLASS : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  const content = (
    <>
      {leading}
      <View className="flex-1 gap-bb-1">
        <Text variant="body" tone="default" testID={testID ? `${testID}-title` : undefined}>
          {title}
        </Text>
        {subtitle ? (
          // On a selected row the surface is `accent` (#f5f5f5), where `muted-foreground` measures
          // 4.35:1 — below the 4.5:1 body floor. The full-strength foreground is used instead, the
          // same asymmetry `Card.sunken` carries (Todo 10). Asserted by the Playwright rendered
          // contrast pass (Todo 22).
          <Text
            variant="bodySmall"
            tone={selected ? 'default' : 'muted'}
            testID={testID ? `${testID}-subtitle` : undefined}
          >
            {subtitle}
          </Text>
        ) : null}
      </View>
      {trailing}
    </>
  )

  if (onPress) {
    return (
      <Pressable
        testID={testID}
        disabled={disabled}
        onPress={onPress}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        accessibilityRole={role}
        accessibilityState={{ disabled, selected: selected ?? false }}
        accessibilityLabel={accessibilityLabel}
        accessibilityHint={accessibilityHint}
        className={classes}
        style={[
          { minHeight: LIST_ITEM_TOUCH_MIN },
          focused ? BUTTON_FOCUS_RING_STYLE : null,
          style,
        ]}
      >
        {content}
      </Pressable>
    )
  }

  return (
    <View
      {...rest}
      testID={testID}
      accessibilityState={selected !== undefined ? { selected } : undefined}
      accessibilityLabel={accessibilityLabel}
      accessibilityHint={accessibilityHint}
      className={classes}
      style={style}
    >
      {content}
    </View>
  )
}