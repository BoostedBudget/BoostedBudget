import { Text, type TextProps } from './Text'

export interface LabelProps extends Omit<TextProps, 'variant'> {
  /** Marks the field as required. The marker is text, never colour alone. */
  required?: boolean
  /** Renders the label in the muted tone when its control is disabled. */
  disabled?: boolean
}

/**
 * A form label. It renders the visible label text; `FormField` is what associates it with a
 * control's `accessibilityLabel`. The required marker is a literal `*` so the state is never
 * conveyed by colour alone.
 */
export function Label({ required = false, disabled = false, children, ...rest }: LabelProps) {
  return (
    <Text variant="label" tone={disabled ? 'muted' : 'default'} {...rest}>
      {children}
      {required ? ' *' : ''}
    </Text>
  )
}