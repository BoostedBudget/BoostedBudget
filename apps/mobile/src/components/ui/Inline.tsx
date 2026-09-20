import { View, type ViewProps } from 'react-native'
import {
  FLEX_ALIGN_CLASSES,
  FLEX_JUSTIFY_CLASSES,
  SPACING_CLASSES,
  type FlexAlign,
  type FlexJustify,
  type SpacingStep,
} from './spacing'

export type InlineAlign = FlexAlign
export type InlineJustify = FlexJustify

export interface InlineProps extends ViewProps {
  gap?: SpacingStep
  align?: InlineAlign
  justify?: InlineJustify
  wrap?: boolean
}

export function Inline({ gap, align, justify, wrap = false, className, ...rest }: InlineProps) {
  const classes = [
    'flex-row',
    wrap ? 'flex-wrap' : 'flex-nowrap',
    gap === undefined ? '' : SPACING_CLASSES.gap[gap],
    align ? FLEX_ALIGN_CLASSES[align] : '',
    justify ? FLEX_JUSTIFY_CLASSES[justify] : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return <View className={classes} {...rest} />
}
