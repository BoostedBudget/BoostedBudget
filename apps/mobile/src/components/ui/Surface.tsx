import { View, type ViewProps } from 'react-native'

export type SurfaceVariant = 'canvas' | 'card' | 'raised' | 'sunken' | 'overlay'

const SURFACE_CLASSES: Record<SurfaceVariant, string> = {
  canvas: 'bg-background',
  card: 'bg-card border border-border rounded-lg',
  raised: 'bg-card border border-border rounded-lg shadow-bb-md',
  sunken: 'bg-muted rounded-lg',
  overlay: 'bg-popover border border-border rounded-lg shadow-bb-lg',
}

export interface SurfaceProps extends ViewProps {
  variant?: SurfaceVariant
}

export function Surface({ variant = 'canvas', className, ...rest }: SurfaceProps) {
  const classes = [SURFACE_CLASSES[variant], className].filter(Boolean).join(' ')

  return <View className={classes} {...rest} />
}
