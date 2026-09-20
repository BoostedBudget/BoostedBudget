import { useEffect, useState } from 'react'
import { AccessibilityInfo, Animated, Easing, Platform, type ViewProps } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'

const MOTION = nativeTokens.light.motion

/**
 * `Animated.loop` reads the RAW `useNativeDriver` flag to choose its looping strategy
 * (`_isUsingNativeDriver()` returns `config.useNativeDriver`), and on web react-native-web has no
 * native animated module: the native-loop branch plays the timing ONCE and then freezes, so the
 * spinner never spins. Native keeps the native driver; web must use the JS driver so the loop
 * actually iterates. Asserted by the Playwright reduced-motion pass (Todo 22).
 */
const SPIN_USE_NATIVE_DRIVER = Platform.OS !== 'web'

/** Normal spin duration, taken from the motion scale. */
export const SPINNER_NORMAL_DURATION_MS = MOTION['transition-duration-bb-normal']
/**
 * Reduced-motion spin duration: the slowest motion token. The repo's doctrine is "slow, never
 * freeze" (`apps/web/src/index.css`) — a frozen spinner reads as a hung page, so the signal is
 * preserved by slowing the rotation rather than stopping it.
 */
export const SPINNER_REDUCED_DURATION_MS = MOTION['transition-duration-bb-slow']

export type SpinnerSize = 'sm' | 'md' | 'lg'

const SIZE_PX: Record<SpinnerSize, number> = {
  sm: nativeTokens.light.spacing['spacing-bb-4'],
  md: nativeTokens.light.spacing['spacing-bb-6'],
  lg: nativeTokens.light.spacing['spacing-bb-8'],
}

/**
 * Tracks the OS "reduce motion" preference. Starts `false`, resolves asynchronously, then follows
 * `reduceMotionChanged` events. Shared with `Skeleton` so every animated feedback primitive honours
 * the same preference through one code path.
 */
export function useReducedMotion(): boolean {
  const [reduceMotion, setReduceMotion] = useState(false)

  useEffect(() => {
    let active = true
    const apply = (enabled: boolean) => {
      if (active) setReduceMotion((previous) => (previous === enabled ? previous : enabled))
    }
    AccessibilityInfo.isReduceMotionEnabled().then(apply)
    const subscription = AccessibilityInfo.addEventListener('reduceMotionChanged', apply)
    return () => {
      active = false
      subscription.remove()
    }
  }, [])

  return reduceMotion
}

export interface SpinnerProps extends ViewProps {
  size?: SpinnerSize
  /** Required accessible name. A spinner with no name is unlabelled to a screen reader. */
  label: string
}

export function Spinner({ size = 'md', label, className, style, ...rest }: SpinnerProps) {
  const reduceMotion = useReducedMotion()
  const [rotation] = useState(() => new Animated.Value(0))
  const duration = reduceMotion ? SPINNER_REDUCED_DURATION_MS : SPINNER_NORMAL_DURATION_MS

  useEffect(() => {
    rotation.setValue(0)
    const animation = Animated.loop(
      Animated.timing(rotation, {
        toValue: 1,
        duration,
        easing: Easing.linear,
        useNativeDriver: SPIN_USE_NATIVE_DRIVER,
      }),
    )
    animation.start()
    return () => animation.stop()
  }, [rotation, duration])

  const spin = rotation.interpolate({ inputRange: [0, 1], outputRange: ['0deg', '360deg'] })
  const dimension = SIZE_PX[size]

  return (
    <Animated.View
      {...rest}
      accessibilityRole="progressbar"
      accessibilityLabel={label}
      className={['border-2 border-primary border-t-transparent rounded-full', className]
        .filter(Boolean)
        .join(' ')}
      style={[style, { width: dimension, height: dimension, transform: [{ rotate: spin }] }]}
    />
  )
}