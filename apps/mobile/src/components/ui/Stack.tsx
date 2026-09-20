import { View, type ViewProps } from 'react-native'
import {
  FLEX_ALIGN_CLASSES,
  FLEX_JUSTIFY_CLASSES,
  SPACING_CLASSES,
  type FlexAlign,
  type FlexJustify,
  type SpacingStep,
} from './spacing'

export type StackAlign = FlexAlign
export type StackJustify = FlexJustify

export interface StackProps extends ViewProps {
  gap?: SpacingStep
  align?: StackAlign
  justify?: StackJustify
}

export function Stack({ gap, align, justify, className, ...rest }: StackProps) {
  const classes = [
    'flex-col',
    gap === undefined ? '' : SPACING_CLASSES.gap[gap],
    align ? FLEX_ALIGN_CLASSES[align] : '',
    justify ? FLEX_JUSTIFY_CLASSES[justify] : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return <View className={classes} {...rest} />
}
