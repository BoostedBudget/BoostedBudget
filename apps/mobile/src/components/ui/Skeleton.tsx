import { useEffect, useState } from 'react'
import { Animated, Easing, Platform, type DimensionValue, type ViewProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import {
  SPINNER_NORMAL_DURATION_MS,
  SPINNER_REDUCED_DURATION_MS,
  shouldUseNativeDriver,
  useReducedMotion,
} from './Spinner'

// react-native-web has no native animated module and logs a fallback warning on every mount when
// `useNativeDriver` is true (see Spinner.tsx). Native keeps the native driver.
export const SKELETON_USE_NATIVE_DRIVER = shouldUseNativeDriver(Platform.OS)

export type SkeletonRadius = 'none' | 'lg' | 'full'

// Only `none`, `lg` and `full` resolve on native (see `Box.tsx`): the calc-based radius tokens do
// not inline through react-native-css.
const RADIUS_CLASSES: Record<SkeletonRadius, string> = {
  none: 'rounded-none',
  lg: 'rounded-lg',
  full: 'rounded-full',
}

const DEFAULT_HEIGHT = nativeTokens.light.spacing['spacing-bb-4']

export interface SkeletonProps extends ViewProps {
  width?: DimensionValue
  height?: DimensionValue
  radius?: SkeletonRadius
}

export function Skeleton({
  width = '100%',
  height = DEFAULT_HEIGHT,
  radius = 'lg',
  className,
  style,
  ...rest
}: SkeletonProps) {
  const reduceMotion = useReducedMotion()
  const [opacity] = useState(() => new Animated.Value(1))
  const duration = reduceMotion ? SPINNER_REDUCED_DURATION_MS : SPINNER_NORMAL_DURATION_MS

  useEffect(() => {
    const animation = Animated.loop(
      Animated.sequence([
        Animated.timing(opacity, {
          toValue: 0.4,
          duration,
          easing: Easing.inOut(Easing.ease),
          useNativeDriver: SKELETON_USE_NATIVE_DRIVER,
        }),
        Animated.timing(opacity, {
          toValue: 1,
          duration,
          easing: Easing.inOut(Easing.ease),
          useNativeDriver: SKELETON_USE_NATIVE_DRIVER,
        }),
      ]),
    )
    animation.start()
    return () => animation.stop()
  }, [opacity, duration])

  return (
    <Animated.View
      {...rest}
      // Placeholder geometry is decorative: keep it out of the accessibility tree entirely.
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className={['bg-skeleton', RADIUS_CLASSES[radius], className].filter(Boolean).join(' ')}
      style={[style, { width, height, opacity }]}
    />
  )
}