import { cloneElement, type ReactElement } from 'react'
import { View } from 'react-native'
import { Label } from './Label'
import { Text } from './Text'

export interface FormFieldProps {
  /** Visible label text. Also becomes the control's `accessibilityLabel`. */
  label: string
  /** Supporting text. Also becomes the control's `accessibilityHint`. */
  hint?: string
  /** Error message. Marks the control invalid and is announced in a polite live region. */
  error?: string
  required?: boolean
  disabled?: boolean
  /** The control this field wraps. `FormField` clones it with the accessibility wiring. */
  children: ReactElement
  testID?: string
}

/**
 * Wraps a single form control with a label, optional hint and optional error.
 *
 * The wrapped control receives `accessibilityLabel` (the label, plus "required" when required),
 * `accessibilityHint` (the hint), `invalid` and `disabled`. Controls turn those into
 * `accessibilityState`. The error renders in a `polite` live region, so it is announced rather
 * than only coloured.
 */
export function FormField({
  label,
  hint,
  error,
  required = false,
  disabled = false,
  children,
  testID,
}: FormFieldProps) {
  const accessibleLabel = required ? `${label} required` : label
  const invalid = Boolean(error)

  const injected: Record<string, unknown> = {
    accessibilityLabel: accessibleLabel,
    invalid,
    disabled,
  }
  if (hint !== undefined) injected.accessibilityHint = hint

  const control = cloneElement(children as ReactElement<Record<string, unknown>>, injected)

  return (
    <View testID={testID} className="gap-bb-2">
      <Label required={required} disabled={disabled}>
        {label}
      </Label>
      {control}
      {hint ? (
        <Text variant="caption" tone="muted">
          {hint}
        </Text>
      ) : null}
      {error ? (
        <Text
          testID={testID ? `${testID}-error` : undefined}
          variant="bodySmall"
          tone="danger"
          accessibilityLiveRegion="polite"
        >
          {error}
        </Text>
      ) : null}
    </View>
  )
}