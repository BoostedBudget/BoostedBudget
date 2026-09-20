import { Children } from 'react'
import { View, type ViewProps } from 'react-native'

export type ListVariant = 'plain' | 'card'

export interface ListProps extends ViewProps {
  /** `card` paints the grouped surface; `plain` leaves the surface to the consumer. */
  variant?: ListVariant
}

/**
 * A container that groups `ListItem` rows. It is deliberately not a data grid: it renders a
 * vertical stack of rows and owns no columns, sorting or selection model.
 *
 * Each child is wrapped in a `listitem` so the rendered `<ul role="list">` contains `<li>`
 * elements. A pressable `ListItem` renders a `<button>`, and a `<button>` is not an allowed child
 * of `list` — without the wrapper the browser a11y tree is structurally invalid (axe
 * `aria-required-children`). `role` (not `accessibilityRole`) is used because RN 0.86's legacy
 * `AccessibilityRole` union has no `listitem` member.
 */
export function List({ variant = 'plain', className, children, ...rest }: ListProps) {
  const classes = [
    variant === 'card' ? 'bg-card border border-border rounded-lg overflow-hidden' : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <View accessibilityRole="list" className={classes} {...rest}>
      {Children.map(children, (child) => (
        <View role="listitem">{child}</View>
      ))}
    </View>
  )
}