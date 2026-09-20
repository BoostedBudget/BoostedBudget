import { useState } from 'react'
import { Platform, Pressable, View, type ViewProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Button, BUTTON_FOCUS_RING_COLOR_CLASS, BUTTON_FOCUS_RING_STYLE } from './Button'
import { Text } from './Text'

const TOUCH_MIN = nativeTokens.light.touchTarget.min

export interface ErrorStateProps extends ViewProps {
  title: string
  description: string
  onRetry: () => void
  retryLabel?: string
  /**
   * Supplementary, user-facing text. It is collapsed behind a disclosure by default and is never
   * derived from an internal error object — the caller must pass a safe, human-readable string.
   */
  detail?: string
}

/**
 * A non-colour-dependent error state.
 *
 * The title and description sit in one `accessible` node that carries `role="alert"` and an
 * assertive live region, so the error is announced once when it appears after a failed action.
 * The retry action and the detail disclosure are siblings of that node, so neither is swallowed
 * by the grouping. `detail` is only mounted when the disclosure is expanded, which keeps a
 * stack-trace-shaped string out of the accessibility tree until the user asks for it.
 */
export function ErrorState({
  title,
  description,
  onRetry,
  retryLabel = 'Retry',
  detail,
  className,
  testID,
  ...rest
}: ErrorStateProps) {
  const [expanded, setExpanded] = useState(false)
  const [focused, setFocused] = useState(false)

  return (
    <View
      {...rest}
      testID={testID}
      className={['items-center justify-center gap-bb-3 p-bb-6', className].filter(Boolean).join(' ')}
    >
      <View
        accessible
        accessibilityRole="alert"
        accessibilityLiveRegion="assertive"
        testID={testID ? `${testID}-summary` : undefined}
        className="items-center gap-bb-1"
      >
        <Text
          variant="subtitle"
          tone="default"
          align="center"
          testID={testID ? `${testID}-title` : undefined}
        >
          {title}
        </Text>
        <Text
          variant="bodySmall"
          tone="muted"
          align="center"
          testID={testID ? `${testID}-description` : undefined}
        >
          {description}
        </Text>
      </View>
      <Button
        variant="outline"
        size="sm"
        label={retryLabel}
        onPress={onRetry}
        testID={testID ? `${testID}-retry` : undefined}
      />
      {detail ? (
        <>
          <Pressable
            accessibilityRole="button"
            accessibilityState={{ expanded }}
            // Web only: react-native-web 0.21 drops `accessibilityState`, so the browser needs
            // `aria-expanded` directly (see Switch.tsx for why this is not applied on native).
            {...(Platform.OS === 'web' ? { 'aria-expanded': expanded } : {})}
            accessibilityLabel={expanded ? 'Hide details' : 'Show details'}
            onPress={() => setExpanded((previous) => !previous)}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            testID={testID ? `${testID}-detail-toggle` : undefined}
            className={[
              'items-center justify-center rounded-lg px-bb-2',
              focused ? BUTTON_FOCUS_RING_COLOR_CLASS : '',
            ]
              .filter(Boolean)
              .join(' ')}
            style={[{ minHeight: TOUCH_MIN }, focused ? BUTTON_FOCUS_RING_STYLE : null]}
          >
            <Text variant="label" tone="primary">
              {expanded ? 'Hide details' : 'Show details'}
            </Text>
          </Pressable>
          {expanded ? (
            <Text
              variant="bodySmall"
              tone="muted"
              testID={testID ? `${testID}-detail` : undefined}
            >
              {detail}
            </Text>
          ) : null}
        </>
      ) : null}
    </View>
  )
}