import { View, type ViewProps } from 'react-native'

export type ListVariant = 'plain' | 'card'

export interface ListProps extends ViewProps {
  /** `card` paints the grouped surface; `plain` leaves the surface to the consumer. */
  variant?: ListVariant
}

/**
 * A container that groups `ListItem` rows. It is deliberately not a data grid: it renders a
 * vertical stack of rows and owns no columns, sorting or selection model.
 */
export function List({ variant = 'plain', className, ...rest }: ListProps) {
  const classes = [
    variant === 'card' ? 'bg-card border border-border rounded-lg overflow-hidden' : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return <View accessibilityRole="list" className={classes} {...rest} />
}