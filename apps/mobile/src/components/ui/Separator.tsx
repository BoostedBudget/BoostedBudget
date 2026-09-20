import { View, type ViewProps } from 'react-native'
import { SPACING_CLASSES, type SpacingStep } from './spacing'

export type SeparatorOrientation = 'horizontal' | 'vertical'

export interface SeparatorProps extends ViewProps {
  orientation?: SeparatorOrientation
  /**
   * Inset from the leading/trailing edges, expressed as a step on the shared spacing scale. A
   * horizontal rule insets inline (`mx-bb-N`); a vertical rule insets block-wise (`my-bb-N`).
   */
  inset?: SpacingStep
}

/**
 * A one-pixel divider. `inset` is a spacing-scale step, so the gap always lands on the same scale
 * as the surrounding layout rather than an arbitrary pixel value.
 */
export function Separator({
  orientation = 'horizontal',
  inset,
  className,
  ...rest
}: SeparatorProps) {
  const insetClass =
    inset === undefined
      ? ''
      : orientation === 'horizontal'
        ? SPACING_CLASSES.mx[inset]
        : SPACING_CLASSES.my[inset]

  const classes = [
    orientation === 'horizontal' ? 'h-px w-full bg-border' : 'w-px self-stretch bg-border',
    insetClass,
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return <View {...rest} className={classes} />
}