import { Pressable, Text, View, type ViewProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'

export type ToastVariant = 'info' | 'success' | 'warning' | 'danger'

export interface ToastAction {
  label: string
  onPress: () => void
}

interface ToastVariantStyle {
  /** Filled surface. `info`/`danger` use the darker `*-strong` contrast tokens. */
  container: string
  /** Foreground that clears 4.5:1 on the filled surface. */
  text: string
  /** Non-colour severity cue — the variant is never conveyed by colour alone. */
  cue: string
}

const TOAST_VARIANTS: Record<ToastVariant, ToastVariantStyle> = {
  info: { container: 'bg-bb-info-strong', text: 'text-info-foreground', cue: 'Info' },
  success: { container: 'bg-success', text: 'text-success-foreground', cue: 'Success' },
  warning: { container: 'bg-warning', text: 'text-warning-foreground', cue: 'Warning' },
  danger: { container: 'bg-bb-danger-strong', text: 'text-destructive-foreground', cue: 'Error' },
}

const TOUCH_MIN = nativeTokens.light.touchTarget.min

export interface ToastProps extends ViewProps {
  message: string
  variant?: ToastVariant
  action?: ToastAction
  onDismiss?: () => void
}

/**
 * Presentational transient notification. The provider owns the queue, the auto-dismiss timer and
 * the enter/exit transition; this component only paints one toast and reports dismissal.
 *
 * `danger` is announced assertively (it interrupts) while every other variant is polite (it waits
 * for a pause). Both carry a text cue so severity survives a colourblind or monochrome rendering.
 */
export function Toast({ message, variant = 'info', action, onDismiss, className, ...rest }: ToastProps) {
  const styles = TOAST_VARIANTS[variant]

  return (
    <View
      {...rest}
      accessibilityRole="alert"
      accessibilityLiveRegion={variant === 'danger' ? 'assertive' : 'polite'}
      className={['w-full rounded-lg p-bb-3 gap-bb-2', styles.container, className].filter(Boolean).join(' ')}
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
      <Text className={['font-bb-sans text-bb-sm', styles.text].join(' ')}>{message}</Text>
      {action ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={action.label}
          onPress={action.onPress}
          className="items-center justify-center self-start"
          style={{ minHeight: TOUCH_MIN }}
        >
          <Text className={['font-bb-sans font-bb-semibold text-bb-sm underline', styles.text].join(' ')}>
            {action.label}
          </Text>
        </Pressable>
      ) : null}
    </View>
  )
}
