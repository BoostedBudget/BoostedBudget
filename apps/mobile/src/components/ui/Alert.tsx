import type { ReactNode } from 'react'
import { Pressable, Text, View, type ViewProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'

export type AlertVariant = 'info' | 'success' | 'warning' | 'danger'

interface AlertVariantStyle {
  /** Filled surface. `info`/`danger` use the darker `*-strong` contrast tokens. */
  container: string
  /** Foreground that clears 4.5:1 on the filled surface. */
  text: string
  /** Non-colour severity cue — severity is never conveyed by colour alone. */
  cue: string
}

const ALERT_VARIANTS: Record<AlertVariant, AlertVariantStyle> = {
  info: { container: 'bg-bb-info-strong', text: 'text-info-foreground', cue: 'Info' },
  success: { container: 'bg-success', text: 'text-success-foreground', cue: 'Success' },
  warning: { container: 'bg-warning', text: 'text-warning-foreground', cue: 'Warning' },
  danger: { container: 'bg-bb-danger-strong', text: 'text-destructive-foreground', cue: 'Error' },
}

const TOUCH_MIN = nativeTokens.light.touchTarget.min

export interface AlertProps extends ViewProps {
  variant?: AlertVariant
  title?: string
  onDismiss?: () => void
  children?: ReactNode
}

export function Alert({
  variant = 'info',
  title,
  onDismiss,
  children,
  className,
  ...rest
}: AlertProps) {
  const styles = ALERT_VARIANTS[variant]

  return (
    <View
      {...rest}
      accessibilityRole="alert"
      accessibilityLiveRegion="assertive"
      className={['rounded-lg p-bb-3 gap-bb-1', styles.container, className].filter(Boolean).join(' ')}
    >
      <View className="flex-row items-start justify-between gap-bb-2">
        <Text
          className={[
            'font-bb-sans font-bb-semibold text-bb-xs uppercase tracking-bb-wide',
            styles.text,
          ].join(' ')}
        >
          {styles.cue}
        </Text>
        {onDismiss ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Dismiss"
            onPress={onDismiss}
            className="items-center justify-center"
            style={{ minHeight: TOUCH_MIN, minWidth: TOUCH_MIN }}
          >
            <Text className={['text-bb-lg', styles.text].join(' ')}>×</Text>
          </Pressable>
        ) : null}
      </View>
      {title ? (
        <Text className={['font-bb-sans font-bb-semibold text-bb-body', styles.text].join(' ')}>
          {title}
        </Text>
      ) : null}
      {children ? (
        <Text className={['font-bb-sans text-bb-sm', styles.text].join(' ')}>{children}</Text>
      ) : null}
    </View>
  )
}